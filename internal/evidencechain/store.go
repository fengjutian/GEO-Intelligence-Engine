package evidencechain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound         = errors.New("evidence chain not found")
	ErrInvalidReference = errors.New("evidence chain reference is outside the finding project")
	ErrInvalidInput     = errors.New("invalid evidence chain input")
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func NormalizeInput(input UpsertInput) (UpsertInput, error) {
	input.QuestionID = strings.TrimSpace(input.QuestionID)
	input.ResponseID = strings.TrimSpace(input.ResponseID)
	input.PageContentID = strings.TrimSpace(input.PageContentID)
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.RetestAuditID = strings.TrimSpace(input.RetestAuditID)
	input.ChangeSummary = strings.TrimSpace(input.ChangeSummary)
	input.ExpectedOutcome = strings.TrimSpace(input.ExpectedOutcome)
	input.VerificationPlan = strings.TrimSpace(input.VerificationPlan)
	input.MetricCode = strings.TrimSpace(input.MetricCode)
	if input.MetricCode == "" {
		input.MetricCode = "mention_rate"
	}
	if input.TargetDelta == 0 {
		input.TargetDelta = 0.01
	}
	if input.QuestionID == "" || input.ResponseID == "" || input.ChangeSummary == "" || input.ExpectedOutcome == "" || input.VerificationPlan == "" {
		return UpsertInput{}, ErrInvalidInput
	}
	switch input.MetricCode {
	case "mention_rate", "recommendation_rate", "positive_mention_rate", "official_citation_rate", "third_party_citation_rate", "eligible_response_rate", "official_citation_word_count", "official_citation_word_share", "official_position_adjusted_word_share", "official_first_citation_sentence_index":
	default:
		return UpsertInput{}, ErrInvalidInput
	}
	if input.TargetDelta < 0 || input.TargetDelta > 1 {
		return UpsertInput{}, ErrInvalidInput
	}
	return input, nil
}

