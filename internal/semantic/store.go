package semantic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/embeddings"
	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
	"github.com/google/uuid"
)

// ErrNotFound marks a missing audit or project.
var ErrNotFound = errors.New("audit was not found")

// ErrNoClaims marks an audit whose answers produced no verifiable claims.
var ErrNoClaims = errors.New("audit answers contain no verifiable claims")

// ErrNoEvidence marks a project without any usable evidence source.
var ErrNoEvidence = errors.New("no evidence source is available for this project")

// ChatClient is the subset of the model gateway the judge needs.
type ChatClient interface {
	Chat(ctx context.Context, input modelgateway.ChatRequest) (modelgateway.ChatResponse, error)
}

type Store struct {
	db         *sql.DB
	gateway    ChatClient
	embeddings *embeddings.Client
}

func NewStore(db *sql.DB, gateway ChatClient, embeddingClient *embeddings.Client) *Store {
	return &Store{db: db, gateway: gateway, embeddings: embeddingClient}
}

// VerifyAudit checks the claims made in an audit's answers against brand pages
// and imported third-party mentions.
func (s *Store) VerifyAudit(ctx context.Context, tenantID, auditID string) (Run, error) {
	if s.gateway == nil {
		return Run{}, errors.New("model gateway is not configured")
	}
	var projectID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id) FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, auditID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	claims, err := s.claims(ctx, tenantID, auditID)
	if err != nil {
		return Run{}, err
	}
	if len(claims) == 0 {
		return Run{}, ErrNoClaims
	}
	pool, err := s.evidencePool(ctx, tenantID, projectID)
	if err != nil {
		return Run{}, err
	}
	if len(pool) == 0 {
		return Run{}, ErrNoEvidence
	}

	claimTexts := make([]string, 0, len(claims))
	for _, claim := range claims {
		claimTexts = append(claimTexts, claim.Text)
	}
	claimVectors, evidenceVectors := s.embed(ctx, claimTexts, pool)

	model := "deepseek-flash"
	checks := make([]Check, 0, len(claims))
	for index, claim := range claims {
		scores := make([]float64, len(pool))
		for position, evidence := range pool {
			if len(claimVectors) > index && position < len(evidenceVectors) {
				scores[position] = Cosine(claimVectors[index], evidenceVectors[position])
			} else {
				scores[position] = LexicalScore(claim.Text, evidence.Text)
			}
		}
		ranked := RankEvidence(claim.Text, pool, scores, MaxEvidencePerClaim)
		if len(ranked) == 0 || !NeedsJudgement(ranked[0]) {
			checks = append(checks, Check{
				Claim: claim.Text, Verdict: VerdictUnknown, Confidence: 0.2,
				Similarity: ranked[0].Score, EvidenceSource: "none",
			})
			continue
		}
		verdict, confidence, quote, err := s.judge(ctx, claim.Text, ranked)
		if err != nil {
			return Run{}, err
		}
		checks = append(checks, Check{
			Claim: claim.Text, Verdict: verdict, Confidence: confidence,
			Similarity: ranked[0].Score, EvidenceSource: ranked[0].Source, EvidenceText: quote,
		})
	}
	return s.persist(ctx, tenantID, projectID, auditID, model, checks)
}

func (s *Store) claims(ctx context.Context, tenantID, auditID string) ([]Claim, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(r.content_text,'') FROM ai_responses r
JOIN audit_samples s ON s.id=r.sample_id
WHERE r.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?) ORDER BY s.sample_no LIMIT 20`, tenantID, auditID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claims := []Claim{}
	seen := map[string]bool{}
	index := 0
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		for _, text := range SplitClaims(content) {
			key := strings.ToLower(text)
			if seen[key] {
				continue
			}
			seen[key] = true
			claims = append(claims, Claim{AnswerIndex: index, Text: text})
			if len(claims) >= MaxClaims {
				return claims, nil
			}
		}
		index++
	}
	return claims, rows.Err()
}

func (s *Store) evidencePool(ctx context.Context, tenantID, projectID string) ([]Evidence, error) {
	pool := []Evidence{}
	pageRows, err := s.db.QueryContext(ctx, `SELECT pc.final_url,COALESCE(pc.content_json->>'$.textSample','')
