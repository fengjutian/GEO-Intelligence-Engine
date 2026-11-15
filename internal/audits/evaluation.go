package audits

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/fengjutian/geo-intelligence-engine/internal/reviews"
	"github.com/google/uuid"
)

// RecordEvaluation stores a judge result for one response and updates audit counters.
func (s *Store) RecordEvaluation(ctx context.Context, tenantID, auditID, responseID string, evaluation *Evaluation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := persistEvaluation(ctx, tx, tenantID, auditID, responseID, evaluation); err != nil {
		return err
	}
	return tx.Commit()
}

// persistEvaluation stores the judge run and answer evaluation, and counts eligible answers.
func persistEvaluation(ctx context.Context, tx *sql.Tx, tenantID, auditID, responseID string, evaluation *Evaluation) error {
	if evaluation == nil || evaluation.JudgeVersion == "" {
		return nil
	}
	ruleVersion := evaluation.EligibilityRuleVersion
	if ruleVersion == "" {
		ruleVersion = "eligibility-v1.0"
	}
	configHash := sha256.Sum256([]byte(evaluation.JudgeVersion + "|" + ruleVersion))
	runID, _ := uuid.NewV7()
	_, err := tx.ExecContext(ctx, `INSERT INTO evaluation_runs (id,tenant_id,audit_id,judge_version,eligibility_rule_version,config_hash,status,created_at,finished_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'succeeded',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE id=id`, runID.String(), tenantID, auditID, evaluation.JudgeVersion, ruleVersion, configHash[:])
	if err != nil {
		return fmt.Errorf("persist evaluation run: %w", err)
	}
	var resolvedRunID string
	if err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM evaluation_runs WHERE audit_id=UUID_TO_BIN(?) AND judge_version=? AND config_hash=?`, auditID, evaluation.JudgeVersion, configHash[:]).Scan(&resolvedRunID); err != nil {
		return fmt.Errorf("read evaluation run: %w", err)
	}
	evidence, _ := json.Marshal(map[string]any{"mentions": evaluation.Mentions})
	evaluationID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO answer_evaluations (id,tenant_id,response_id,evaluation_run_id,content_validity,answer_outcome,eligible_for_core_metrics,eligibility_rule_version,recommendation_label,sentiment,confidence,evidence_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,?,CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		evaluationID.String(), tenantID, responseID, resolvedRunID, evaluation.ContentValidity, evaluation.AnswerOutcome, evaluation.Eligible, ruleVersion, nullableString(evaluation.RecommendationLabel), nullableString(evaluation.Sentiment), evaluation.Confidence, evidence)
	if err != nil {
		return fmt.Errorf("persist answer evaluation: %w", err)
	}
	for _, citation := range evaluation.Citations {
		normalizedHash := sha256.Sum256([]byte(citation.NormalizedURL))
		citationID, _ := uuid.NewV7()
		if _, err := tx.ExecContext(ctx, `INSERT INTO citations (id,tenant_id,evaluation_id,raw_url,normalized_url,normalized_url_hash,domain,source_type,support_label,confidence,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,NULL,?,UTC_TIMESTAMP(6))`,
			citationID.String(), tenantID, evaluationID.String(), citation.RawURL, citation.NormalizedURL, normalizedHash[:], citation.Domain, citation.SourceType, citation.Confidence); err != nil {
			return fmt.Errorf("persist citation: %w", err)
		}
	}
	if evaluation.Eligible {
		if _, err := tx.ExecContext(ctx, `UPDATE audits SET eligible_count=eligible_count+1 WHERE id=UUID_TO_BIN(?)`, auditID); err != nil {
			return fmt.Errorf("increment eligible count: %w", err)
		}
	}
	reasonCode, priority := "", ""
	if !evaluation.Eligible {
		reasonCode, priority = "INELIGIBLE_ANSWER", "high"
	} else if evaluation.AnswerOutcome == "refusal" {
		reasonCode, priority = "REFUSAL", "medium"
	}
	if reasonCode != "" {
		if err := reviews.Enqueue(ctx, tx, tenantID, evaluationID.String(), reasonCode, priority); err != nil {
			return fmt.Errorf("enqueue review item: %w", err)
		}
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
