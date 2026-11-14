package crawls

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/entities"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("crawl resource not found")
var ErrNoWork = errors.New("no crawl work available")
var ErrProjectArchived = errors.New("project is archived")
var ErrInvalidState = errors.New("crawl run state does not allow this action")
var ErrInvalidStartURL = errors.New("crawl start URL is invalid")

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
	input = NormalizeLimits(input)
	startURLs := input.StartURLs
	if len(startURLs) == 0 {
		startURLs = []string{baseURL}
	}
	normalizedStarts := make([]string, 0, len(startURLs))
	seenStarts := make(map[string]struct{}, len(startURLs))
	for _, raw := range startURLs {
		normalized, normalizeErr := NormalizeCrawlURL(raw)
		if normalizeErr != nil {
			return Run{}, ErrInvalidStartURL
		}
		if _, exists := seenStarts[normalized]; exists {
			continue
		}
		seenStarts[normalized] = struct{}{}
		normalizedStarts = append(normalizedStarts, normalized)
		if len(normalizedStarts) >= input.MaxPages {
			break
		}
	}
	if len(normalizedStarts) == 0 {
		return Run{}, ErrInvalidStartURL
	}
	runID, _ := uuid.NewV7()
	config, err := json.Marshal(input)
	if err != nil {
		return Run{}, fmt.Errorf("encode crawl config: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("begin crawl creation: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
INSERT INTO crawl_runs (id,tenant_id,project_id,website_id,status,config_snapshot,discovered_count,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'queued',CAST(? AS JSON),?,UTC_TIMESTAMP(6))`, runID.String(), tenantID, projectID, input.WebsiteID, config, len(normalizedStarts))
	if err != nil {
		return Run{}, fmt.Errorf("insert crawl run: %w", err)
	}
	for _, startURL := range normalizedStarts {
		urlID, _ := uuid.NewV7()
		hash := sha256.Sum256([]byte(startURL))
		_, err = tx.ExecContext(ctx, `
INSERT INTO crawl_urls (id,tenant_id,crawl_run_id,original_url,normalized_url,normalized_url_hash,state,depth,priority,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'queued',0,100,UTC_TIMESTAMP(6))`, urlID.String(), tenantID, runID.String(), startURL, startURL, hash[:])
		if err != nil {
			return Run{}, fmt.Errorf("insert crawl URL: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit crawl creation: %w", err)
	}
	return s.Get(ctx, tenantID, runID.String())
}

func (s *Store) Get(ctx context.Context, tenantID, runID string) (Run, error) {
	var run Run
	var blocked uint32
	err := s.db.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(r.id),BIN_TO_UUID(r.project_id),BIN_TO_UUID(r.website_id),r.status,r.discovered_count,r.fetched_count,r.failed_count,r.started_at,r.finished_at,r.created_at,
       (SELECT COUNT(*) FROM crawl_urls cu WHERE cu.crawl_run_id=r.id AND cu.state='blocked')
FROM crawl_runs r WHERE r.tenant_id=UUID_TO_BIN(?) AND r.id=UUID_TO_BIN(?)`, tenantID, runID).Scan(&run.ID, &run.ProjectID, &run.WebsiteID, &run.Status, &run.DiscoveredCount, &run.FetchedCount, &run.FailedCount, &run.StartedAt, &run.FinishedAt, &run.CreatedAt, &blocked)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("read crawl run: %w", err)
	}
	run.Counts = Counts{Discovered: run.DiscoveredCount, Fetched: run.FetchedCount, Failed: run.FailedCount, Blocked: blocked}
	return run, nil
}

func (s *Store) ListByProject(ctx context.Context, tenantID, projectID string, limit int) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(r.id),BIN_TO_UUID(r.project_id),BIN_TO_UUID(r.website_id),r.status,r.discovered_count,r.fetched_count,r.failed_count,r.started_at,r.finished_at,r.created_at,
       (SELECT COUNT(*) FROM crawl_urls cu WHERE cu.crawl_run_id=r.id AND cu.state='blocked')
