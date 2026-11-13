package audits

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

func (s *Store) CreateAudit(ctx context.Context, tenantID, projectID, protocolID, idempotencyKey string) (Audit, error) {
	if idempotencyKey == "" {
		return Audit{}, ErrInvalidInput
	}
	requestHash := hashRequest(projectID, protocolID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Audit{}, err
	}
	defer tx.Rollback()

	var existingID, existingHash string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),COALESCE(LOWER(HEX(request_hash)),'') FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND idempotency_key=? LIMIT 1`, tenantID, idempotencyKey).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != hex.EncodeToString(requestHash) {
			return Audit{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return Audit{}, err
		}
		return s.GetAudit(ctx, tenantID, existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Audit{}, err
	}

	var protocolProject, status string
	var configJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),status,config_json FROM measurement_protocols WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, protocolID).Scan(&protocolProject, &status, &configJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Audit{}, ErrNotFound
	}
	if err != nil {
		return Audit{}, err
	}
	if protocolProject != projectID {
		return Audit{}, ErrNotFound
	}
	if status != "published" {
		return Audit{}, ErrInvalidInput
	}
	var projectStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM projects WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID).Scan(&projectStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Audit{}, ErrNotFound
		}
		return Audit{}, err
	}
	if projectStatus == "archived" {
		return Audit{}, ErrArchived
	}
	var config map[string]any
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return Audit{}, err
	}
	questionVersionID := configString(config, "questionVersionId")
	knowledgeSnapshotID := configString(config, "knowledgeSnapshotId")
	engines := configEngines(config)
	samples := configInt(config, "samplesPerQuestion")
	if samples < 1 {
		samples = 1
	}
	budget := int64(configInt(config, "budgetMicrounits"))
	currency := configString(config, "currency")
	region := configString(config, "region")
	language := configString(config, "language")
	if knowledgeSnapshotID == "" || questionVersionID == "" || len(engines) == 0 {
		return Audit{}, ErrInvalidInput
	}

	questionRows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(id) FROM questions WHERE version_id=UUID_TO_BIN(?) ORDER BY created_at,id`, questionVersionID)
	if err != nil {
		return Audit{}, err
	}
	questionIDs := []string{}
	for questionRows.Next() {
		var questionID string
		if err := questionRows.Scan(&questionID); err != nil {
			questionRows.Close()
			return Audit{}, err
		}
		questionIDs = append(questionIDs, questionID)
	}
	questionRows.Close()
	if len(questionIDs) == 0 {
		return Audit{}, ErrInvalidInput
	}
	planned := len(questionIDs) * len(engines) * samples
	if planned > MaxExpandedSamples {
		return Audit{}, ErrPlanTooLarge
	}
	estimatedCost := int64(planned) * int64(configInt(config, "estimatedCostPerSample"))
	if budget > 0 && estimatedCost > budget {
		return Audit{}, ErrBudgetExceeded
	}
	auditID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO audits (id,tenant_id,project_id,protocol_id,question_version_id,knowledge_snapshot_id,status,planned_count,completed_count,eligible_count,budget_microunits,currency,idempotency_key,request_hash,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'queued',?,0,0,?,?,?,?,UTC_TIMESTAMP(6))`,
		auditID.String(), tenantID, projectID, protocolID, questionVersionID, knowledgeSnapshotID, planned, budget, currency, idempotencyKey, requestHash)
	if err != nil {
		return Audit{}, fmt.Errorf("insert audit: %w", err)
	}
	if err := ensureAuditBudget(ctx, tx, tenantID, auditID.String(), budget, estimatedCost, currency); err != nil {
		return Audit{}, err
	}

	statement, err := tx.PrepareContext(ctx, `INSERT INTO audit_samples (id,tenant_id,audit_id,question_id,engine_config_id,model_selector,channel,region,language,sample_no,sample_key,status,dimensions_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,'queued',CAST(? AS JSON),UTC_TIMESTAMP(6))`)
	if err != nil {
		return Audit{}, err
	}
	defer statement.Close()
	for _, engine := range engines {
		engineConfigID := configString(engine, "engineConfigId")
		modelSelector := configString(engine, "modelSelector")
		channel := configString(engine, "channel")
		parameters, _ := json.Marshal(engine["parameters"])
		parametersHash := sha256.Sum256(parameters)
		for sampleNo := 1; sampleNo <= samples; sampleNo++ {
			for _, questionID := range questionIDs {
				sampleKey := auditSampleKey(auditID.String(), questionID, engineConfigID, modelSelector, channel, region, language, hex.EncodeToString(parametersHash[:]), sampleNo)
				sampleID, _ := uuid.NewV7()
				dimensions, _ := json.Marshal(map[string]any{"questionId": questionID, "engineConfigId": engineConfigID, "modelSelector": modelSelector, "channel": channel, "region": region, "language": language, "sampleNo": sampleNo})
				if _, err := statement.ExecContext(ctx, sampleID.String(), tenantID, auditID.String(), questionID, engineConfigID, modelSelector, channel, region, language, sampleNo, sampleKey[:], dimensions); err != nil {
					return Audit{}, fmt.Errorf("expand audit sample: %w", err)
				}
			}
		}
	}
	outboxID, _ := uuid.NewV7()
	payload, _ := json.Marshal(map[string]any{"auditId": auditID.String(), "plannedCount": planned})
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events (id,tenant_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'audit.queued','audit',UUID_TO_BIN(?),CAST(? AS JSON),UTC_TIMESTAMP(6))`, outboxID.String(), tenantID, auditID.String(), payload)
	if err != nil {
		return Audit{}, fmt.Errorf("enqueue audit event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Audit{}, err
	}
	return s.GetAudit(ctx, tenantID, auditID.String())
}