func (s *Store) Upsert(ctx context.Context, tenantID, actorID, findingID string, input UpsertInput) (Chain, error) {
	input, err := NormalizeInput(input)
	if err != nil {
		return Chain{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Chain{}, err
	}
	defer tx.Rollback()
	var projectID, sourceType, sourceID string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),source_type,BIN_TO_UUID(source_id)
FROM findings WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, findingID).Scan(&projectID, &sourceType, &sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return Chain{}, ErrNotFound
	}
	if err != nil {
		return Chain{}, fmt.Errorf("load finding: %w", err)
	}
	if input.PageContentID == "" && sourceType == "page_content" {
		input.PageContentID = sourceID
	}
	if input.PageContentID == "" {
		return Chain{}, ErrInvalidInput
	}
	if err := validateReferences(ctx, tx, tenantID, projectID, input); err != nil {
		return Chain{}, err
	}
	snapshot, err := loadEvidenceSnapshot(ctx, tx, tenantID, input.QuestionID, input.ResponseID, input.PageContentID, input.MetricCode)
	if err != nil {
		return Chain{}, err
	}
	snapshotJSON, _ := json.Marshal(snapshot)
	var previousStatus string
	_ = tx.QueryRowContext(ctx, `SELECT status FROM finding_evidence_chains WHERE tenant_id=UUID_TO_BIN(?) AND finding_id=UUID_TO_BIN(?)`, tenantID, findingID).Scan(&previousStatus)
	if previousStatus != "" && previousStatus != StatusEvidenceReady {
		return Chain{}, ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO finding_evidence_chains
(finding_id,tenant_id,project_id,status,question_id,response_id,page_content_id,task_id,retest_audit_id,change_summary,expected_outcome,verification_plan,metric_code,target_delta,evidence_snapshot_json,retest_outcome,decision_source,created_by,updated_by,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'evidence_ready',UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(NULLIF(?,'')),UUID_TO_BIN(NULLIF(?,'')),?,?,?,?,?,CAST(? AS JSON),'pending','pending',UUID_TO_BIN(?),UUID_TO_BIN(?),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE question_id=VALUES(question_id),response_id=VALUES(response_id),page_content_id=VALUES(page_content_id),task_id=COALESCE(VALUES(task_id),task_id),retest_audit_id=VALUES(retest_audit_id),change_summary=VALUES(change_summary),expected_outcome=VALUES(expected_outcome),verification_plan=VALUES(verification_plan),metric_code=VALUES(metric_code),target_delta=VALUES(target_delta),evidence_snapshot_json=VALUES(evidence_snapshot_json),comparison_json=NULL,retest_outcome='pending',decision_source='pending',decision_reason=NULL,version=version+1,updated_by=VALUES(updated_by),updated_at=VALUES(updated_at)`,
		findingID, tenantID, projectID, input.QuestionID, input.ResponseID, input.PageContentID, input.TaskID, input.RetestAuditID,
		input.ChangeSummary, input.ExpectedOutcome, input.VerificationPlan, input.MetricCode, input.TargetDelta, snapshotJSON, actorID, actorID)
	if err != nil {
		return Chain{}, fmt.Errorf("upsert evidence chain: %w", err)
	}
	if err := appendChainEvent(ctx, tx, tenantID, findingID, actorID, map[bool]string{true: "updated", false: "created"}[previousStatus != ""], previousStatus, map[bool]string{true: previousStatus, false: StatusEvidenceReady}[previousStatus != ""], "", nil); err != nil {
		return Chain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Chain{}, err
	}
	return s.Get(ctx, tenantID, findingID)
}

func validateReferences(ctx context.Context, tx *sql.Tx, tenantID, projectID string, input UpsertInput) error {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_responses r
JOIN audit_samples s ON s.id=r.sample_id
JOIN audits a ON a.id=s.audit_id
WHERE r.tenant_id=UUID_TO_BIN(?) AND r.id=UUID_TO_BIN(?) AND s.question_id=UUID_TO_BIN(?) AND a.project_id=UUID_TO_BIN(?)`,
		tenantID, input.ResponseID, input.QuestionID, projectID).Scan(&count)
	if err != nil || count != 1 {
		return ErrInvalidReference
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM page_contents pc
JOIN crawl_runs cr ON cr.id=pc.crawl_run_id
WHERE pc.tenant_id=UUID_TO_BIN(?) AND pc.id=UUID_TO_BIN(?) AND cr.project_id=UUID_TO_BIN(?)`,
		tenantID, input.PageContentID, projectID).Scan(&count)
	if err != nil || count != 1 {
		return ErrInvalidReference
	}
	if input.TaskID != "" {
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimization_tasks WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`, tenantID, input.TaskID, projectID).Scan(&count)
		if err != nil || count != 1 {
			return ErrInvalidReference
		}
	}
	if input.RetestAuditID != "" {
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`, tenantID, input.RetestAuditID, projectID).Scan(&count)
		if err != nil || count != 1 {
			return ErrInvalidReference
		}
	}
	return nil
}

func (s *Store) Get(ctx context.Context, tenantID, findingID string) (Chain, error) {
	var chain Chain
	var taskID, taskTitle, taskStatus sql.NullString
	var retestID, retestStatus sql.NullString
	var retestOutcome string
	var snapshotJSON, comparisonJSON []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(c.finding_id),BIN_TO_UUID(c.project_id),c.status,c.version,
BIN_TO_UUID(t.id),t.title,t.status,c.change_summary,c.expected_outcome,c.verification_plan,c.metric_code,c.target_delta,c.evidence_snapshot_json,c.comparison_json,
BIN_TO_UUID(a.id),a.status,c.retest_outcome,c.created_at,c.updated_at
FROM finding_evidence_chains c
LEFT JOIN optimization_tasks t ON t.id=c.task_id
LEFT JOIN audits a ON a.id=c.retest_audit_id
WHERE c.tenant_id=UUID_TO_BIN(?) AND c.finding_id=UUID_TO_BIN(?)`, tenantID, findingID).Scan(
		&chain.FindingID, &chain.ProjectID, &chain.Status, &chain.Version,
		&taskID, &taskTitle, &taskStatus, &chain.ChangeSummary, &chain.ExpectedOutcome, &chain.VerificationPlan, &chain.MetricCode, &chain.TargetDelta, &snapshotJSON, &comparisonJSON,
		&retestID, &retestStatus, &retestOutcome, &chain.CreatedAt, &chain.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Chain{}, ErrNotFound
	}
	if err != nil {
		return Chain{}, fmt.Errorf("get evidence chain: %w", err)
	}
	if err := json.Unmarshal(snapshotJSON, &chain.Snapshot); err != nil {
		return Chain{}, fmt.Errorf("decode evidence snapshot: %w", err)
	}
	chain.Question, chain.Answer, chain.Page = chain.Snapshot.Question, chain.Snapshot.Answer, chain.Snapshot.Page
	if len(comparisonJSON) > 0 && string(comparisonJSON) != "null" {
		var comparison Comparison
		if err := json.Unmarshal(comparisonJSON, &comparison); err != nil {
			return Chain{}, fmt.Errorf("decode comparison: %w", err)
		}
		chain.Comparison = &comparison
	}
	if taskID.Valid {
		chain.Task = &TaskEvidence{ID: taskID.String, Title: taskTitle.String, Status: taskStatus.String}
	}
	if retestID.Valid {
		chain.Retest = &RetestEvidence{AuditID: retestID.String, Status: retestStatus.String, Outcome: retestOutcome}
	}
	chain.Events, err = s.listEvents(ctx, tenantID, findingID)
	if err != nil {
		return Chain{}, err
	}
	return chain, nil
}
