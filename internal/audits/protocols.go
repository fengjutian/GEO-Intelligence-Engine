package audits

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

func (s *Store) CreateProtocol(ctx context.Context, tenantID, projectID string, input ProtocolInput) (Protocol, error) {
	input.QuestionVersionID = strings.TrimSpace(input.QuestionVersionID)
	if input.QuestionVersionID == "" || len(input.Engines) == 0 {
		return Protocol{}, ErrInvalidInput
	}
	if input.SamplesPerQuestion == 0 {
		input.SamplesPerQuestion = 1
	}
	if input.SamplesPerQuestion < 1 || input.SamplesPerQuestion > 10 {
		return Protocol{}, ErrInvalidInput
	}
	if input.Currency == "" {
		input.Currency = "USD"
	}
	input.Currency = strings.ToUpper(input.Currency)
	if len(input.Currency) != 3 {
		return Protocol{}, ErrInvalidInput
	}
	if input.BudgetMicrounits < 0 || input.EstimatedCostPerSample < 0 {
		return Protocol{}, ErrInvalidInput
	}
	if input.JudgeVersion == "" {
		input.JudgeVersion = "judge-rules-v1.1"
	}
	if input.MetricVersion == "" {
		input.MetricVersion = MetricVersion
	}
	if input.PromptTemplateVersion == "" {
		input.PromptTemplateVersion = "prompt-v1"
	}

	var projectStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM projects WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID).Scan(&projectStatus); errors.Is(err, sql.ErrNoRows) {
		return Protocol{}, ErrNotFound
	} else if err != nil {
		return Protocol{}, err
	}
	if projectStatus == "archived" {
		return Protocol{}, ErrArchived
	}
	var published int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM question_set_versions qsv JOIN question_sets qs ON qs.id=qsv.question_set_id
WHERE qsv.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.id=UUID_TO_BIN(?) AND qsv.status='published'`, tenantID, projectID, input.QuestionVersionID).Scan(&published); err != nil {
		return Protocol{}, err
	}
	if published == 0 {
		return Protocol{}, ErrInvalidInput
	}

	engines := make([]map[string]any, 0, len(input.Engines))
	for _, engine := range input.Engines {
		engine.EngineConfigID = strings.TrimSpace(engine.EngineConfigID)
		engine.ModelSelector = strings.TrimSpace(engine.ModelSelector)
		if engine.EngineConfigID == "" || engine.ModelSelector == "" {
			return Protocol{}, ErrInvalidInput
		}
		var provider, channel, status string
		err := s.db.QueryRowContext(ctx, `SELECT provider,channel,status FROM engine_configs WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, engine.EngineConfigID).Scan(&provider, &channel, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return Protocol{}, ErrNotFound
		}
		if err != nil {
			return Protocol{}, err
		}
		if status != "active" {
			return Protocol{}, ErrInvalidInput
		}
		if engine.Channel == "" {
			engine.Channel = channel
		}
		if !isChannel(engine.Channel) {
			return Protocol{}, ErrInvalidInput
		}
		if engine.Parameters == nil {
			engine.Parameters = map[string]any{}
		}
		engines = append(engines, map[string]any{
			"engineConfigId": engine.EngineConfigID,
			"provider":       provider,
			"modelSelector":  engine.ModelSelector,
			"channel":        engine.Channel,
			"parameters":     engine.Parameters,
		})
	}

	config := map[string]any{
		"questionVersionId":      input.QuestionVersionID,
		"engines":                engines,
		"region":                 defaultString(input.Region, "global"),
		"language":               defaultString(input.Language, "zh-CN"),
		"samplesPerQuestion":     input.SamplesPerQuestion,
		"budgetMicrounits":       input.BudgetMicrounits,
		"currency":               input.Currency,
		"judgeVersion":           input.JudgeVersion,
		"metricVersion":          input.MetricVersion,
		"promptTemplateVersion":  input.PromptTemplateVersion,
		"estimatedCostPerSample": input.EstimatedCostPerSample,
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return Protocol{}, err
	}
	hash := sha256.Sum256(encoded)
	var existing int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM measurement_protocols WHERE project_id=UUID_TO_BIN(?)`, projectID).Scan(&existing); err != nil {
		return Protocol{}, err
	}
	semanticVersion := fmt.Sprintf("1.0.%d", existing+1)
	id, _ := uuid.NewV7()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Protocol{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO measurement_protocols (id,tenant_id,project_id,semantic_version,config_json,config_hash,status,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,CAST(? AS JSON),?,'draft',UTC_TIMESTAMP(6))`,
		id.String(), tenantID, projectID, semanticVersion, encoded, hash[:])
	if err != nil {
		return Protocol{}, fmt.Errorf("insert protocol: %w", err)
	}
	for index, engine := range input.Engines {
		parameters, _ := json.Marshal(engines[index]["parameters"])
		channel, _ := engines[index]["channel"].(string)
		engineID, _ := uuid.NewV7()
		_, err = tx.ExecContext(ctx, `INSERT INTO protocol_engines (id,tenant_id,protocol_id,engine_config_id,model_selector,channel,parameters_json)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,CAST(? AS JSON))`,
			engineID.String(), tenantID, id.String(), engine.EngineConfigID, engine.ModelSelector, channel, parameters)
		if err != nil {
			return Protocol{}, fmt.Errorf("insert protocol engine: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Protocol{}, err
	}
	return s.GetProtocol(ctx, tenantID, id.String())
}

func (s *Store) PublishProtocol(ctx context.Context, tenantID, protocolID string) (Protocol, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Protocol{}, err
	}
	defer tx.Rollback()
	var projectID, status string
	var configJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),status,config_json FROM measurement_protocols WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, protocolID).Scan(&projectID, &status, &configJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Protocol{}, ErrNotFound
	}
	if err != nil {
		return Protocol{}, err
	}
	if status != "draft" {
		return Protocol{}, ErrConflict
	}
	var config map[string]any
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return Protocol{}, err
	}
	questionVersionID := configString(config, "questionVersionId")
	var published int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM question_set_versions WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND status='published'`, tenantID, questionVersionID).Scan(&published); err != nil {
		return Protocol{}, err
	}
	if published == 0 {
		return Protocol{}, ErrInvalidInput
	}
	snapshotID, snapshotVersion, err := createKnowledgeSnapshot(ctx, tx, tenantID, projectID)
	if err != nil {
		return Protocol{}, err
	}
	config["knowledgeSnapshotId"] = snapshotID
	config["knowledgeVersion"] = snapshotVersion
	encoded, err := json.Marshal(config)
	if err != nil {
		return Protocol{}, err
	}
	hash := sha256.Sum256(encoded)
	_, err = tx.ExecContext(ctx, `UPDATE measurement_protocols SET config_json=CAST(? AS JSON),config_hash=?,status='published',published_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, encoded, hash[:], protocolID)
	if err != nil {
		return Protocol{}, err
	}
	if err := tx.Commit(); err != nil {
		return Protocol{}, err
	}
	return s.GetProtocol(ctx, tenantID, protocolID)
}

