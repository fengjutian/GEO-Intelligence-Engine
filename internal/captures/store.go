package captures

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/fengjutian/geo-intelligence-engine/internal/judge"
	"github.com/google/uuid"
)

// ErrBatchInvalid marks a malformed batch envelope.
var ErrBatchInvalid = errors.New("capture batch is invalid")

// Capture is one stored observation.
type Capture struct {
	ID           string    `json:"id"`
	AuditID      string    `json:"auditId"`
	Channel      string    `json:"channel"`
	Provider     string    `json:"provider"`
	Product      string    `json:"product"`
	Model        string    `json:"model"`
	TrustTier    string    `json:"trustTier"`
	Method       string    `json:"method"`
	ObservedAt   time.Time `json:"observedAt"`
	Revoked      bool      `json:"revoked"`
	AnswerLength int       `json:"answerLength"`
}

type Store struct {
	db     *sql.DB
	judge  *judge.Client
	audits *audits.Store
}

func NewStore(db *sql.DB, judgeClient *judge.Client, auditStore *audits.Store) *Store {
	return &Store{db: db, judge: judgeClient, audits: auditStore}
}

// Submit stores a capture batch and evaluates each accepted answer.
func (s *Store) Submit(ctx context.Context, tenantID string, batch BatchRequest, rawCaptures []byte) (BatchResult, error) {
	if _, err := uuid.Parse(batch.BatchID); err != nil {
		return BatchResult{}, ErrBatchInvalid
	}
	items, err := DecodeCaptures(batch.Captures)
	if err != nil {
		return BatchResult{}, err
	}
	signatureVerified, signatureErr := VerifySignature(batch.Agent, rawCaptures)
	result := BatchResult{BatchID: batch.BatchID, Results: make([]ItemResult, 0, len(items))}
	acceptedAudits := map[string]bool{}
	for _, capture := range items {
		outcome := s.submitOne(ctx, tenantID, batch, capture, signatureVerified, signatureErr)
		if outcome.Status == "accepted" {
			acceptedAudits[capture.AuditID] = true
		}
		result.Results = append(result.Results, outcome)
	}
	// Captures are judged during submission; rebuilding the snapshot makes them
	// visible to metrics without a separate manual recompute call.
	if s.audits != nil {
		for auditID := range acceptedAudits {
			_, _ = s.audits.RecomputeMetrics(ctx, tenantID, auditID)
		}
	}
	return result, nil
}

func (s *Store) submitOne(ctx context.Context, tenantID string, batch BatchRequest, capture CaptureInput, signatureVerified bool, signatureErr error) ItemResult {
	outcome := ItemResult{CaptureID: capture.CaptureID}
	if err := ValidateCapture(capture, time.Now().UTC()); err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) {
			outcome.Status, outcome.Code, outcome.Message = "rejected", validation.Code, validation.Message
		} else {
			outcome.Status, outcome.Code, outcome.Message = "rejected", "INVALID_CAPTURE", err.Error()
		}
		return outcome
	}
	if RequiresSignature(capture.Provenance.Method) && (!signatureVerified || signatureErr != nil) {
		outcome.Status, outcome.Code = "rejected", "ATTESTATION_INVALID"
		outcome.Message = "signature is required and must verify for this method"
		return outcome
	}
	supported, err := s.surfaceSupported(ctx, capture.Surface.Provider, capture.Surface.Product)
	if err != nil {
		outcome.Status, outcome.Code = "rejected", "INTERNAL"
		return outcome
	}
	if !supported {
		outcome.Status, outcome.Code = "rejected", "SURFACE_UNSUPPORTED"
		outcome.Message = "surface.provider/product is not registered for capture"
		return outcome
	}

	var existing int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM capture_provenance WHERE tenant_id=UUID_TO_BIN(?) AND capture_id=UUID_TO_BIN(?)`, tenantID, capture.CaptureID).Scan(&existing); err != nil {
		outcome.Status, outcome.Code = "rejected", "INTERNAL"
		return outcome
	}
	if existing > 0 {
		outcome.Status = "duplicate"
		return outcome
	}

	var projectID, auditStatus string
	err = s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),status FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, capture.AuditID).Scan(&projectID, &auditStatus)
	if errors.Is(err, sql.ErrNoRows) {
		outcome.Status, outcome.Code = "rejected", "AUDIT_NOT_FOUND"
		return outcome
	}
	if err != nil {
		outcome.Status, outcome.Code = "rejected", "INTERNAL"
		return outcome
	}
	var questionCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM questions q JOIN audits a ON a.id=UUID_TO_BIN(?) AND a.tenant_id=UUID_TO_BIN(?)
WHERE q.id=UUID_TO_BIN(?) AND q.version_id=a.question_version_id`, capture.AuditID, tenantID, capture.QuestionID).Scan(&questionCount); err != nil || questionCount == 0 {
		outcome.Status, outcome.Code = "rejected", "QUESTION_NOT_IN_AUDIT"
		return outcome
	}

	engineConfigID, err := s.ensureEngineConfig(ctx, tenantID, capture)
	if err != nil {
		outcome.Status, outcome.Code = "rejected", "ENGINE_CONFIG_FAILED"
		return outcome
	}

	responseID, err := s.persist(ctx, tenantID, projectID, engineConfigID, batch, capture, signatureVerified)
	if err != nil {
		outcome.Status, outcome.Code = "rejected", "PERSIST_FAILED"
		outcome.Message = err.Error()
		return outcome
	}
	outcome.Status = "accepted"
	s.evaluate(ctx, tenantID, capture, responseID)
	return outcome
}