FROM page_contents pc JOIN crawl_runs cr ON cr.id=pc.crawl_run_id
WHERE pc.tenant_id=UUID_TO_BIN(?) AND cr.project_id=UUID_TO_BIN(?)
ORDER BY pc.created_at DESC LIMIT 20`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	for pageRows.Next() {
		var url, text string
		if err := pageRows.Scan(&url, &text); err != nil {
			pageRows.Close()
			return nil, err
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		pool = append(pool, Evidence{Source: "brand_page", Ref: url, Text: truncate(text, 1200)})
	}
	pageRows.Close()

	mentionRows, err := s.db.QueryContext(ctx, `SELECT source_url,title,COALESCE(snippet,'') FROM source_mentions
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at DESC LIMIT 20`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	for mentionRows.Next() {
		var url, title, snippet string
		if err := mentionRows.Scan(&url, &title, &snippet); err != nil {
			mentionRows.Close()
			return nil, err
		}
		text := strings.TrimSpace(title + " " + snippet)
		if text == "" {
			continue
		}
		pool = append(pool, Evidence{Source: "third_party", Ref: url, Text: truncate(text, 400)})
	}
	mentionRows.Close()
	return pool, nil
}

func (s *Store) embed(ctx context.Context, claims []string, pool []Evidence) ([][]float64, [][]float64) {
	if s.embeddings == nil || !s.embeddings.Enabled() {
		return nil, nil
	}
	texts := append([]string{}, claims...)
	for _, evidence := range pool {
		texts = append(texts, evidence.Text)
	}
	vectors, err := s.embeddings.Embed(ctx, texts)
	if err != nil || len(vectors) != len(texts) {
		return nil, nil
	}
	return vectors[:len(claims)], vectors[len(claims):]
}

// judge asks the model to adjudicate one claim against retrieved passages only.
func (s *Store) judge(ctx context.Context, claim string, evidence []Evidence) (string, float64, string, error) {
	var builder strings.Builder
	for index, item := range evidence {
		builder.WriteString("[")
		builder.WriteString(string(rune('1' + index)))
		builder.WriteString("] ")
		builder.WriteString(item.Text)
		builder.WriteString("\n")
	}
	prompt := "You verify one claim against supplied evidence only. Never use outside knowledge.\n" +
		"Claim: " + claim + "\nEvidence:\n" + builder.String() +
		"\nDecide supported (evidence confirms), unsupported (evidence contradicts), or unknown (evidence insufficient).\n" +
		"Return ONLY JSON: {\"verdict\":\"supported|unsupported|unknown\",\"confidence\":0..1,\"evidence\":\"exact quote\"}"
	temperature := 0.0
	response, err := s.gateway.Chat(ctx, modelgateway.ChatRequest{
		Provider:    "deepseek",
		Messages:    []modelgateway.Message{{Role: "system", Content: "Return only valid JSON. Be conservative."}, {Role: "user", Content: prompt}},
		MaxTokens:   512,
		Temperature: &temperature,
	})
	if err != nil {
		return "", 0, "", err
	}
	var raw struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Evidence   string  `json:"evidence"`
	}
	content := strings.TrimSpace(response.Content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		_ = json.Unmarshal([]byte(content[start:end+1]), &raw)
	}
	confidence := raw.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return NormalizeVerdict(raw.Verdict), confidence, truncate(strings.TrimSpace(raw.Evidence), 400), nil
}

func (s *Store) persist(ctx context.Context, tenantID, projectID, auditID, model string, checks []Check) (Run, error) {
	runID := uuid.NewString()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO semantic_check_runs (id,tenant_id,project_id,audit_id,status,model,claim_count,created_at,finished_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'succeeded',?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		runID, tenantID, projectID, auditID, model, len(checks)); err != nil {
		return Run{}, err
	}
	for index := range checks {
		checks[index].ID = uuid.NewString()
		checks[index].Status = "pending"
		if _, err := tx.ExecContext(ctx, `INSERT INTO semantic_checks (id,tenant_id,project_id,run_id,audit_id,claim,verdict,confidence,similarity,evidence_source,evidence_text,status,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,'pending',UTC_TIMESTAMP(6))`,
			checks[index].ID, tenantID, projectID, runID, auditID, checks[index].Claim, checks[index].Verdict,
			checks[index].Confidence, checks[index].Similarity, checks[index].EvidenceSource, checks[index].EvidenceText); err != nil {
			return Run{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, err
	}
	return Run{ID: runID, AuditID: auditID, Status: "succeeded", Model: model, ClaimCount: len(checks), Checks: checks, CreatedAt: time.Now().UTC()}, nil
}

// ListByAudit returns the newest checks for one audit.
func (s *Store) ListByAudit(ctx context.Context, tenantID, auditID string) ([]Check, error) {
	return s.list(ctx, `tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?)`, tenantID, auditID)
}

// ListByProject returns the newest checks for a project, optionally filtered by status.
func (s *Store) ListByProject(ctx context.Context, tenantID, projectID, status string) ([]Check, error) {
	if status == "" {
		return s.list(ctx, `tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`, tenantID, projectID)
	}
	return s.list(ctx, `tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND status=?`, tenantID, projectID, status)
}

func (s *Store) list(ctx context.Context, where string, args ...any) ([]Check, error) {
	query := `SELECT BIN_TO_UUID(id),claim,verdict,confidence,similarity,evidence_source,COALESCE(evidence_text,''),status
FROM semantic_checks WHERE ` + where + ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Check{}
	for rows.Next() {
		var item Check
		if err := rows.Scan(&item.ID, &item.Claim, &item.Verdict, &item.Confidence, &item.Similarity, &item.EvidenceSource, &item.EvidenceText, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
