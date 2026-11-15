package drafts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound marks a missing draft.
var ErrNotFound = errors.New("draft was not found")

// ErrInvalid marks unusable input.
var ErrInvalid = errors.New("draft input is invalid")

// ErrConflict marks an illegal status transition.
var ErrConflict = errors.New("draft status transition is not allowed")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Generate builds one draft from the current project configuration.
func (s *Store) Generate(ctx context.Context, tenantID, projectID, kind string) (Draft, error) {
	if !ValidKind(kind) {
		return Draft{}, ErrInvalid
	}
	var brandName, officialURL, description string
	err := s.db.QueryRowContext(ctx, `SELECT b.canonical_name,
COALESCE((SELECT w.base_url FROM websites w WHERE w.tenant_id=b.tenant_id AND w.project_id=b.project_id ORDER BY w.created_at LIMIT 1), CONCAT('https://', b.official_domain)),
COALESCE(b.description,'')
FROM brands b
WHERE b.tenant_id=UUID_TO_BIN(?) AND b.project_id=UUID_TO_BIN(?) ORDER BY b.created_at LIMIT 1`, tenantID, projectID).Scan(&brandName, &officialURL, &description)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	if err != nil {
		return Draft{}, err
	}
	brand := Brand{Name: brandName, OfficialURL: officialURL, Description: description}

	var title string
	var payload map[string]any
	switch kind {
	case "jsonld":
		title = "Schema.org 结构化数据"
		payload = BuildJSONLD(brand)
	case "llms_txt":
		title = "llms.txt 索引"
		payload = map[string]any{"content": BuildLLMsTxt(brand, s.projectURLs(ctx, tenantID, projectID))}
	case "meta":
		title = "页面 meta 建议"
		payload = BuildMeta(brand, officialURL, brandName)
	}
	content, _ := json.Marshal(payload)
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO content_drafts (id,tenant_id,project_id,kind,status,title,payload_json,version,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,'draft',?,CAST(? AS JSON),1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		id, tenantID, projectID, kind, title, content); err != nil {
		return Draft{}, err
	}
	return s.Get(ctx, tenantID, projectID, id)
}

func (s *Store) projectURLs(ctx context.Context, tenantID, projectID string) []string {
	rows, err := s.db.QueryContext(ctx, `SELECT pc.final_url FROM page_contents pc
JOIN crawl_runs cr ON cr.id=pc.crawl_run_id
WHERE pc.tenant_id=UUID_TO_BIN(?) AND cr.project_id=UUID_TO_BIN(?)
ORDER BY pc.created_at DESC LIMIT 200`, tenantID, projectID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	urls := []string{}
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err == nil {
			urls = append(urls, url)
		}
	}
	return urls
}

// List returns drafts newest first.
func (s *Store) List(ctx context.Context, tenantID, projectID, kind string) ([]Draft, error) {
	query := `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),page_content_id,kind,status,title,payload_json,version,created_at,updated_at
FROM content_drafts WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`
	args := []any{tenantID, projectID}
	if kind != "" {
		query += " AND kind=?"
		args = append(args, kind)
	}
	query += " ORDER BY created_at DESC LIMIT 200"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Draft{}
	for rows.Next() {
		item, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Get returns one draft.
func (s *Store) Get(ctx context.Context, tenantID, projectID, id string) (Draft, error) {
	row := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),page_content_id,kind,status,title,payload_json,version,created_at,updated_at
FROM content_drafts WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID, id)
	item, err := scanDraft(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	return item, err
}

// SetStatus moves a draft through draft -> approved -> published.
func (s *Store) SetStatus(ctx context.Context, tenantID, projectID, id, status string) (Draft, error) {
	current, err := s.Get(ctx, tenantID, projectID, id)
	if err != nil {
		return Draft{}, err
	}
	if !allowedTransition(current.Status, status) {
		return Draft{}, ErrConflict
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE content_drafts SET status=?,version=version+1,updated_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, status, tenantID, projectID, id); err != nil {
		return Draft{}, err
	}
	return s.Get(ctx, tenantID, projectID, id)
}

func allowedTransition(from, to string) bool {
	switch from {
	case "draft":
		return to == "approved" || to == "discarded"
	case "approved":
		return to == "published" || to == "discarded"
	default:
		return false
	}
}

// Changes compares two drafts of the same project.
func (s *Store) Changes(ctx context.Context, tenantID, projectID, fromID, toID string) ([]Change, error) {
	before, err := s.Get(ctx, tenantID, projectID, fromID)
	if err != nil {
		return nil, err
	}
	after, err := s.Get(ctx, tenantID, projectID, toID)
	if err != nil {
		return nil, err
	}
	return Diff(before.Payload, after.Payload), nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanDraft(row rowScanner) (Draft, error) {
	var item Draft
	var pageContent []byte
	var payload []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(&item.ID, &item.ProjectID, &pageContent, &item.Kind, &item.Status, &item.Title, &payload, &item.Version, &createdAt, &updatedAt); err != nil {
		return Draft{}, err
	}
	if len(pageContent) > 0 {
		value := string(pageContent)
		item.PageContentID = &value
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &item.Payload)
	}
	item.CreatedAt = createdAt
	item.UpdatedAt = updatedAt
	return item, nil
}