FROM crawl_runs r WHERE r.tenant_id=UUID_TO_BIN(?) AND r.project_id=UUID_TO_BIN(?)
ORDER BY r.created_at DESC,r.id DESC LIMIT ?`, tenantID, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("query project crawl runs: %w", err)
	}
	defer rows.Close()
	runs := []Run{}
	for rows.Next() {
		var run Run
		var blocked uint32
		if err := rows.Scan(&run.ID, &run.ProjectID, &run.WebsiteID, &run.Status, &run.DiscoveredCount, &run.FetchedCount, &run.FailedCount, &run.StartedAt, &run.FinishedAt, &run.CreatedAt, &blocked); err != nil {
			return nil, fmt.Errorf("scan project crawl run: %w", err)
		}
		run.Counts = Counts{Discovered: run.DiscoveredCount, Fetched: run.FetchedCount, Failed: run.FailedCount, Blocked: blocked}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project crawl runs: %w", err)
	}
	return runs, nil
}

func (s *Store) Claim(ctx context.Context, workerID string) (Claim, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Claim{}, fmt.Errorf("begin crawl claim: %w", err)
	}
	defer tx.Rollback()
	// A worker crash leaves URLs leased in 'fetching'. Requeue them once the lease expires so
	// a long crawl can always make progress instead of stalling on a lost page.
	_, err = tx.ExecContext(ctx, `UPDATE crawl_urls SET state='queued',lease_owner=NULL,lease_expires_at=NULL
WHERE state='fetching' AND lease_expires_at IS NOT NULL AND lease_expires_at < UTC_TIMESTAMP(6)`)
	if err != nil {
		return Claim{}, fmt.Errorf("reclaim expired crawl leases: %w", err)
	}
	var claim Claim
	var configJSON []byte
	err = tx.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(r.id),BIN_TO_UUID(u.id),BIN_TO_UUID(r.website_id),u.normalized_url,u.depth,r.config_snapshot
FROM crawl_urls u JOIN crawl_runs r ON r.tenant_id=u.tenant_id AND r.id=u.crawl_run_id
WHERE u.state='queued' AND r.status IN ('queued','running') ORDER BY u.priority DESC,u.created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).
		Scan(&claim.RunID, &claim.CrawlURLID, &claim.WebsiteID, &claim.URL, &claim.Depth, &configJSON)
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
	var tenantID, websiteID, projectID, runStatus, baseURL string
	var configJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(r.tenant_id),BIN_TO_UUID(r.website_id),BIN_TO_UUID(r.project_id),r.status,r.config_snapshot,w.base_url
FROM crawl_runs r JOIN websites w ON w.id=r.website_id
WHERE r.id=UUID_TO_BIN(?) AND r.status IN ('running','paused') FOR UPDATE`, completion.RunID).Scan(&tenantID, &websiteID, &projectID, &runStatus, &configJSON, &baseURL)
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
	if len(completion.StructuredData) > 20 {
		completion.StructuredData = completion.StructuredData[:20]
	}
	if err := entities.Extract(ctx, tx, tenantID, projectID, contentID.String(), completion.StructuredData); err != nil {
		return fmt.Errorf("extract entities: %w", err)
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
	if err := insertTechnicalFindings(ctx, tx, tenantID, projectID, contentID.String(), completion); err != nil {
		return err
	}
	if err := enqueueDiscoveredLinks(ctx, tx, tenantID, completion, configJSON, baseURL); err != nil {
		return err
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
	var remaining int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM crawl_urls WHERE crawl_run_id=UUID_TO_BIN(?) AND state IN ('queued','fetching')`, completion.RunID).Scan(&remaining)
	if err != nil {
		return fmt.Errorf("count remaining crawl URLs: %w", err)
	}
	if remaining == 0 && runStatus != "paused" {
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='succeeded',fetched_count=fetched_count+1,finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, completion.RunID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status=?,fetched_count=fetched_count+1 WHERE id=UUID_TO_BIN(?)`, runStatus, completion.RunID)
	}
	if err != nil {
		return fmt.Errorf("complete crawl run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit crawl completion: %w", err)
	}
	return nil
}

