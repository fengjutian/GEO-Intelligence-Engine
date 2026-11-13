package audits

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

// SampleClaim carries everything a model worker needs to execute one sample.
type SampleClaim struct {
	SampleID              string
	AuditID               string
	ProjectID             string
	QuestionID            string
	QuestionText          string
	Language              string
	Region                string
	Provider              string
	ModelSelector         string
	Channel               string
	CostMicrounits        int64
	Currency              string
	BrandName             string
	BrandDomain           string
	BrandAliases          []string
	PromptTemplateVersion string
}

// SampleResult is the normalized outcome of one provider call.
type SampleResult struct {
	SampleID          string
	AuditID           string
	Provider          string
	ResolvedModel     string
	ProviderRequestID string
	Content           string
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	FinishReason      string
	LatencyMS         int64
	CostMicrounits    int64
	Currency          string
	RequestHash       string
	RawHash           string
	Evaluation        *Evaluation
}

// Evaluation is the Answer Judge output attached to one model response.
type Evaluation struct {
	ContentValidity        string
	AnswerOutcome          string
	RecommendationLabel    string
	Sentiment              string
	Confidence             float64
	Eligible               bool
	JudgeVersion           string
	EligibilityRuleVersion string
	Mentions               []EvaluationMention
}

// EvaluationMention is one brand occurrence in the answer.
type EvaluationMention struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// SampleFailure records an execution error and retries once before failing the sample.
type SampleFailure struct {
	SampleID  string
	AuditID   string
	ErrorCode string
}

