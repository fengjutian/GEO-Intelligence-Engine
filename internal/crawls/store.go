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
	"regexp"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/entities"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("crawl resource not found")
var ErrNoWork = errors.New("no crawl work available")
var ErrProjectArchived = errors.New("project is archived")
var ErrInvalidState = errors.New("crawl run state does not allow this action")
var ErrInvalidStartURL = errors.New("crawl start URL is invalid")
var ErrNoContentSnapshot = errors.New("crawl run has no content snapshots")

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

func (s *Store) RunContentAnalysis(ctx context.Context, tenantID, runID string) (ContentAnalysisRun, error) {
	var projectID, runStatus string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),status FROM crawl_runs WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, runID).Scan(&projectID, &runStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ContentAnalysisRun{}, ErrNotFound
	}
	if err != nil {
		return ContentAnalysisRun{}, err
	}
	version := "content-rules-v1"
	configHash := sha256.Sum256([]byte(version))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ContentAnalysisRun{}, err
	}
	defer tx.Rollback()
	analysisID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO content_analysis_runs (id,tenant_id,crawl_run_id,analyzer_version,config_hash,status,created_at,finished_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?, 'running',UTC_TIMESTAMP(6),NULL)
ON DUPLICATE KEY UPDATE status='running',finished_at=NULL`, analysisID, tenantID, runID, version, configHash[:])
	if err != nil {
		return ContentAnalysisRun{}, fmt.Errorf("upsert content analysis run: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM content_analysis_runs WHERE crawl_run_id=UUID_TO_BIN(?) AND analyzer_version=? AND config_hash=?`, runID, version, configHash[:]).Scan(&analysisID); err != nil {
		return ContentAnalysisRun{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM content_signals WHERE tenant_id=UUID_TO_BIN(?) AND analysis_run_id=UUID_TO_BIN(?)`, tenantID, analysisID); err != nil {
		return ContentAnalysisRun{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(id),content_json FROM page_contents WHERE tenant_id=UUID_TO_BIN(?) AND crawl_run_id=UUID_TO_BIN(?) AND content_json IS NOT NULL ORDER BY id`, tenantID, runID)
	if err != nil {
		return ContentAnalysisRun{}, err
	}
	type pageSnapshot struct {
		contentID string
		content   []byte
	}
	pages := []pageSnapshot{}
	for rows.Next() {
		var page pageSnapshot
		if err := rows.Scan(&page.contentID, &page.content); err != nil {
			rows.Close()
			return ContentAnalysisRun{}, err
		}
		pages = append(pages, page)
	}
	rows.Close()
	if len(pages) == 0 {
		return ContentAnalysisRun{}, ErrNoContentSnapshot
	}
	totalFindings := 0
	for _, page := range pages {
		var completion Completion
		if err := json.Unmarshal(page.content, &completion); err != nil {
			continue
		}
		rules := contentFindings(completion)
		if completion.TextLength < 200 {
			rules = append(rules, technicalFinding{"CONTENT_THIN", "medium", "Extracted page text is below the minimum useful length", map[string]any{"url": completion.FinalURL, "textLength": completion.TextLength, "threshold": 200}})
		}
		if _, err := tx.ExecContext(ctx, `UPDATE findings SET status='superseded',updated_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND source_type='page_content' AND source_id=UUID_TO_BIN(?) AND status='open'
  AND (source_version IN ('content-rules-v1','content-rules-v2') OR rule_code='CONTENT_THIN')`, tenantID, page.contentID); err != nil {
			return ContentAnalysisRun{}, err
		}
		for _, rule := range rules {
			findingID := uuid.NewString()
			evidence, _ := json.Marshal(rule.evidence)
			if _, err := tx.ExecContext(ctx, `INSERT INTO findings (id,tenant_id,project_id,source_type,source_id,source_version,rule_code,severity,confidence,status,summary,evidence_json,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'page_content',UUID_TO_BIN(?),'content-rules-v1',?,?,1.0,'open',?,CAST(? AS JSON),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
				findingID, tenantID, projectID, page.contentID, rule.code, rule.severity, rule.summary, evidence); err != nil {
				return ContentAnalysisRun{}, fmt.Errorf("insert re-run finding: %w", err)
			}
			signalID := uuid.NewString()
			value, _ := json.Marshal(map[string]any{"severity": rule.severity, "summary": rule.summary, "evidence": rule.evidence})
			if _, err := tx.ExecContext(ctx, `INSERT INTO content_signals (id,tenant_id,analysis_run_id,page_content_id,rule_code,value_json,confidence,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,CAST(? AS JSON),1.0,UTC_TIMESTAMP(6))`,
				signalID, tenantID, analysisID, page.contentID, rule.code, value); err != nil {
				return ContentAnalysisRun{}, fmt.Errorf("insert content signal: %w", err)
			}
			totalFindings++
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE content_analysis_runs SET status='succeeded',finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, analysisID); err != nil {
		return ContentAnalysisRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return ContentAnalysisRun{}, err
	}
	return s.GetContentAnalysisRun(ctx, tenantID, analysisID)
}

func (s *Store) GetContentAnalysisRun(ctx context.Context, tenantID, analysisID string) (ContentAnalysisRun, error) {
	var run ContentAnalysisRun
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(crawl_run_id),analyzer_version,status,created_at,finished_at
FROM content_analysis_runs WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, analysisID).Scan(&run.ID, &run.CrawlRunID, &run.AnalyzerVersion, &run.Status, &run.CreatedAt, &run.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ContentAnalysisRun{}, ErrNotFound
	}
	if err != nil {
		return ContentAnalysisRun{}, err
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT page_content_id),COUNT(*) FROM content_signals WHERE tenant_id=UUID_TO_BIN(?) AND analysis_run_id=UUID_TO_BIN(?)`, tenantID, analysisID).Scan(&run.Pages, &run.Findings)
	return run, nil
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
	_, err = tx.ExecContext(ctx, `INSERT INTO page_contents (id,tenant_id,page_id,crawl_run_id,http_status,final_url,title,language,content_hash,parser_version,content_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,CAST(? AS JSON),UTC_TIMESTAMP(6))`, contentID.String(), tenantID, pageID.String(), completion.RunID, completion.HTTPStatus, completion.FinalURL, completion.Title, completion.Language, contentHash, completion.ParserVersion, completionSnapshot(completion))
	if err != nil {
		return fmt.Errorf("insert page content: %w", err)
	}
	if len(completion.StructuredData) > 20 {
		completion.StructuredData = completion.StructuredData[:20]
	}
	if err := entities.Extract(ctx, tx, tenantID, projectID, contentID.String(), completion.StructuredData); err != nil {
		return fmt.Errorf("extract entities: %w", err)
	}
	if err := entities.ExtractText(ctx, tx, tenantID, projectID, contentID.String(), completion.TextSample); err != nil {
		return fmt.Errorf("extract text entity candidates: %w", err)
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

func completionSnapshot(completion Completion) string {
	content, _ := json.Marshal(map[string]any{
		"finalUrl": completion.FinalURL, "httpStatus": completion.HTTPStatus, "title": completion.Title,
		"language": completion.Language, "canonicalUrl": completion.CanonicalURL, "links": completion.Links,
		"noIndex": completion.NoIndex, "textLength": completion.TextLength, "headings": completion.Headings,
		"textSample": completion.TextSample, "author": completion.Author, "publishedAt": completion.PublishedAt,
		"faq": completion.FAQ, "terms": completion.Terms,
		"parserVersion": completion.ParserVersion, "rendered": completion.Rendered, "staticTextLength": completion.StaticTextLength,
	})
	return string(content)
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

func (s *Store) ListProjectFindings(ctx context.Context, tenantID, projectID, rulePrefix, status string, limit int) ([]Finding, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT BIN_TO_UUID(id),rule_code,severity,status,summary,evidence_json,created_at
FROM findings WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`
	args := []any{tenantID, projectID}
	if rulePrefix != "" {
		query += " AND rule_code LIKE ?"
		args = append(args, rulePrefix+"%")
	}
	if status != "" {
		query += " AND status=?"
		args = append(args, status)
	}
	query += " ORDER BY FIELD(severity,'critical','high','medium','low'),created_at DESC,id LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query project findings: %w", err)
	}
	defer rows.Close()
	findings := []Finding{}
	for rows.Next() {
		var finding Finding
		var evidence []byte
		if err := rows.Scan(&finding.ID, &finding.RuleCode, &finding.Severity, &finding.Status, &finding.Summary, &evidence, &finding.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project finding: %w", err)
		}
		_ = json.Unmarshal(evidence, &finding.Evidence)
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

func (s *Store) UpdateFindingStatus(ctx context.Context, tenantID, findingID, status, actorID, reason string) (Finding, error) {
	switch status {
	case "open", "confirmed", "dismissed", "accepted_risk", "resolved":
	default:
		return Finding{}, ErrInvalidState
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Finding{}, err
	}
	defer tx.Rollback()
	var previous string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM findings WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, findingID).Scan(&previous); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Finding{}, ErrNotFound
		}
		return Finding{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE findings SET status=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, status, tenantID, findingID); err != nil {
		return Finding{}, err
	}
	payload, _ := json.Marshal(map[string]any{"before": previous, "after": status, "reason": reason})
	auditID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (id,tenant_id,actor_type,actor_id,action,resource_type,resource_id,result,payload_summary,occurred_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'user',UUID_TO_BIN(?),'finding.updated','finding',UUID_TO_BIN(?),'succeeded',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		auditID, tenantID, actorID, findingID, payload); err != nil {
		return Finding{}, err
	}
	if err := tx.Commit(); err != nil {
		return Finding{}, err
	}
	var finding Finding
	var evidence []byte
	if err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),rule_code,severity,status,summary,evidence_json,created_at FROM findings WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, findingID).Scan(&finding.ID, &finding.RuleCode, &finding.Severity, &finding.Status, &finding.Summary, &evidence, &finding.CreatedAt); err != nil {
		return Finding{}, err
	}
	_ = json.Unmarshal(evidence, &finding.Evidence)
	return finding, nil
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
	findings = append(findings, contentFindings(completion)...)
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
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'page_content',UUID_TO_BIN(?),?,?,?,1.0,'open',?,CAST(? AS JSON),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
			id.String(), tenantID, projectID, contentID, findingVersion(finding.code), finding.code, finding.severity, finding.summary, evidence)
		if err != nil {
			return fmt.Errorf("insert technical finding: %w", err)
		}
	}
	return nil
}

var statisticPattern = regexp.MustCompile(`(?i)\d+(\.\d+)?\s*(%|percent|倍)|\d{4}\s*年`)

func findingVersion(code string) string {
	switch code {
	case "CONTENT_HEADINGS_MISSING", "CONTENT_H1_MISSING", "CONTENT_H1_MULTIPLE", "CONTENT_HEADING_SKIP", "CONTENT_ANSWER_FIRST", "CONTENT_NO_EVIDENCE", "CONTENT_AUTHOR_MISSING", "CONTENT_PUBLISHED_DATE_MISSING", "CONTENT_PUBLISHED_DATE_INVALID", "CONTENT_STALE_CONTENT", "CONTENT_FAQ_ANSWER_MISSING", "CONTENT_FAQ_DUPLICATE", "CONTENT_TERM_UNDEFINED", "CONTENT_ABSOLUTE_CLAIM":
		return "content-rules-v1"
	case "CONTENT_OPINION_AS_FACT":
		return "content-rules-v2"
	case "CONTENT_JS_ONLY":
		return "content-rules-v1"
	default:
		return "technical-rules-v1"
	}
}

// contentFindings applies deterministic P0 content-structure and evidence rules.
func contentFindings(completion Completion) []technicalFinding {
	findings := []technicalFinding{}
	if completion.Rendered && completion.StaticTextLength < 200 {
		findings = append(findings, technicalFinding{"CONTENT_JS_ONLY", "medium", "Page content requires JavaScript; non-rendering crawlers see little or no text", map[string]any{
			"url": completion.FinalURL, "staticTextLength": completion.StaticTextLength, "renderedTextLength": completion.TextLength,
		}})
	}
	h1Count := 0
	for _, heading := range completion.Headings {
		if heading.Level == 1 {
			h1Count++
		}
	}
	if len(completion.Headings) == 0 {
		if completion.TextLength >= 300 {
			findings = append(findings, technicalFinding{"CONTENT_HEADINGS_MISSING", "high", "Page has substantial text but no semantic headings", map[string]any{"textLength": completion.TextLength}})
		}
	} else if h1Count == 0 {
		findings = append(findings, technicalFinding{"CONTENT_H1_MISSING", "high", "Page does not contain an H1 heading", map[string]any{"headingCount": len(completion.Headings)}})
	}
	if h1Count > 1 {
		findings = append(findings, technicalFinding{"CONTENT_H1_MULTIPLE", "medium", "Page contains multiple H1 headings", map[string]any{"h1Count": h1Count}})
	}
	previousLevel := 0
	for _, heading := range completion.Headings {
		if previousLevel > 0 && heading.Level > previousLevel+1 {
			findings = append(findings, technicalFinding{"CONTENT_HEADING_SKIP", "medium", "Heading levels skip a level", map[string]any{"fromLevel": previousLevel, "toLevel": heading.Level, "heading": heading.Text}})
			break
		}
		previousLevel = heading.Level
	}
	sample := strings.TrimSpace(completion.TextSample)
	if sample != "" && completion.TextLength >= 300 {
		firstSentence := sample
		for _, separator := range []string{".", "。", "!", "！", "?", "？"} {
			if index := strings.Index(sample, separator); index > 0 && index < 200 {
				firstSentence = sample[:index+len(separator)]
				break
			}
		}
		if len([]rune(firstSentence)) < 40 {
			findings = append(findings, technicalFinding{"CONTENT_ANSWER_FIRST", "medium", "Opening text does not provide a direct answer", map[string]any{"opening": firstSentence, "threshold": 40}})
		}
		if statisticPattern.MatchString(sample) && externalLinkCount(completion) == 0 {
			findings = append(findings, technicalFinding{"CONTENT_NO_EVIDENCE", "medium", "Page contains statistics without external evidence links", map[string]any{"opening": firstSentence}})
		}
	}
	if strings.TrimSpace(completion.Author) == "" {
		findings = append(findings, technicalFinding{"CONTENT_AUTHOR_MISSING", "low", "Page does not declare an author", map[string]any{"url": completion.FinalURL}})
	}
	if strings.TrimSpace(completion.PublishedAt) == "" {
		findings = append(findings, technicalFinding{"CONTENT_PUBLISHED_DATE_MISSING", "low", "Page does not declare a published date", map[string]any{"url": completion.FinalURL}})
	} else if published, err := parsePublishedDate(completion.PublishedAt); err != nil {
		findings = append(findings, technicalFinding{"CONTENT_PUBLISHED_DATE_INVALID", "low", "Published date cannot be parsed", map[string]any{"publishedAt": completion.PublishedAt}})
	} else if age := time.Since(published); age > 730*24*time.Hour {
		findings = append(findings, technicalFinding{"CONTENT_STALE_CONTENT", "medium", "Page content appears stale", map[string]any{"publishedAt": completion.PublishedAt, "ageDays": int(age.Hours() / 24)}})
	}
	seenQuestions := map[string]bool{}
	for _, item := range completion.FAQ {
		question := strings.TrimSpace(item.Question)
		if question == "" {
			continue
		}
		if strings.TrimSpace(item.Answer) == "" {
			findings = append(findings, technicalFinding{"CONTENT_FAQ_ANSWER_MISSING", "high", "FAQ question has no answer", map[string]any{"question": question}})
		}
		normalized := strings.ToLower(question)
		if seenQuestions[normalized] {
			findings = append(findings, technicalFinding{"CONTENT_FAQ_DUPLICATE", "medium", "FAQ contains a duplicate question", map[string]any{"question": question}})
		}
		seenQuestions[normalized] = true
	}
	for _, term := range completion.Terms {
		if !term.Defined && term.Count >= 3 {
			findings = append(findings, technicalFinding{"CONTENT_TERM_UNDEFINED", "low", "Repeated acronym has no nearby definition", map[string]any{"term": term.Term, "count": term.Count}})
		}
	}
	if sample != "" {
		sourced := externalLinkCount(completion) > 0 || attributionPattern.MatchString(sample)
		if phrase := absoluteClaim(sample); phrase != "" {
			severity := "low"
			summary := "Page contains an absolute claim without qualification"
			if !sourced {
				severity = "medium"
				summary = "Page contains an absolute claim without a cited source"
			}
			findings = append(findings, technicalFinding{"CONTENT_ABSOLUTE_CLAIM", severity, summary, map[string]any{"phrase": phrase, "sourced": sourced}})
		}
		if finding, ok := opinionAsFact(sample, sourced); ok {
			findings = append(findings, finding)
		}
	}
	return findings
}

var opinionPattern = regexp.MustCompile(`(?i)\b(we believe|in our opinion|arguably|recommend|should)\b|我们认为|我觉得|值得|推荐`)
var attributionPattern = regexp.MustCompile(`(?i)\b(according to|research|report|survey|source)\b|根据|据.{0,12}(显示|表明|报告)|数据显示|研究表明|来源`)

func opinionAsFact(sample string, sourced bool) (technicalFinding, bool) {
	if sourced {
		return technicalFinding{}, false
	}
	for _, sentence := range splitSentences(sample) {
		if opinionPattern.MatchString(sentence) && absoluteClaim(sentence) != "" {
			return technicalFinding{"CONTENT_OPINION_AS_FACT", "medium", "Opinion is presented as an unsourced factual claim", map[string]any{"sentence": sentence}}, true
		}
	}
	return technicalFinding{}, false
}

func splitSentences(sample string) []string {
	parts := []string{}
	current := strings.Builder{}
	for _, char := range sample {
		current.WriteRune(char)
		if strings.ContainsRune("。！？.!?；;", char) {
			if value := strings.TrimSpace(current.String()); value != "" {
				parts = append(parts, value)
			}
			current.Reset()
		}
	}
	if value := strings.TrimSpace(current.String()); value != "" {
		parts = append(parts, value)
	}
	return parts
}

var absolutePattern = regexp.MustCompile(`(?i)\b(best|always|never|guaranteed|100%)\b`)

func absoluteClaim(sample string) string {
	for _, phrase := range []string{"绝对", "唯一", "最好", "一定"} {
		if strings.Contains(sample, phrase) {
			return phrase
		}
	}
	if match := absolutePattern.FindString(sample); match != "" {
		return match
	}
	return ""
}

func parsePublishedDate(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02T15:04:05Z0700"} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date: %s", value)
}

func externalLinkCount(completion Completion) int {
	base, err := url.Parse(completion.FinalURL)
	if err != nil || base.Host == "" {
		return 0
	}
	count := 0
	for _, raw := range completion.Links {
		link, err := url.Parse(raw)
		if err != nil || link.Host == "" {
			continue
		}
		if !strings.EqualFold(link.Host, base.Host) {
			count++
		}
	}
	return count
}