func enqueueDiscoveredLinks(ctx context.Context, tx *sql.Tx, tenantID string, completion Completion, configJSON []byte, baseURL string) error {
	var config CreateInput
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return fmt.Errorf("decode crawl config: %w", err)
	}
	if config.MaxDepth <= 0 {
		return nil
	}
	var depth int
	err := tx.QueryRowContext(ctx, `SELECT depth FROM crawl_urls WHERE id=UUID_TO_BIN(?) AND crawl_run_id=UUID_TO_BIN(?)`, completion.CrawlURLID, completion.RunID).Scan(&depth)
	if err != nil {
		return fmt.Errorf("read crawl URL depth: %w", err)
	}
	if depth >= config.MaxDepth {
		return nil
	}
	var discovered int
	err = tx.QueryRowContext(ctx, `SELECT discovered_count FROM crawl_runs WHERE id=UUID_TO_BIN(?)`, completion.RunID).Scan(&discovered)
	if err != nil {
		return fmt.Errorf("read crawl discovered count: %w", err)
	}
	planned := PlanDiscovery(baseURL, completion.FinalURL, completion.Links, depth, config.MaxDepth, config.MaxPages-discovered)
	inserted := 0
	for _, link := range planned {
		urlID, _ := uuid.NewV7()
		hash := sha256.Sum256([]byte(link))
		priority := 100 - (depth+1)*10
		if priority < 0 {
			priority = 0
		}
		result, insertErr := tx.ExecContext(ctx, `
INSERT IGNORE INTO crawl_urls (id,tenant_id,crawl_run_id,original_url,normalized_url,normalized_url_hash,state,depth,priority,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'queued',?,?,UTC_TIMESTAMP(6))`, urlID.String(), tenantID, completion.RunID, link, link, hash[:], depth+1, priority)
		if insertErr != nil {
			return fmt.Errorf("enqueue discovered crawl URL: %w", insertErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("read enqueue result: %w", rowsErr)
		}
		if rows > 0 {
			inserted++
		}
	}
	if inserted > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET discovered_count=discovered_count+? WHERE id=UUID_TO_BIN(?)`, inserted, completion.RunID)
		if err != nil {
			return fmt.Errorf("update crawl discovered count: %w", err)
		}
	}
	return nil
}

func (s *Store) Fail(ctx context.Context, failure Failure) error {
	if strings.TrimSpace(failure.ErrorCode) == "" {
		failure.ErrorCode = "FETCH_FAILED"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin crawl failure: %w", err)
	}
	defer tx.Rollback()
	var runStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM crawl_runs WHERE id=UUID_TO_BIN(?) FOR UPDATE`, failure.RunID).Scan(&runStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read crawl state: %w", err)
	}
	if runStatus != "queued" && runStatus != "running" && runStatus != "paused" {
		// A late failure callback must not overwrite a cancelled or finished run.
		return nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE crawl_urls SET state='failed',lease_owner=NULL,lease_expires_at=NULL
WHERE id=UUID_TO_BIN(?) AND crawl_run_id=UUID_TO_BIN(?) AND state IN ('queued','fetching')`, failure.CrawlURLID, failure.RunID)
	if err != nil {
		return fmt.Errorf("fail crawl URL: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read failed crawl URL count: %w", err)
	}
	if affected > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET failed_count=failed_count+1 WHERE id=UUID_TO_BIN(?)`, failure.RunID)
		if err != nil {
			return fmt.Errorf("increment crawl failure count: %w", err)
		}
	}
	var remaining int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM crawl_urls WHERE crawl_run_id=UUID_TO_BIN(?) AND state IN ('queued','fetching')`, failure.RunID).Scan(&remaining)
	if err != nil {
		return fmt.Errorf("count remaining crawl URLs: %w", err)
	}
	switch {
	case runStatus == "paused":
		// Keep the pause intent; remaining pages resume on the next resume action.
	case remaining > 0:
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='running' WHERE id=UUID_TO_BIN(?)`, failure.RunID)
	default:
		var fetched int
		if err = tx.QueryRowContext(ctx, `SELECT fetched_count FROM crawl_runs WHERE id=UUID_TO_BIN(?)`, failure.RunID).Scan(&fetched); err == nil {
			finalStatus := "failed"
			if fetched > 0 {
				finalStatus = "succeeded"
			}
			_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status=?,finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, finalStatus, failure.RunID)
		}
	}
	if err != nil {
		return fmt.Errorf("finalize crawl after failure: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit crawl failure: %w", err)
	}
	return nil
}

