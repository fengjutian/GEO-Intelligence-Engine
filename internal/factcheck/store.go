package factcheck

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("content analysis run was not found")
var ErrNoContent = errors.New("content analysis run has no page text")

type Check struct {
	ID            string  `json:"id"`
	PageContentID string  `json:"pageContentId"`
	Claim         string  `json:"claim"`
	Verdict       string  `json:"verdict"`
	Confidence    float64 `json:"confidence"`
	Evidence      string  `json:"evidence"`
	Status        string  `json:"status"`
}

type Run struct {
	ID         string    `json:"id"`
	AnalysisID string    `json:"analysisRunId"`
	Status     string    `json:"status"`
	Model      string    `json:"model"`
	Checks     []Check   `json:"checks"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Store struct {
	db      *sql.DB
	gateway *modelgateway.Gateway
}

func NewStore(db *sql.DB, gateway *modelgateway.Gateway) *Store {
	return &Store{db: db, gateway: gateway}
}

type pageText struct {
	id   string
	text string
}

// Verify asks the judge model to extract claims and label them against page text only.
func (s *Store) Verify(ctx context.Context, tenantID, analysisRunID string) (Run, error) {
	var projectID, crawlRunID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(c.project_id),BIN_TO_UUID(a.crawl_run_id)
FROM content_analysis_runs a JOIN crawl_runs c ON c.id=a.crawl_run_id
WHERE a.tenant_id=UUID_TO_BIN(?) AND a.id=UUID_TO_BIN(?)`, tenantID, analysisRunID).Scan(&projectID, &crawlRunID)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),COALESCE(content_json->>'$.textSample','') FROM page_contents WHERE tenant_id=UUID_TO_BIN(?) AND crawl_run_id=UUID_TO_BIN(?) ORDER BY id LIMIT 3`, tenantID, crawlRunID)
	if err != nil {
		return Run{}, err
	}
	pages := []pageText{}
	for rows.Next() {
		var page pageText
		if err := rows.Scan(&page.id, &page.text); err != nil {
			rows.Close()
			return Run{}, err
		}
		if strings.TrimSpace(page.text) != "" {
			pages = append(pages, page)
		}
	}
	rows.Close()
	if len(pages) == 0 {
		return Run{}, ErrNoContent
	}
	if s.gateway == nil {
		return Run{}, errors.New("model gateway is not configured")
	}
	runID := uuid.NewString()
	model := "deepseek-flash"
	checks := []Check{}
	for _, page := range pages {
		prompt := "You are a conservative fact-checking assistant. Using ONLY the supplied page text, " +
			"extract up to 5 factual claims. For each claim return verdict supported|unsupported|unknown, " +
			"confidence 0..1, and an exact evidence quote. Never use outside knowledge. Return ONLY a JSON array " +
			"like [{\"claim\":\"...\",\"verdict\":\"unknown\",\"confidence\":0.5,\"evidence\":\"...\"}].\n\nPAGE TEXT:\n" +
			truncate(page.text, 6000)
		temperature := 0.0
		response, err := s.gateway.Chat(ctx, modelgateway.ChatRequest{
			Provider:    "deepseek",
			Messages:    []modelgateway.Message{{Role: "system", Content: "Return only valid JSON."}, {Role: "user", Content: prompt}},
			MaxTokens:   2048,
			Temperature: &temperature,
		})
		if err != nil {
			return Run{}, err
		}
		model = response.Model
		checks = append(checks, parseChecks(response.Content, page.id)...)
	}
	if len(checks) == 0 {
		return Run{}, ErrNoContent
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO fact_check_runs (id,tenant_id,project_id,analysis_run_id,status,model,created_at,finished_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'succeeded',?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, runID, tenantID, projectID, analysisRunID, model); err != nil {
		return Run{}, err
	}
	for _, check := range checks {
		check.ID = uuid.NewString()
		check.Status = "pending"
		if _, err := tx.ExecContext(ctx, `INSERT INTO fact_checks (id,tenant_id,project_id,run_id,page_content_id,claim,verdict,confidence,evidence,status,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,'pending',UTC_TIMESTAMP(6))`,
			check.ID, tenantID, projectID, runID, check.PageContentID, check.Claim, check.Verdict, check.Confidence, check.Evidence); err != nil {
			return Run{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, err
	}
	return Run{ID: runID, AnalysisID: analysisRunID, Status: "succeeded", Model: model, Checks: checks, CreatedAt: time.Now().UTC()}, nil
}

func parseChecks(content, pageID string) []Check {
	value := strings.TrimSpace(content)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	start := strings.Index(value, "[")
	end := strings.LastIndex(value, "]")
	if start < 0 || end <= start {
		return nil
	}
	var raw []struct {
		Claim      string  `json:"claim"`
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Evidence   string  `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(value[start:end+1]), &raw); err != nil {
		return nil
	}
	valid := map[string]bool{"supported": true, "unsupported": true, "unknown": true}
	result := []Check{}
	for _, item := range raw {
		claim := strings.TrimSpace(item.Claim)
		if claim == "" || !valid[item.Verdict] {
			continue
		}
		if item.Confidence < 0 {
			item.Confidence = 0
		}
		if item.Confidence > 1 {
			item.Confidence = 1
		}
		result = append(result, Check{PageContentID: pageID, Claim: claim, Verdict: item.Verdict, Confidence: item.Confidence, Evidence: strings.TrimSpace(item.Evidence)})
		if len(result) >= 5 {
			break
		}
	}
	return result
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// ListChecks returns fact checks for a project, optionally filtered by status.
func (s *Store) ListChecks(ctx context.Context, tenantID, projectID, status string, limit int) ([]Check, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	query := `SELECT BIN_TO_UUID(id),BIN_TO_UUID(page_content_id),claim,verdict,confidence,COALESCE(evidence,''),status
FROM fact_checks WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`
	args := []any{tenantID, projectID}
	if strings.TrimSpace(status) != "" {
		query += " AND status=?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Check{}
	for rows.Next() {
		var item Check
		if err := rows.Scan(&item.ID, &item.PageContentID, &item.Claim, &item.Verdict, &item.Confidence, &item.Evidence, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateCheckStatus confirms or dismisses a pending fact check.
func (s *Store) UpdateCheckStatus(ctx context.Context, tenantID, checkID, status string) (Check, error) {
	if status != "confirmed" && status != "dismissed" {
		return Check{}, errors.New("invalid fact check status")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE fact_checks SET status=? WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, status, tenantID, checkID)
	if err != nil {
		return Check{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return Check{}, ErrNotFound
	}
	var item Check
	if err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(page_content_id),claim,verdict,confidence,COALESCE(evidence,''),status FROM fact_checks WHERE id=UUID_TO_BIN(?)`, checkID).Scan(&item.ID, &item.PageContentID, &item.Claim, &item.Verdict, &item.Confidence, &item.Evidence, &item.Status); err != nil {
		return Check{}, err
	}
	return item, nil
}
