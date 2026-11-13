package crawls

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("crawl resource not found")
var ErrNoWork = errors.New("no crawl work available")
var ErrProjectArchived = errors.New("project is archived")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) PrimaryWebsiteID(ctx context.Context, tenantID, projectID string) (string, error) {
	var websiteID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM websites WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at,id LIMIT 1`, tenantID, projectID).Scan(&websiteID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read primary website: %w", err)
	}
	return websiteID, nil
}

func (s *Store) Create(ctx context.Context, tenantID, projectID string, input CreateInput) (Run, error) {
	var baseURL, projectStatus string
	err := s.db.QueryRowContext(ctx, `
SELECT w.base_url,p.status FROM websites w JOIN projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id
WHERE w.tenant_id=UUID_TO_BIN(?) AND w.project_id=UUID_TO_BIN(?) AND w.id=UUID_TO_BIN(?)`, tenantID, projectID, input.WebsiteID).Scan(&baseURL, &projectStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("load crawl website: %w", err)
	}
	if projectStatus == "archived" {
		return Run{}, ErrProjectArchived
	}
	startURL := baseURL
	if len(input.StartURLs) > 0 {
		startURL = input.StartURLs[0]
	}
	runID, _ := uuid.NewV7()
	urlID, _ := uuid.NewV7()
	config, err := json.Marshal(input)
	if err != nil {
		return Run{}, fmt.Errorf("encode crawl config: %w", err)
	}
	hash := sha256.Sum256([]byte(startURL))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("begin crawl creation: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
INSERT INTO crawl_runs (id,tenant_id,project_id,website_id,status,config_snapshot,discovered_count,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'queued',CAST(? AS JSON),1,UTC_TIMESTAMP(6))`, runID.String(), tenantID, projectID, input.WebsiteID, config)
	if err != nil {
		return Run{}, fmt.Errorf("insert crawl run: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO crawl_urls (id,tenant_id,crawl_run_id,original_url,normalized_url,normalized_url_hash,state,depth,priority,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'queued',0,100,UTC_TIMESTAMP(6))`, urlID.String(), tenantID, runID.String(), startURL, startURL, hash[:])
	if err != nil {
		return Run{}, fmt.Errorf("insert crawl URL: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit crawl creation: %w", err)
	}
	return s.Get(ctx, tenantID, runID.String())
}

func (s *Store) Get(ctx context.Context, tenantID, runID string) (Run, error) {
	var run Run
	err := s.db.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),BIN_TO_UUID(website_id),status,discovered_count,fetched_count,failed_count,started_at,finished_at,created_at
FROM crawl_runs WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, runID).Scan(&run.ID, &run.ProjectID, &run.WebsiteID, &run.Status, &run.DiscoveredCount, &run.FetchedCount, &run.FailedCount, &run.StartedAt, &run.FinishedAt, &run.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("read crawl run: %w", err)
	}
	run.Counts = Counts{Discovered: run.DiscoveredCount, Fetched: run.FetchedCount, Failed: run.FailedCount}
	return run, nil
}

func (s *Store) Claim(ctx context.Context, workerID string) (Claim, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Claim{}, fmt.Errorf("begin crawl claim: %w", err)
	}
	defer tx.Rollback()
	var claim Claim
	var configJSON []byte
	err = tx.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(r.id),BIN_TO_UUID(u.id),BIN_TO_UUID(r.website_id),u.normalized_url,r.config_snapshot