func (s *Store) GetProtocol(ctx context.Context, tenantID, protocolID string) (Protocol, error) {
	var item Protocol
	var configJSON []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),semantic_version,status,LOWER(HEX(config_hash)),config_json,created_at,published_at
FROM measurement_protocols WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, protocolID).Scan(&item.ID, &item.ProjectID, &item.SemanticVersion, &item.Status, &item.ConfigHash, &configJSON, &item.CreatedAt, &item.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Protocol{}, ErrNotFound
	}
	if err != nil {
		return Protocol{}, err
	}
	_ = json.Unmarshal(configJSON, &item.Config)
	return item, nil
}

func (s *Store) ListProtocols(ctx context.Context, tenantID, projectID string) ([]Protocol, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),semantic_version,status,LOWER(HEX(config_hash)),config_json,created_at,published_at
FROM measurement_protocols WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at DESC,id DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Protocol{}
	for rows.Next() {
		var item Protocol
		var configJSON []byte
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.SemanticVersion, &item.Status, &item.ConfigHash, &configJSON, &item.CreatedAt, &item.PublishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(configJSON, &item.Config)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) EstimateAudit(ctx context.Context, tenantID, protocolID string) (Estimate, error) {
	protocol, err := s.GetProtocol(ctx, tenantID, protocolID)
	if err != nil {
		return Estimate{}, err
	}
	if protocol.Status != "published" {
		return Estimate{}, ErrInvalidInput
	}
	return s.estimateProtocol(ctx, protocol)
}

func (s *Store) estimateProtocol(ctx context.Context, protocol Protocol) (Estimate, error) {
	questionVersionID := configString(protocol.Config, "questionVersionId")
	engines := configEngines(protocol.Config)
	samples := configInt(protocol.Config, "samplesPerQuestion")
	if samples < 1 {
		samples = 1
	}
	var questions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM questions WHERE version_id=UUID_TO_BIN(?)`, questionVersionID).Scan(&questions); err != nil {
		return Estimate{}, err
	}
	planned := questions * len(engines) * samples
	if planned == 0 {
		return Estimate{}, ErrInvalidInput
	}
	costPerSample := int64(configInt(protocol.Config, "estimatedCostPerSample"))
	cost := int64(planned) * costPerSample
	currency := configString(protocol.Config, "currency")
	parallelism := len(engines)
	if parallelism < 1 {
		parallelism = 1
	}
	seconds := int(math.Ceil(float64(planned)/float64(parallelism))) * 5
	return Estimate{PlannedSamples: planned, EstimatedDurationSeconds: seconds, EstimatedCost: Money{AmountMicrounits: cost, Currency: currency}}, nil
}

func configString(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}

func configInt(config map[string]any, key string) int {
	switch value := config[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		result, _ := value.Int64()
		return int(result)
	default:
		return 0
	}
}

func configEngines(config map[string]any) []map[string]any {
	raw, _ := config["engines"].([]any)
	engines := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if engine, ok := entry.(map[string]any); ok {
			engines = append(engines, engine)
		}
	}
	return engines
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