// RecordRobotsSnapshot stores the latest robots.txt evidence for a run's website. The
// table keeps one row per website host so repeated crawls refresh rather than duplicate it.
func (s *Store) RecordRobotsSnapshot(ctx context.Context, runID string, snapshot RobotsSnapshot) error {
	if snapshot.ParserVersion == "" {
		snapshot.ParserVersion = "robots-v1"
	}
	if len(snapshot.Rules) == 0 {
		snapshot.Rules = json.RawMessage(`{}`)
	}
	if !json.Valid(snapshot.Rules) {
		return errors.New("invalid robots rules")
	}
	host := strings.TrimSpace(strings.ToLower(snapshot.Host))
	if host == "" {
		if parsed, err := url.Parse(snapshot.RequestedURL); err == nil {
			host = strings.ToLower(parsed.Hostname())
		}
	}
	if host == "" {
		return errors.New("robots snapshot requires a host")
	}
	if len(snapshot.Content) > 512<<10 {
		snapshot.Content = snapshot.Content[:512<<10]
	}
	var contentHash []byte
	if strings.TrimSpace(snapshot.ContentHash) != "" {
		decoded, err := hex.DecodeString(snapshot.ContentHash)
		if err != nil || len(decoded) != sha256.Size {
			return errors.New("invalid robots content hash")
		}
		contentHash = decoded
	}
	var tenantID, websiteID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(tenant_id),BIN_TO_UUID(website_id) FROM crawl_runs WHERE id=UUID_TO_BIN(?)`, runID).Scan(&tenantID, &websiteID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load crawl run for robots snapshot: %w", err)
	}
	snapshotID, _ := uuid.NewV7()
	_, err = s.db.ExecContext(ctx, `
INSERT INTO robots_snapshots (id,tenant_id,website_id,host,requested_url,http_status,content_hash,parser_version,rules_json,content,fetched_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,CAST(? AS JSON),?,UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE requested_url=VALUES(requested_url),http_status=VALUES(http_status),content_hash=VALUES(content_hash),
  parser_version=VALUES(parser_version),rules_json=VALUES(rules_json),content=VALUES(content),fetched_at=UTC_TIMESTAMP(6)`,
		snapshotID.String(), tenantID, websiteID, host, snapshot.RequestedURL, snapshot.HTTPStatus, contentHash, snapshot.ParserVersion, snapshot.Rules, snapshot.Content)
	if err != nil {
		return fmt.Errorf("store robots snapshot: %w", err)
	}
	return nil
}

// Block marks a URL as disallowed by robots.txt without counting it as a fetch failure.
func (s *Store) Block(ctx context.Context, block BlockedURL) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin crawl block: %w", err)
	}
	defer tx.Rollback()
	var runStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM crawl_runs WHERE id=UUID_TO_BIN(?) FOR UPDATE`, block.RunID).Scan(&runStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read crawl state: %w", err)
	}
	if runStatus != "queued" && runStatus != "running" && runStatus != "paused" {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE crawl_urls SET state='blocked',lease_owner=NULL,lease_expires_at=NULL
WHERE id=UUID_TO_BIN(?) AND crawl_run_id=UUID_TO_BIN(?) AND state IN ('queued','fetching')`, block.CrawlURLID, block.RunID)
	if err != nil {
		return fmt.Errorf("block crawl URL: %w", err)
	}
	var remaining int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM crawl_urls WHERE crawl_run_id=UUID_TO_BIN(?) AND state IN ('queued','fetching')`, block.RunID).Scan(&remaining)
	if err != nil {
		return fmt.Errorf("count remaining crawl URLs: %w", err)
	}
	switch {
	case runStatus == "paused":
	case remaining > 0:
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='running' WHERE id=UUID_TO_BIN(?)`, block.RunID)
	default:
		_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET status='succeeded',finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, block.RunID)
	}
	if err != nil {
		return fmt.Errorf("finalize crawl after block: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit crawl block: %w", err)
	}
	return nil
}

// RecordSitemaps persists sitemap parse evidence and enqueues discovered URLs for the run.
func (s *Store) RecordSitemaps(ctx context.Context, report SitemapReport) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sitemap report: %w", err)
	}
	defer tx.Rollback()
	var tenantID, websiteID, runStatus, baseURL string
	var configJSON []byte
	var discovered int
	err = tx.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(r.tenant_id),BIN_TO_UUID(r.website_id),r.status,r.config_snapshot,r.discovered_count,w.base_url
FROM crawl_runs r JOIN websites w ON w.id=r.website_id
WHERE r.id=UUID_TO_BIN(?) FOR UPDATE`, report.RunID).Scan(&tenantID, &websiteID, &runStatus, &configJSON, &discovered, &baseURL)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load crawl run for sitemaps: %w", err)
	}
	if runStatus != "queued" && runStatus != "running" && runStatus != "paused" {
		return nil
	}
	for _, document := range report.Documents {
		if strings.TrimSpace(document.URL) == "" {
			continue
		}
		if document.ParserVersion == "" {
			document.ParserVersion = "sitemap-v1"
		}
		var contentHash []byte
		if strings.TrimSpace(document.ContentHash) != "" {
			decoded, decodeErr := hex.DecodeString(document.ContentHash)
			if decodeErr != nil || len(decoded) != sha256.Size {
				continue
			}
			contentHash = decoded
		}
		documentID, _ := uuid.NewV7()
		_, err = tx.ExecContext(ctx, `INSERT INTO crawl_sitemaps (id,tenant_id,crawl_run_id,url,parent_url,http_status,content_hash,url_count,parser_version,error_code,fetched_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,?,UTC_TIMESTAMP(6))`,
			documentID.String(), tenantID, report.RunID, document.URL, document.ParentURL, document.HTTPStatus, contentHash, document.URLCount, document.ParserVersion, document.ErrorCode)
		if err != nil {
			return fmt.Errorf("insert crawl sitemap: %w", err)
		}
	}
	var config CreateInput
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return fmt.Errorf("decode crawl config: %w", err)
	}
	if config.MaxDepth > 0 && len(report.URLs) > 0 {
		planned := PlanDiscovery(baseURL, "", report.URLs, 0, 1, config.MaxPages-discovered)
		inserted := 0
		for _, link := range planned {
			urlID, _ := uuid.NewV7()
			hash := sha256.Sum256([]byte(link))
			result, insertErr := tx.ExecContext(ctx, `
INSERT IGNORE INTO crawl_urls (id,tenant_id,crawl_run_id,original_url,normalized_url,normalized_url_hash,state,depth,priority,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'queued',1,95,UTC_TIMESTAMP(6))`, urlID.String(), tenantID, report.RunID, link, link, hash[:])
			if insertErr != nil {
				return fmt.Errorf("enqueue sitemap URL: %w", insertErr)
			}
			rows, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return fmt.Errorf("read sitemap enqueue result: %w", rowsErr)
			}
			if rows > 0 {
				inserted++
			}
		}
		if inserted > 0 {
			_, err = tx.ExecContext(ctx, `UPDATE crawl_runs SET discovered_count=discovered_count+? WHERE id=UUID_TO_BIN(?)`, inserted, report.RunID)
			if err != nil {
				return fmt.Errorf("update sitemap discovered count: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sitemap report: %w", err)
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

// GetRobotsEvidence returns the robots.txt snapshots persisted for the run's website.
func (s *Store) GetRobotsEvidence(ctx context.Context, tenantID, runID string) ([]RobotsEvidence, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT rs.host,rs.requested_url,rs.http_status,COALESCE(LOWER(HEX(rs.content_hash)),''),rs.parser_version,rs.rules_json,COALESCE(rs.content,''),rs.fetched_at
FROM robots_snapshots rs JOIN crawl_runs r ON r.tenant_id=rs.tenant_id AND r.website_id=rs.website_id
WHERE r.tenant_id=UUID_TO_BIN(?) AND r.id=UUID_TO_BIN(?)
ORDER BY rs.fetched_at DESC,rs.host`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("query robots evidence: %w", err)
	}
	defer rows.Close()
	items := []RobotsEvidence{}
	for rows.Next() {
		var item RobotsEvidence
		var rules []byte
		if err := rows.Scan(&item.Host, &item.RequestedURL, &item.HTTPStatus, &item.ContentHash, &item.ParserVersion, &rules, &item.Content, &item.FetchedAt); err != nil {
			return nil, fmt.Errorf("scan robots evidence: %w", err)
		}
		item.Rules = json.RawMessage(rules)
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListSitemapEvidence returns every sitemap document fetched for the run.
func (s *Store) ListSitemapEvidence(ctx context.Context, tenantID, runID string) ([]SitemapEvidence, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT sm.url,COALESCE(sm.parent_url,''),sm.http_status,COALESCE(LOWER(HEX(sm.content_hash)),''),sm.url_count,sm.parser_version,COALESCE(sm.error_code,''),sm.fetched_at
FROM crawl_sitemaps sm JOIN crawl_runs r ON r.tenant_id=sm.tenant_id AND r.id=sm.crawl_run_id
WHERE sm.tenant_id=UUID_TO_BIN(?) AND sm.crawl_run_id=UUID_TO_BIN(?)
ORDER BY sm.fetched_at,sm.url`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("query sitemap evidence: %w", err)
	}
	defer rows.Close()
	items := []SitemapEvidence{}
	for rows.Next() {
		var item SitemapEvidence
		if err := rows.Scan(&item.URL, &item.ParentURL, &item.HTTPStatus, &item.ContentHash, &item.URLCount, &item.ParserVersion, &item.ErrorCode, &item.FetchedAt); err != nil {
			return nil, fmt.Errorf("scan sitemap evidence: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListFindings(ctx context.Context, tenantID, runID string) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(f.id),f.rule_code,f.severity,f.status,f.summary,f.evidence_json,f.created_at
FROM findings f JOIN page_contents c ON c.id=f.source_id
WHERE f.tenant_id=UUID_TO_BIN(?) AND c.crawl_run_id=UUID_TO_BIN(?) AND f.source_type='page_content'
ORDER BY FIELD(f.severity,'critical','high','medium','low'),f.created_at,f.id`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("query crawl findings: %w", err)
	}
	defer rows.Close()
	findings := []Finding{}
	for rows.Next() {
		var finding Finding
		var evidence []byte
		if err := rows.Scan(&finding.ID, &finding.RuleCode, &finding.Severity, &finding.Status, &finding.Summary, &evidence, &finding.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan crawl finding: %w", err)
		}
		if err := json.Unmarshal(evidence, &finding.Evidence); err != nil {
			return nil, fmt.Errorf("decode finding evidence: %w", err)
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

type technicalFinding struct {
	code     string
	severity string
	summary  string
	evidence map[string]any
}

func insertTechnicalFindings(ctx context.Context, tx *sql.Tx, tenantID, projectID, contentID string, completion Completion) error {
	findings := []technicalFinding{}
	if completion.HTTPStatus < 200 || completion.HTTPStatus >= 300 {
		findings = append(findings, technicalFinding{"HTTP_NON_2XX", "high", "Page returned a non-success HTTP status", map[string]any{"httpStatus": completion.HTTPStatus, "url": completion.FinalURL}})
	}
	if completion.CanonicalURL == nil || strings.TrimSpace(*completion.CanonicalURL) == "" {
		findings = append(findings, technicalFinding{"CANONICAL_MISSING", "medium", "Page does not declare a canonical URL", map[string]any{"url": completion.FinalURL}})
	}
	if completion.NoIndex {
		findings = append(findings, technicalFinding{"ROBOTS_NOINDEX", "high", "Page declares a noindex directive", map[string]any{"url": completion.FinalURL}})
	}
	if completion.Language == nil || strings.TrimSpace(*completion.Language) == "" {
		findings = append(findings, technicalFinding{"HTML_LANG_MISSING", "low", "Page does not declare an HTML language", map[string]any{"url": completion.FinalURL}})
	}
	if completion.TextLength < 200 {
		findings = append(findings, technicalFinding{"CONTENT_THIN", "medium", "Extracted page text is below the minimum useful length", map[string]any{"url": completion.FinalURL, "textLength": completion.TextLength, "threshold": 200}})
	}
	for _, finding := range findings {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create finding id: %w", err)
		}
		evidence, err := json.Marshal(finding.evidence)
		if err != nil {
			return fmt.Errorf("encode finding evidence: %w", err)
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO findings (id,tenant_id,project_id,source_type,source_id,source_version,rule_code,severity,confidence,status,summary,evidence_json,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'page_content',UUID_TO_BIN(?),'technical-rules-v1',?,?,1.0,'open',?,CAST(? AS JSON),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
			id.String(), tenantID, projectID, contentID, finding.code, finding.severity, finding.summary, evidence)
		if err != nil {
			return fmt.Errorf("insert technical finding: %w", err)
		}
	}
	return nil
}