func (s *Store) ensureEngineConfig(ctx context.Context, tenantID string, capture CaptureInput) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM engine_configs
WHERE tenant_id=UUID_TO_BIN(?) AND provider=? AND channel=? ORDER BY created_at LIMIT 1`, tenantID, capture.Surface.Provider, capture.Channel).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO engine_configs (id,tenant_id,provider,channel,credential_ref,region,limits_json,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'capture://none',?,'{}','active',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		id, tenantID, capture.Surface.Provider, capture.Channel, capture.Surface.Region); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) persist(ctx context.Context, tenantID, projectID, engineConfigID string, batch BatchRequest, capture CaptureInput, signatureVerified bool) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var sampleNo int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sample_no),0)+1 FROM audit_samples WHERE audit_id=UUID_TO_BIN(?) AND question_id=UUID_TO_BIN(?) AND engine_config_id=UUID_TO_BIN(?)`,
		capture.AuditID, capture.QuestionID, engineConfigID).Scan(&sampleNo); err != nil {
		return "", err
	}
	model := capture.Surface.Model
	if model == "" {
		model = capture.Surface.Product
	}
	sampleKey := sha256.Sum256([]byte(fmt.Sprintf("capture|%s|%s|%s|%s|%s|%d", capture.AuditID, capture.QuestionID, engineConfigID, model, capture.Channel, sampleNo)))
	dimensions, _ := json.Marshal(map[string]any{"surface": capture.Surface, "method": capture.Provenance.Method})
	sampleID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_samples (id,tenant_id,audit_id,question_id,engine_config_id,model_selector,channel,region,language,sample_no,sample_key,status,attempt_count,dimensions_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,'succeeded',0,CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		sampleID, tenantID, capture.AuditID, capture.QuestionID, engineConfigID, model, capture.Channel, capture.Surface.Region, capture.Surface.Locale, sampleNo, sampleKey[:], dimensions); err != nil {
		return "", err
	}
	answerHash := sha256.Sum256([]byte(capture.Answer.Text))
	responseID := uuid.NewString()
	usage, _ := json.Marshal(map[string]any{"capture": true})
	if _, err := tx.ExecContext(ctx, `INSERT INTO ai_responses (id,tenant_id,sample_id,request_id,resolved_model,transport_status,raw_hash,usage_json,cost_microunits,finish_reason,content_text,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),NULL,?,'succeeded',?,CAST(? AS JSON),0,?,?,UTC_TIMESTAMP(6))`,
		responseID, tenantID, sampleID, model, answerHash[:], usage, capture.Answer.FinishReason, capture.Answer.Text); err != nil {
		return "", err
	}
	tier := TierOf(capture.Provenance.Method, capture.Provenance.ScreenshotSHA256 != "", signatureVerified)
	surface, _ := json.Marshal(capture.Surface)
	if _, err := tx.ExecContext(ctx, `INSERT INTO capture_provenance (id,tenant_id,project_id,audit_id,capture_id,response_id,batch_id,channel,surface_json,trust_tier,method,observed_at,session_hash,screenshot_sha256,consent_basis,consent_reference,agent_id,agent_version,signature_verified,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,CAST(? AS JSON),?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))`,
		uuid.NewString(), tenantID, projectID, capture.AuditID, capture.CaptureID, responseID, batch.BatchID, capture.Channel, surface, tier, capture.Provenance.Method,
		capture.ObservedAt.UTC(), capture.Provenance.SessionHash, capture.Provenance.ScreenshotSHA256, capture.Consent.Basis, capture.Consent.Reference,
		batch.Agent.ID, batch.Agent.Version, signatureVerified); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return responseID, nil
}