func (s *Store) GetAudit(ctx context.Context, tenantID, auditID string) (Audit, error) {
	var item Audit
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),BIN_TO_UUID(protocol_id),status,planned_count,completed_count,eligible_count,budget_microunits,currency,created_at,started_at,finished_at
FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, auditID).Scan(&item.ID, &item.ProjectID, &item.ProtocolID, &item.Status, &item.PlannedCount, &item.CompletedCount, &item.EligibleCount, &item.BudgetMicrounits, &item.Currency, &item.CreatedAt, &item.StartedAt, &item.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Audit{}, ErrNotFound
	}
	if err != nil {
		return Audit{}, err
	}
	return item, nil
}

func (s *Store) CancelAudit(ctx context.Context, tenantID, auditID string) (Audit, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Audit{}, err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, auditID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return Audit{}, ErrNotFound
	}
	if err != nil {
		return Audit{}, err
	}
	switch status {
	case "cancelled":
		if err := tx.Commit(); err != nil {
			return Audit{}, err
		}
		return s.GetAudit(ctx, tenantID, auditID)
	case "succeeded", "failed", "partial_success":
		return Audit{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE audits SET status='cancelled',finished_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, auditID); err != nil {
		return Audit{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE audit_samples SET status='cancelled',lease_owner=NULL,lease_expires_at=NULL WHERE audit_id=UUID_TO_BIN(?) AND status IN ('queued','running')`, auditID); err != nil {
		return Audit{}, err
	}
	if err := releaseAuditReservation(ctx, tx, tenantID, auditID); err != nil {
		return Audit{}, err
	}
	if err := tx.Commit(); err != nil {
		return Audit{}, err
	}
	return s.GetAudit(ctx, tenantID, auditID)
}

func auditSampleKey(auditID, questionID, engineConfigID, modelSelector, channel, region, language, parametersHash string, sampleNo int) [sha256.Size]byte {
	return sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%d", auditID, questionID, engineConfigID, modelSelector, channel, region, language, parametersHash, sampleNo)))
}

func (s *Store) ListSamples(ctx context.Context, tenantID, auditID string) ([]Sample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(question_id),BIN_TO_UUID(engine_config_id),model_selector,channel,COALESCE(region,''),language,sample_no,status,created_at
FROM audit_samples WHERE tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?) ORDER BY created_at,id LIMIT 1000`, tenantID, auditID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Sample{}
	for rows.Next() {
		var item Sample
		if err := rows.Scan(&item.ID, &item.QuestionID, &item.EngineConfigID, &item.ModelSelector, &item.Channel, &item.Region, &item.Language, &item.SampleNo, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
