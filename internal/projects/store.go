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
var ErrVersionConflict = errors.New("project version conflict")
var ErrArchived = errors.New("project is archived")

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

func (s *Store) List(ctx context.Context, tenantID, userID, role string, limit int) (Page, error) {
	rows, err := s.db.QueryContext(ctx, projectSelect+`
WHERE p.tenant_id=UUID_TO_BIN(?) AND (
  ? IN ('owner','project_admin')
  OR NOT EXISTS (SELECT 1 FROM project_memberships pm WHERE pm.tenant_id=p.tenant_id AND pm.user_id=UUID_TO_BIN(?))
  OR EXISTS (SELECT 1 FROM project_memberships pm WHERE pm.tenant_id=p.tenant_id AND pm.project_id=p.id AND pm.user_id=UUID_TO_BIN(?))
) ORDER BY p.created_at DESC,p.id DESC LIMIT ?`, tenantID, role, userID, userID, limit+1)
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

func (s *Store) Update(ctx context.Context, tenantID, projectID string, version uint64, input PatchInput) (Project, error) {
	current, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return Project{}, err
	}
	if current.Status == "archived" {
		return Project{}, ErrArchived
	}
	name, locale, region, timezone := current.Name, current.Locale, current.Region, current.Timezone
	if input.Name != nil {
		name = *input.Name
	}
	if input.Locale != nil {
		locale = *input.Locale
	}
	if input.Region != nil {
		region = *input.Region
	}
	if input.Timezone != nil {
		timezone = *input.Timezone
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE projects SET name=?,default_locale=?,region=?,timezone=?,version=version+1,updated_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND version=? AND status='active'`,
		name, locale, region, timezone, tenantID, projectID, version)
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Project{}, fmt.Errorf("read update result: %w", err)
	}
	if rowsAffected == 0 {
		return Project{}, ErrVersionConflict
	}
	return s.Get(ctx, tenantID, projectID)
}

func (s *Store) Archive(ctx context.Context, tenantID, projectID string, version uint64) (JobRef, error) {
	workflowID, err := uuid.NewV7()
	if err != nil {
		return JobRef{}, fmt.Errorf("create workflow id: %w", err)
	}
	jobID, err := uuid.NewV7()
	if err != nil {
		return JobRef{}, fmt.Errorf("create archive job id: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return JobRef{}, fmt.Errorf("begin archive transaction: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
UPDATE projects SET status='archived',archived_at=UTC_TIMESTAMP(6),version=version+1,updated_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND version=? AND status='active'`, tenantID, projectID, version)
	if err != nil {
		return JobRef{}, fmt.Errorf("archive project: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return JobRef{}, fmt.Errorf("read archive result: %w", err)
	}
	if rowsAffected == 0 {
		var status string
		var actualVersion uint64
		err = tx.QueryRowContext(ctx, `SELECT status,version FROM projects WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID).Scan(&status, &actualVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return JobRef{}, ErrNotFound
		}
		if err != nil {
			return JobRef{}, fmt.Errorf("read project archive state: %w", err)
		}
		if status == "archived" {
			return JobRef{}, ErrArchived
		}
		return JobRef{}, ErrVersionConflict
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO workflows (id,tenant_id,workflow_type,resource_type,resource_id,status,created_at,started_at,finished_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'project_archive','project',UUID_TO_BIN(?),'succeeded',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, workflowID.String(), tenantID, projectID)
	if err != nil {
		return JobRef{}, fmt.Errorf("insert archive workflow: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO jobs (id,tenant_id,workflow_id,stage,job_type,idempotency_key,state,attempt_count,max_attempts,version,payload_json,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'archive','project_archive',?,'succeeded',1,1,1,JSON_OBJECT('projectId',?),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		jobID.String(), tenantID, workflowID.String(), "project-archive:"+projectID+fmt.Sprintf(":%d", version), projectID)
	if err != nil {
		return JobRef{}, fmt.Errorf("insert archive job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return JobRef{}, fmt.Errorf("commit archive transaction: %w", err)
	}
	return JobRef{JobID: jobID.String(), Status: "succeeded"}, nil
}

const projectSelect = `
SELECT BIN_TO_UUID(p.id),p.name,
       (SELECT b.canonical_name FROM brands b WHERE b.tenant_id=p.tenant_id AND b.project_id=p.id ORDER BY b.created_at,b.id LIMIT 1),
       (SELECT w.base_url FROM websites w WHERE w.tenant_id=p.tenant_id AND w.project_id=p.id ORDER BY w.created_at,w.id LIMIT 1),
       p.default_locale,p.region,p.timezone,
       p.status,p.version,p.created_at,p.updated_at
FROM projects p `

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