// evaluate judges the captured answer; failures do not roll back the capture.
func (s *Store) evaluate(ctx context.Context, tenantID string, capture CaptureInput, responseID string) {
	if s.judge == nil || s.audits == nil {
		return
	}
	var question, brandName, brandDomain, language string
	_ = s.db.QueryRowContext(ctx, `SELECT q.text_value,COALESCE(b.canonical_name,''),COALESCE(b.official_domain,''),COALESCE(q.language,'zh-CN')
FROM questions q
JOIN audits a ON a.id=UUID_TO_BIN(?) AND a.question_version_id=q.version_id
JOIN brands b ON b.tenant_id=a.tenant_id AND b.project_id=a.project_id
WHERE q.id=UUID_TO_BIN(?) LIMIT 1`, capture.AuditID, capture.QuestionID).Scan(&question, &brandName, &brandDomain, &language)
	if question == "" {
		slog.Default().Warn("capture judge skipped", "reason", "question lookup empty", "capture", capture.CaptureID)
		return
	}
	result, err := s.judge.Evaluate(ctx, judge.Input{
		Question: question, Answer: capture.Answer.Text, BrandName: brandName, BrandDomain: brandDomain,
		Language: language, TransportStatus: "succeeded", FinishReason: capture.Answer.FinishReason,
	})
	if err != nil {
		slog.Default().Error("capture judge failed", "error", err, "capture", capture.CaptureID)
		return
	}
	evaluation := &audits.Evaluation{
		ContentValidity: result.ContentValidity, AnswerOutcome: result.AnswerOutcome,
		RecommendationLabel: result.RecommendationLabel, Sentiment: result.Sentiment,
		Confidence: result.Confidence, Eligible: result.EligibleForCoreMetrics,
		JudgeVersion: result.JudgeVersion, EligibilityRuleVersion: result.EligibilityRuleVersion,
	}
	for _, mention := range result.Mentions {
		evaluation.Mentions = append(evaluation.Mentions, audits.EvaluationMention{Text: mention.Text, Start: mention.Start, End: mention.End})
	}
	for _, citation := range result.Citations {
		evaluation.Citations = append(evaluation.Citations, audits.EvaluationCitation{
			RawURL: citation.RawURL, NormalizedURL: citation.NormalizedURL, Domain: citation.Domain,
			SourceType: citation.SourceType, Confidence: citation.Confidence,
		})
	}
	_ = s.audits.RecordEvaluation(ctx, tenantID, capture.AuditID, responseID, evaluation)
}

