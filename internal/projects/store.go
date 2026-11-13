package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("project not found")

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, tenantID string, input CreateInput) (Project, error) {
	projectID, err := uuid.NewV7()
	if err != nil {
		return Project{}, fmt.Errorf("create project id: %w", err)
	}
	brandID, err := uuid.NewV7()
	if err != nil {
		return Project{}, fmt.Errorf("create brand id: %w", err)
	}
	websiteID, err := uuid.NewV7()
	if err != nil {
		return Project{}, fmt.Errorf("create website id: %w", err)
	}
	parsedURL, err := url.Parse(input.OfficialURL)
	if err != nil {
		return Project{}, fmt.Errorf("parse official URL: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
INSERT INTO projects (id,tenant_id,name,status,timezone,default_locale,region,version,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),?,'active',?,?,?,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		projectID.String(), tenantID, input.Name, input.Timezone, input.Locale, input.Region)
	if err != nil {
		return Project{}, fmt.Errorf("insert project: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO brands (id,tenant_id,project_id,canonical_name,official_domain,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'active',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		brandID.String(), tenantID, projectID.String(), input.BrandName, strings.ToLower(parsedURL.Hostname()))
	if err != nil {
		return Project{}, fmt.Errorf("insert brand: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO websites (id,tenant_id,project_id,base_url,normalized_host,ownership_status,crawl_policy,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'unverified',JSON_OBJECT(),UTC_TIMESTAMP(6))`,
		websiteID.String(), tenantID, projectID.String(), input.OfficialURL, strings.ToLower(parsedURL.Hostname()))
	if err != nil {
		return Project{}, fmt.Errorf("insert website: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project transaction: %w", err)
	}
	return s.Get(ctx, tenantID, projectID.String())
}

func (s *Store) Get(ctx context.Context, tenantID, projectID string) (Project, error) {
	row := s.db.QueryRowContext(ctx, projectSelect+`
WHERE p.tenant_id=UUID_TO_BIN(?) AND p.id=UUID_TO_BIN(?)`, tenantID, projectID)
	return scanProject(row)
}

func (s *Store) List(ctx context.Context, tenantID string, limit int) (Page, error) {
	rows, err := s.db.QueryContext(ctx, projectSelect+`
WHERE p.tenant_id=UUID_TO_BIN(?) ORDER BY p.created_at DESC,p.id DESC LIMIT ?`, tenantID, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	items := make([]Project, 0, limit)
	for rows.Next() {
		project, scanErr := scanProject(rows)
		if scanErr != nil {
			return Page{}, scanErr
		}
		items = append(items, project)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate projects: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return Page{Items: items, Page: PageMeta{HasMore: hasMore}}, nil
}

const projectSelect = `
SELECT BIN_TO_UUID(p.id),p.name,b.canonical_name,w.base_url,p.default_locale,p.region,p.timezone,
       p.status,p.version,p.created_at,p.updated_at
FROM projects p
JOIN brands b ON b.tenant_id=p.tenant_id AND b.project_id=p.id
JOIN websites w ON w.tenant_id=p.tenant_id AND w.project_id=p.id `

type scanner interface {
	Scan(dest ...any) error
}

func scanProject(row scanner) (Project, error) {
	var project Project
	err := row.Scan(&project.ID, &project.Name, &project.BrandName, &project.OfficialURL,
		&project.Locale, &project.Region, &project.Timezone, &project.Status, &project.Version,
		&project.CreatedAt, &project.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("scan project: %w", err)
	}
	return project, nil
}