FROM crawl_urls u JOIN crawl_runs r ON r.tenant_id=u.tenant_id AND r.id=u.crawl_run_id
WHERE u.state='queued' AND r.status='queued' ORDER BY u.priority DESC,u.created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).
		Scan(&claim.RunID, &claim.CrawlURLID, &claim.WebsiteID, &claim.URL, &configJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Claim{}, ErrNoWork
	}
	if err != nil {
		return Claim{}, fmt.Errorf("select crawl work: %w", err)
	}
	var config CreateInput
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return Claim{}, fmt.Errorf("decode crawl config: %w", err)
	}
	claim.MaxBytes = 5 << 20
	_, err = tx.ExecContext(ctx, `UPDATE crawl_urls SET state='fetching',lease_owner=?,lease_expires_at=UTC_TIMESTAMP(6)+INTERVAL 2 MINUTE WHERE id=UUID_TO_BIN(?)`, workerID, claim.CrawlURLID)
	if err != nil {
		return Claim{}, fmt.Errorf("lease crawl URL: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='running',started_at=COALESCE(started_at,UTC_TIMESTAMP(6)) WHERE id=UUID_TO_BIN(?)`, claim.RunID)
	if err != nil {
		return Claim{}, fmt.Errorf("start crawl run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Claim{}, fmt.Errorf("commit crawl claim: %w", err)
	}
	return claim, nil
}

func (s *Store) Complete(ctx context.Context, completion Completion) error {
	contentHash, err := hex.DecodeString(completion.ContentHash)
	if err != nil || len(contentHash) != sha256.Size {
		return errors.New("invalid content hash")
	}
	urlHash := sha256.Sum256([]byte(completion.FinalURL))
	pageID, _ := uuid.NewV7()
	contentID, _ := uuid.NewV7()
	attemptID, _ := uuid.NewV7()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin crawl completion: %w", err)
	}
	defer tx.Rollback()
	var tenantID, websiteID string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(tenant_id),BIN_TO_UUID(website_id) FROM crawl_runs WHERE id=UUID_TO_BIN(?) AND status='running' FOR UPDATE`, completion.RunID).Scan(&tenantID, &websiteID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load running crawl: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO website_pages (id,tenant_id,website_id,canonical_url,normalized_url_hash,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE canonical_url=VALUES(canonical_url)`, pageID.String(), tenantID, websiteID, completion.FinalURL, urlHash[:])
	if err != nil {
		return fmt.Errorf("upsert website page: %w", err)
	}
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM website_pages WHERE website_id=UUID_TO_BIN(?) AND normalized_url_hash=?`, websiteID, urlHash[:]).Scan(&pageID)
	if err != nil {
		return fmt.Errorf("read website page: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO page_contents (id,tenant_id,page_id,crawl_run_id,http_status,final_url,title,language,content_hash,parser_version,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,UTC_TIMESTAMP(6))`, contentID.String(), tenantID, pageID.String(), completion.RunID, completion.HTTPStatus, completion.FinalURL, completion.Title, completion.Language, contentHash, completion.ParserVersion)
	if err != nil {
		return fmt.Errorf("insert page content: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE website_pages SET latest_content_id=UUID_TO_BIN(?) WHERE id=UUID_TO_BIN(?)`, contentID.String(), pageID.String())
	if err != nil {
		return fmt.Errorf("set latest page content: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE crawl_urls SET state='succeeded',lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?) AND crawl_run_id=UUID_TO_BIN(?)`, completion.CrawlURLID, completion.RunID)
	if err != nil {
		return fmt.Errorf("complete crawl URL: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fetch_attempts (id,tenant_id,crawl_url_id,attempt_no,status,http_status,metadata_json,started_at,finished_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),1,'succeeded',?,JSON_OBJECT('finalUrl',?),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, attemptID.String(), tenantID, completion.CrawlURLID, completion.HTTPStatus, completion.FinalURL)
	if err != nil {
		return fmt.Errorf("insert fetch attempt: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='succeeded',fetched_count=fetched_count+1,finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, completion.RunID)
	if err != nil {
		return fmt.Errorf("complete crawl run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit crawl completion: %w", err)
	}
	return nil
}

func (s *Store) Fail(ctx context.Context, failure Failure) error {
	if strings.TrimSpace(failure.ErrorCode) == "" {
		failure.ErrorCode = "FETCH_FAILED"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE crawl_urls u JOIN crawl_runs r ON r.id=u.crawl_run_id
SET u.state='failed',u.lease_owner=NULL,u.lease_expires_at=NULL,r.status='failed',r.failed_count=r.failed_count+1,r.finished_at=UTC_TIMESTAMP(6)
WHERE u.id=UUID_TO_BIN(?) AND r.id=UUID_TO_BIN(?)`, failure.CrawlURLID, failure.RunID)
	if err != nil {
		return fmt.Errorf("fail crawl run: %w", err)
	}
	return nil
}