// ListByAudit returns captures newest first.
func (s *Store) ListByAudit(ctx context.Context, tenantID, auditID string) ([]Capture, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(cp.capture_id),BIN_TO_UUID(cp.audit_id),cp.channel,cp.surface_json,cp.trust_tier,cp.method,cp.observed_at,cp.revoked_at IS NOT NULL,CHAR_LENGTH(COALESCE(ar.content_text,''))
FROM capture_provenance cp JOIN ai_responses ar ON ar.id=cp.response_id
WHERE cp.tenant_id=UUID_TO_BIN(?) AND cp.audit_id=UUID_TO_BIN(?) ORDER BY cp.observed_at DESC LIMIT 200`, tenantID, auditID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Capture{}
	for rows.Next() {
		var item Capture
		var surface []byte
		if err := rows.Scan(&item.ID, &item.AuditID, &item.Channel, &surface, &item.TrustTier, &item.Method, &item.ObservedAt, &item.Revoked, &item.AnswerLength); err != nil {
			return nil, err
		}
		var parsed Surface
		_ = json.Unmarshal(surface, &parsed)
		item.Provider, item.Product, item.Model = parsed.Provider, parsed.Product, parsed.Model
		items = append(items, item)
	}
	return items, rows.Err()
}

// Revoke soft-deletes a capture and marks the response cancelled.
func (s *Store) Revoke(ctx context.Context, tenantID, captureID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return invalid("REVOKE_REASON_REQUIRED", "reason is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var responseID string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(response_id) FROM capture_provenance
WHERE tenant_id=UUID_TO_BIN(?) AND capture_id=UUID_TO_BIN(?) AND revoked_at IS NULL FOR UPDATE`, tenantID, captureID).Scan(&responseID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBatchInvalid
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE capture_provenance SET revoked_at=UTC_TIMESTAMP(6) WHERE tenant_id=UUID_TO_BIN(?) AND capture_id=UUID_TO_BIN(?)`, tenantID, captureID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_responses SET transport_status='cancelled' WHERE id=UUID_TO_BIN(?)`, responseID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListByBatch returns the captures accepted in one batch.
func (s *Store) ListByBatch(ctx context.Context, tenantID, batchID string) ([]Capture, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(cp.capture_id),BIN_TO_UUID(cp.audit_id),cp.channel,cp.surface_json,cp.trust_tier,cp.method,cp.observed_at,cp.revoked_at IS NOT NULL,CHAR_LENGTH(COALESCE(ar.content_text,''))
FROM capture_provenance cp JOIN ai_responses ar ON ar.id=cp.response_id
WHERE cp.tenant_id=UUID_TO_BIN(?) AND cp.batch_id=UUID_TO_BIN(?) ORDER BY cp.observed_at DESC LIMIT 200`, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Capture{}
	for rows.Next() {
		var item Capture
		var surface []byte
		if err := rows.Scan(&item.ID, &item.AuditID, &item.Channel, &surface, &item.TrustTier, &item.Method, &item.ObservedAt, &item.Revoked, &item.AnswerLength); err != nil {
			return nil, err
		}
		var parsed Surface
		_ = json.Unmarshal(surface, &parsed)
		item.Provider, item.Product, item.Model = parsed.Provider, parsed.Product, parsed.Model
		items = append(items, item)
	}
	return items, rows.Err()
}

// SurfaceEntry is one registered capture surface.
type SurfaceEntry struct {
	Provider string `json:"provider"`
	Product  string `json:"product"`
	Enabled  bool   `json:"enabled"`
}

// surfaceSupported reports whether the provider/product pair may be captured.
func (s *Store) surfaceSupported(ctx context.Context, provider, product string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM capture_surfaces WHERE provider=? AND product=? AND enabled=TRUE`,
		strings.ToLower(strings.TrimSpace(provider)), strings.ToLower(strings.TrimSpace(product))).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListSurfaces returns the capture surface registry.
func (s *Store) ListSurfaces(ctx context.Context) ([]SurfaceEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider,product,enabled FROM capture_surfaces ORDER BY provider,product`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SurfaceEntry{}
	for rows.Next() {
		var item SurfaceEntry
		if err := rows.Scan(&item.Provider, &item.Product, &item.Enabled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// AddSurface registers or re-enables a capture surface.
func (s *Store) AddSurface(ctx context.Context, provider, product string) (SurfaceEntry, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	product = strings.ToLower(strings.TrimSpace(product))
	if provider == "" || product == "" {
		return SurfaceEntry{}, invalid("SURFACE_INVALID", "provider and product are required")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO capture_surfaces (id,provider,product,enabled,created_at)
VALUES (UUID_TO_BIN(UUID()),?,?,TRUE,UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE enabled=TRUE`, provider, product); err != nil {
		return SurfaceEntry{}, err
	}
	return SurfaceEntry{Provider: provider, Product: product, Enabled: true}, nil
}