func (s *Store) ClaimSample(ctx context.Context, workerID string) (SampleClaim, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SampleClaim{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE audit_samples s JOIN audits a ON a.id=s.audit_id
SET s.status='queued',s.lease_owner=NULL,s.lease_expires_at=NULL
WHERE s.status='running' AND s.lease_expires_at IS NOT NULL AND s.lease_expires_at < UTC_TIMESTAMP(6) AND a.status IN ('queued','running')`)
	if err != nil {
		return SampleClaim{}, fmt.Errorf("reclaim expired sample leases: %w", err)
	}
	var claim SampleClaim
	var aliasesJSON []byte
	err = tx.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(s.id),BIN_TO_UUID(s.audit_id),BIN_TO_UUID(a.project_id),BIN_TO_UUID(s.question_id),
       q.text_value,s.language,COALESCE(s.region,''),ec.provider,s.model_selector,s.channel,
       CAST(JSON_UNQUOTE(JSON_EXTRACT(mp.config_json,'$.estimatedCostPerSample')) AS SIGNED),
       a.currency,
       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(ks.content_json,'$.brand.canonicalName')),''),
       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(ks.content_json,'$.brand.officialDomain')),''),
       COALESCE(JSON_EXTRACT(ks.content_json,'$.aliases'),JSON_ARRAY()),
       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(mp.config_json,'$.promptTemplateVersion')),'prompt-v1')
FROM audit_samples s
JOIN audits a ON a.id=s.audit_id AND a.tenant_id=s.tenant_id
JOIN questions q ON q.id=s.question_id
JOIN engine_configs ec ON ec.id=s.engine_config_id
JOIN measurement_protocols mp ON mp.id=a.protocol_id
LEFT JOIN knowledge_snapshots ks ON ks.id=a.knowledge_snapshot_id
WHERE s.status='queued' AND a.status IN ('queued','running')
ORDER BY s.created_at,s.id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(
		&claim.SampleID, &claim.AuditID, &claim.ProjectID, &claim.QuestionID,
		&claim.QuestionText, &claim.Language, &claim.Region, &claim.Provider, &claim.ModelSelector, &claim.Channel,
		&claim.CostMicrounits, &claim.Currency, &claim.BrandName, &claim.BrandDomain, &aliasesJSON, &claim.PromptTemplateVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return SampleClaim{}, ErrNoWork
	}
	if err != nil {
		return SampleClaim{}, fmt.Errorf("select audit sample: %w", err)
	}
	var aliases []map[string]any
	if len(aliasesJSON) > 0 && json.Unmarshal(aliasesJSON, &aliases) == nil {
		for _, entry := range aliases {
			if alias, ok := entry["alias"].(string); ok && strings.TrimSpace(alias) != "" {
				claim.BrandAliases = append(claim.BrandAliases, alias)
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE audit_samples SET status='running',lease_owner=?,lease_expires_at=UTC_TIMESTAMP(6)+INTERVAL 2 MINUTE WHERE id=UUID_TO_BIN(?)`, workerID, claim.SampleID)
	if err != nil {
		return SampleClaim{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE audits SET status='running',started_at=COALESCE(started_at,UTC_TIMESTAMP(6)) WHERE id=UUID_TO_BIN(?) AND status='queued'`, claim.AuditID)
	if err != nil {
		return SampleClaim{}, err
	}
	if err := tx.Commit(); err != nil {
		return SampleClaim{}, err
	}
	return claim, nil
}

func (s *Store) CompleteSample(ctx context.Context, result SampleResult) error {
	requestHash, err := decodeHash(result.RequestHash)
	if err != nil {
		return ErrInvalidInput
	}
	rawHash, err := decodeHash(result.RawHash)
	if err != nil {
		return ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var auditStatus, sampleStatus, tenantID string
	err = tx.QueryRowContext(ctx, `SELECT a.status,s.status,BIN_TO_UUID(a.tenant_id)
FROM audits a JOIN audit_samples s ON s.audit_id=a.id
WHERE s.id=UUID_TO_BIN(?) AND a.id=UUID_TO_BIN(?) FOR UPDATE`, result.SampleID, result.AuditID).Scan(&auditStatus, &sampleStatus, &tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if sampleStatus == "succeeded" || sampleStatus == "cancelled" || sampleStatus == "failed" {
		if err := tx.Commit(); err != nil {
			return err
		}
		return nil
	}
	if auditStatus == "cancelled" {
		if err := settleSampleCost(ctx, tx, tenantID, result.AuditID, result.CostMicrounits); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE audit_samples SET status='cancelled',lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?)`, result.SampleID); err != nil {
			return err
		}
		return tx.Commit()
	}
	requestID, _ := uuid.NewV7()
	responseID, _ := uuid.NewV7()
	usage, _ := json.Marshal(map[string]any{"promptTokens": result.PromptTokens, "completionTokens": result.CompletionTokens, "totalTokens": result.TotalTokens})
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_requests (id,tenant_id,sample_id,provider,request_object_key,request_hash,parameters_json,idempotency_key,sent_at,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,JSON_OBJECT(),?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		requestID.String(), tenantID, result.SampleID, result.Provider, "db://prompt/"+requestID.String(), requestHash, result.SampleID)
	if err != nil {
		return fmt.Errorf("persist model request: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_responses (id,tenant_id,sample_id,request_id,provider_request_id,resolved_model,transport_status,raw_object_key,raw_hash,usage_json,cost_microunits,currency,finish_reason,latency_ms,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'succeeded',?,?,CAST(? AS JSON),?,?,?,?,UTC_TIMESTAMP(6))`,
		responseID.String(), tenantID, result.SampleID, requestID.String(), result.ProviderRequestID, result.ResolvedModel, "db://raw/"+responseID.String(), rawHash, usage, result.CostMicrounits, result.Currency, result.FinishReason, result.LatencyMS)
	if err != nil {
		return fmt.Errorf("persist model response: %w", err)
	}
	if err := persistEvaluation(ctx, tx, tenantID, result.AuditID, responseID.String(), result.Evaluation); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE audit_samples SET status='succeeded',lease_owner=NULL,lease_expires_at=NULL,last_error_code=NULL WHERE id=UUID_TO_BIN(?)`, result.SampleID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE audits SET completed_count=completed_count+1 WHERE id=UUID_TO_BIN(?)`, result.AuditID); err != nil {
		return err
	}
	if err := settleSampleCost(ctx, tx, tenantID, result.AuditID, result.CostMicrounits); err != nil {
		return err
	}
	completed, err := finalizeAudit(ctx, tx, result.AuditID)
	if err != nil {
		return err
	}
	if completed {
		if err := releaseAuditReservation(ctx, tx, tenantID, result.AuditID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) FailSample(ctx context.Context, failure SampleFailure) error {
	code := sanitizeErrorCode(failure.ErrorCode)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var auditStatus, sampleStatus, tenantID string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT a.status,s.status,s.attempt_count,BIN_TO_UUID(a.tenant_id)
FROM audits a JOIN audit_samples s ON s.audit_id=a.id
WHERE s.id=UUID_TO_BIN(?) AND a.id=UUID_TO_BIN(?) FOR UPDATE`, failure.SampleID, failure.AuditID).Scan(&auditStatus, &sampleStatus, &attempts, &tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if sampleStatus == "succeeded" || sampleStatus == "cancelled" || sampleStatus == "failed" {
		return tx.Commit()
	}
	if auditStatus == "cancelled" {
		if _, err := tx.ExecContext(ctx, `UPDATE audit_samples SET status='cancelled',lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?)`, failure.SampleID); err != nil {
			return err
		}
		return tx.Commit()
	}
	attempts++
	if attempts < 2 {
		if _, err := tx.ExecContext(ctx, `UPDATE audit_samples SET status='queued',attempt_count=?,last_error_code=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?)`, attempts, code, failure.SampleID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE audit_samples SET status='failed',attempt_count=?,last_error_code=?,lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?)`, attempts, code, failure.SampleID); err != nil {
		return err
	}
	completed, err := finalizeAudit(ctx, tx, failure.AuditID)
	if err != nil {
		return err
	}
	if completed {
		if err := releaseAuditReservation(ctx, tx, tenantID, failure.AuditID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func finalizeAudit(ctx context.Context, tx *sql.Tx, auditID string) (bool, error) {
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_samples WHERE audit_id=UUID_TO_BIN(?) AND status IN ('queued','running')`, auditID).Scan(&remaining); err != nil {
		return false, err
	}
	if remaining > 0 {
		_, err := tx.ExecContext(ctx, `UPDATE audits SET status='running' WHERE id=UUID_TO_BIN(?) AND status='queued'`, auditID)
		return false, err
	}
	var total, failed int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(status='failed'),0) FROM audit_samples WHERE audit_id=UUID_TO_BIN(?)`, auditID).Scan(&total, &failed); err != nil {
		return false, err
	}
	status := "succeeded"
	switch {
	case total == 0:
		status = "failed"
	case failed == total:
		status = "failed"
	case failed > 0:
		status = "partial_success"
	}
	_, err := tx.ExecContext(ctx, `UPDATE audits SET status=?,finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, status, auditID)
	return true, err
}

func decodeHash(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("invalid hash")
	}
	return decoded, nil
}

func sanitizeErrorCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "MODEL_EXECUTION_FAILED"
	}
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' {
			builder.WriteRune(char)
		}
	}
	if builder.Len() == 0 {
		return "MODEL_EXECUTION_FAILED"
	}
	if builder.Len() > 64 {
		return builder.String()[:64]
	}
	return builder.String()
}
