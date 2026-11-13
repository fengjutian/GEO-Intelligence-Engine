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
var ErrInvalidState = errors.New("crawl run state does not allow this action")

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
	canonicalURL := completion.CanonicalURL
	if canonicalURL == nil {
		canonicalURL = &completion.FinalURL
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin crawl completion: %w", err)
	}
	defer tx.Rollback()
	var tenantID, websiteID string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(tenant_id),BIN_TO_UUID(website_id) FROM crawl_runs WHERE id=UUID_TO_BIN(?) AND status IN ('running','paused') FOR UPDATE`, completion.RunID).Scan(&tenantID, &websiteID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load running crawl: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO website_pages (id,tenant_id,website_id,canonical_url,normalized_url_hash,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE canonical_url=VALUES(canonical_url)`, pageID.String(), tenantID, websiteID, canonicalURL, urlHash[:])
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
	for index, link := range completion.Links {
		if index >= 100 {
			break
		}
		if strings.TrimSpace(link) == "" {
			continue
		}
		linkID, _ := uuid.NewV7()
		linkHash := sha256.Sum256([]byte(link))
		_, err = tx.ExecContext(ctx, `INSERT INTO page_links (id,tenant_id,source_content_id,target_url,target_url_hash)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?)`, linkID.String(), tenantID, contentID.String(), link, linkHash[:])
		if err != nil {
			return fmt.Errorf("insert page link: %w", err)
		}
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
	_, err = tx.ExecContext(ctx, `UPDATE crawl_runs
SET status=IF(status='paused','paused','succeeded'),fetched_count=fetched_count+1,
    finished_at=IF(status='paused',NULL,UTC_TIMESTAMP(6)) WHERE id=UUID_TO_BIN(?)`, completion.RunID)
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
	WHERE u.id=UUID_TO_BIN(?) AND r.id=UUID_TO_BIN(?) AND r.status IN ('queued','running')`, failure.CrawlURLID, failure.RunID)
	if err != nil {
		return fmt.Errorf("fail crawl run: %w", err)
	}
	return nil
}

func (s *Store) Control(ctx context.Context, tenantID, runID, action string) (Run, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("begin crawl control: %w", err)
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM crawl_runs WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, runID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("read crawl state: %w", err)
	}
	switch action {
	case "pause":
		if status != "queued" && status != "running" {
			return Run{}, ErrInvalidState
		}
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='paused' WHERE id=UUID_TO_BIN(?)`, runID)
	case "resume":
		if status != "paused" {
			return Run{}, ErrInvalidState
		}
		var remaining int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM crawl_urls WHERE crawl_run_id=UUID_TO_BIN(?) AND state IN ('queued','fetching')`, runID).Scan(&remaining); err == nil {
			if remaining > 0 {
				_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='queued' WHERE id=UUID_TO_BIN(?)`, runID)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='succeeded',finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, runID)
			}
		}
	case "cancel":
		if status != "queued" && status != "running" && status != "paused" {
			return Run{}, ErrInvalidState
		}
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='cancelled',finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, runID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE crawl_urls SET state='cancelled',lease_owner=NULL,lease_expires_at=NULL WHERE crawl_run_id=UUID_TO_BIN(?) AND state IN ('queued','fetching')`, runID)
		}
	default:
		return Run{}, ErrInvalidState
	}
	if err != nil {
		return Run{}, fmt.Errorf("apply crawl action: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit crawl action: %w", err)
	}
	return s.Get(ctx, tenantID, runID)
}

func (s *Store) ListPages(ctx context.Context, tenantID, runID string) ([]PageSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(p.id),BIN_TO_UUID(c.id),p.canonical_url,c.final_url,c.http_status,c.title,c.language,
       LOWER(HEX(c.content_hash)),c.parser_version,c.created_at
FROM page_contents c
JOIN website_pages p ON p.id=c.page_id
JOIN crawl_runs r ON r.id=c.crawl_run_id AND r.tenant_id=c.tenant_id
WHERE c.tenant_id=UUID_TO_BIN(?) AND c.crawl_run_id=UUID_TO_BIN(?)
ORDER BY c.created_at,p.id`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("query crawl pages: %w", err)
	}
	defer rows.Close()
	pages := []PageSummary{}
	for rows.Next() {
		var page PageSummary
		if err := rows.Scan(&page.PageID, &page.ContentID, &page.CanonicalURL, &page.FinalURL, &page.HTTPStatus, &page.Title, &page.Language, &page.ContentHash, &page.ParserVersion, &page.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan crawl page: %w", err)
		}
		pages = append(pages, page)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate crawl pages: %w", err)
	}
	return pages, nil
}

func (s *Store) GetPageContent(ctx context.Context, tenantID, contentID string) (PageDetail, error) {
	var detail PageDetail
	err := s.db.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(p.id),BIN_TO_UUID(c.id),p.canonical_url,c.final_url,c.http_status,c.title,c.language,
       LOWER(HEX(c.content_hash)),c.parser_version,c.created_at
FROM page_contents c JOIN website_pages p ON p.id=c.page_id
WHERE c.tenant_id=UUID_TO_BIN(?) AND c.id=UUID_TO_BIN(?)`, tenantID, contentID).Scan(
		&detail.PageID, &detail.ContentID, &detail.CanonicalURL, &detail.FinalURL, &detail.HTTPStatus,
		&detail.Title, &detail.Language, &detail.ContentHash, &detail.ParserVersion, &detail.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PageDetail{}, ErrNotFound
	}
	if err != nil {
		return PageDetail{}, fmt.Errorf("read page content: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT target_url FROM page_links WHERE source_content_id=UUID_TO_BIN(?) ORDER BY id`, contentID)
	if err != nil {
		return PageDetail{}, fmt.Errorf("query page links: %w", err)
	}
	defer rows.Close()
	detail.Links = []string{}
	for rows.Next() {
		var link string
		if err := rows.Scan(&link); err != nil {
			return PageDetail{}, fmt.Errorf("scan page link: %w", err)
		}
		detail.Links = append(detail.Links, link)
	}
	return detail, rows.Err()
}
