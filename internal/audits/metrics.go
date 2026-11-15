package audits

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
)

const (
	// MetricVersion is the immutable snapshot computed from Judge output.
	MetricVersion = "metrics-v1"
	// HumanMetricVersion is the snapshot recomputed after adjudications rewrite
	// the effective Judge decision for some answers.
	HumanMetricVersion = "metrics-v1h"
)

// MetricValue is one aggregated rate with its numerator, denominator and interval.
type MetricValue struct {
	ID                string         `json:"id"`
	MetricCode        string         `json:"metricCode"`
	AggregationMethod string         `json:"aggregationMethod"`
	Dimensions        map[string]any `json:"dimensions"`
	Numerator         *float64       `json:"numerator"`
	Denominator       *float64       `json:"denominator"`
	Value             *float64       `json:"value"`
	CILow             *float64       `json:"ciLow"`
	CIHigh            *float64       `json:"ciHigh"`
	IntervalMethod    string         `json:"intervalMethod"`
}

type metricGroup struct {
	dimensions          map[string]any
	eligible            int
	mentions            int
	recommendations     int
	refusals            int
	positiveMentions    int
	mentionFrequency    int
	officialCitations   int
	thirdPartyCitations int
	members             []map[string]any
}

func (s *Store) GetMetrics(ctx context.Context, tenantID, auditID, aggregationMethod, channel string) ([]MetricValue, error) {
	if aggregationMethod == "" {
		aggregationMethod = "answer_equal"
	}
	var snapshotID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM metric_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?) ORDER BY generated_at DESC,id DESC LIMIT 1`, tenantID, auditID).Scan(&snapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),metric_code,aggregation_method,dimensions_json,numerator,denominator,value_decimal,ci_low,ci_high,interval_method
FROM metric_values WHERE tenant_id=UUID_TO_BIN(?) AND snapshot_id=UUID_TO_BIN(?) AND aggregation_method=? ORDER BY metric_code,id`, tenantID, snapshotID, aggregationMethod)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MetricValue{}
	for rows.Next() {
		var item MetricValue
		var dimensions []byte
		var numerator, denominator, value, ciLow, ciHigh sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.MetricCode, &item.AggregationMethod, &dimensions, &numerator, &denominator, &value, &ciLow, &ciHigh, &item.IntervalMethod); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(dimensions, &item.Dimensions)
		if channel != "" {
			if value, ok := item.Dimensions["channel"].(string); !ok || value != channel {
				continue
			}
		}
		item.Numerator = nullFloat(numerator)
		item.Denominator = nullFloat(denominator)
		item.Value = nullFloat(value)
		item.CILow = nullFloat(ciLow)
		item.CIHigh = nullFloat(ciHigh)
		items = append(items, item)
	}
	return items, rows.Err()
}

func generateMetrics(ctx context.Context, tx *sql.Tx, tenantID, auditID string) error {
	return generateMetricsVersion(ctx, tx, tenantID, auditID, MetricVersion, false)
}

// generateMetricsVersion writes one snapshot per (audit, evaluation run,
// version). The Judge snapshot is immutable and never rewritten; the human
// snapshot is rebuilt in place so repeated adjudications stay idempotent.
func generateMetricsVersion(ctx context.Context, tx *sql.Tx, tenantID, auditID, version string, human bool) error {
	var evaluationRunID string
	err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM evaluation_runs WHERE tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?) ORDER BY created_at DESC,id DESC LIMIT 1`, tenantID, auditID).Scan(&evaluationRunID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var planned int
	if err := tx.QueryRowContext(ctx, `SELECT planned_count FROM audits WHERE id=UUID_TO_BIN(?)`, auditID).Scan(&planned); err != nil {
		return err
	}
	snapshotUUID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO metric_snapshots (id,tenant_id,audit_id,evaluation_run_id,metric_version,status,generated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,'ready',UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE id=id`, snapshotUUID.String(), tenantID, auditID, evaluationRunID, version)
	if err != nil {
		return fmt.Errorf("create metric snapshot: %w", err)
	}
	var snapshotID string
	if err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM metric_snapshots WHERE audit_id=UUID_TO_BIN(?) AND evaluation_run_id=UUID_TO_BIN(?) AND metric_version=?`, auditID, evaluationRunID, version).Scan(&snapshotID); err != nil {
		return err
	}
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM metric_values WHERE snapshot_id=UUID_TO_BIN(?)`, snapshotID).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		if !human {
			return nil
		}
		if err := clearMetricSnapshot(ctx, tx, snapshotID); err != nil {
			return err
		}
	}

	factsQuery := `
SELECT BIN_TO_UUID(s.engine_config_id),ec.provider,s.channel,s.model_selector,BIN_TO_UUID(ae.id),BIN_TO_UUID(ae.response_id),BIN_TO_UUID(s.question_id),BIN_TO_UUID(s.id),
       COALESCE(JSON_UNQUOTE(ae.human_decision_json->'$.answerOutcome'), ae.answer_outcome),
       COALESCE(JSON_UNQUOTE(ae.human_decision_json->'$.recommendationLabel'), COALESCE(ae.recommendation_label,'')),
       COALESCE(JSON_UNQUOTE(ae.human_decision_json->'$.sentiment'), COALESCE(ae.sentiment,'')),
       COALESCE(JSON_LENGTH(ae.evidence_json->'$.mentions'),0),
       (SELECT COUNT(*) FROM citations c WHERE c.evaluation_id=ae.id AND c.source_type='official'),
       (SELECT COUNT(*) FROM citations c WHERE c.evaluation_id=ae.id AND c.source_type='third_party')
FROM answer_evaluations ae
JOIN ai_responses resp ON resp.id=ae.response_id
JOIN audit_samples s ON s.id=resp.sample_id
JOIN engine_configs ec ON ec.id=s.engine_config_id
WHERE s.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?) AND ae.evaluation_run_id=UUID_TO_BIN(?)
  AND (ae.eligible_for_core_metrics=TRUE OR %s)`
	eligibility := "FALSE"
	if human {
		eligibility = "ae.human_decision_json->'$.eligible' = TRUE"
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(factsQuery, eligibility), tenantID, auditID, evaluationRunID)
	if err != nil {
		return fmt.Errorf("load metric facts: %w", err)
	}
	defer rows.Close()
	groups := map[string]*metricGroup{}
	group := func(dimensions map[string]any) *metricGroup {
		keyBytes, _ := json.Marshal(dimensions)
		key := string(keyBytes)
		if existing, ok := groups[key]; ok {
			return existing
		}
		created := &metricGroup{dimensions: dimensions}
		groups[key] = created
		return created
	}
	for rows.Next() {
		var engineID, provider, channel, modelSelector, evaluationID, responseID, questionID, sampleID, outcome, recommendation, sentiment string
		var mentions, officialCitations, thirdPartyCitations int
		if err := rows.Scan(&engineID, &provider, &channel, &modelSelector, &evaluationID, &responseID, &questionID, &sampleID, &outcome, &recommendation, &sentiment, &mentions, &officialCitations, &thirdPartyCitations); err != nil {
			return err
		}
		member := map[string]any{
			"evaluationId": evaluationID, "responseId": responseID, "questionId": questionID, "sampleId": sampleID,
			"answerOutcome": outcome, "recommendationLabel": recommendation, "sentiment": sentiment, "mentionCount": mentions,
			"officialCitationCount": officialCitations, "thirdPartyCitationCount": thirdPartyCitations,
		}
		targets := []*metricGroup{
			group(map[string]any{"channel": channel}),
			group(map[string]any{"channel": channel, "engineConfigId": engineID, "provider": provider, "modelSelector": modelSelector}),
		}
		for _, target := range targets {
			target.eligible++
			target.mentionFrequency += mentions
			if officialCitations > 0 {
				target.officialCitations++
			}
			if thirdPartyCitations > 0 {
				target.thirdPartyCitations++
			}
			target.members = append(target.members, member)
			if recommendation == "mentioned" {
				target.mentions++
				if sentiment == "positive" {
					target.positiveMentions++
				}
			}
			if outcome == "recommendation" {
				target.recommendations++
			}
			if outcome == "refusal" {
				target.refusals++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, group := range groups {
		for _, metric := range []struct {
			code        string
			numerator   int
			denominator int
		}{
			{"mention_rate", group.mentions, group.eligible},
			{"recommendation_rate", group.recommendations, group.eligible},
			{"refusal_rate", group.refusals, group.eligible},
			{"positive_mention_rate", group.positiveMentions, group.mentions},
			{"official_citation_rate", group.officialCitations, group.eligible},
			{"third_party_citation_rate", group.thirdPartyCitations, group.eligible},
			{"eligible_response_rate", group.eligible, planned},
		} {
			metricValueID, err := insertMetricValue(ctx, tx, tenantID, snapshotID, metric.code, group.dimensions, metric.numerator, metric.denominator)
			if err != nil {
				return err
			}
			if err := insertMetricEvidence(ctx, tx, tenantID, metricValueID, group.members); err != nil {
				return err
			}
		}
		if group.eligible > 0 {
			value := float64(group.mentionFrequency) / float64(group.eligible)
			metricValueID, err := insertMetricFrequency(ctx, tx, tenantID, snapshotID, group.dimensions, group.mentionFrequency, group.eligible, value)
			if err != nil {
				return err
			}
			if err := insertMetricEvidence(ctx, tx, tenantID, metricValueID, group.members); err != nil {
				return err
			}
		}
	}
	if err := generateShareMetrics(ctx, tx, tenantID, auditID, snapshotID); err != nil {
		return err
	}
	return nil
}

// clearMetricSnapshot removes the derived rows of one human snapshot so it can
// be rebuilt without violating the uniqueness of its values.
func clearMetricSnapshot(ctx context.Context, tx *sql.Tx, snapshotID string) error {
	for _, statement := range []string{
		`DELETE me FROM metric_evidence_sets me JOIN metric_values mv ON mv.id=me.metric_value_id WHERE mv.snapshot_id=UUID_TO_BIN(?)`,
		`DELETE mq FROM metric_quality_flags mq JOIN metric_values mv ON mv.id=mq.metric_value_id WHERE mv.snapshot_id=UUID_TO_BIN(?)`,
		`DELETE FROM metric_values WHERE snapshot_id=UUID_TO_BIN(?)`,
	} {
		if _, err := tx.ExecContext(ctx, statement, snapshotID); err != nil {
			return fmt.Errorf("clear metric snapshot: %w", err)
		}
	}
	return nil
}

// RecomputeMetrics rebuilds the human-adjusted snapshot from the effective
// Judge decisions, including adjudication write-backs. The immutable Judge
// snapshot (metrics-v1) is left untouched.
func (s *Store) RecomputeMetrics(ctx context.Context, tenantID, auditID string) ([]MetricValue, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, auditID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "succeeded" && status != "partial_success" {
		return nil, ErrConflict
	}
	var evaluationRuns int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM evaluation_runs WHERE tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?)`, tenantID, auditID).Scan(&evaluationRuns); err != nil {
		return nil, err
	}
	if evaluationRuns == 0 {
		return nil, ErrConflict
	}
	if err := generateMetricsVersion(ctx, tx, tenantID, auditID, HumanMetricVersion, true); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMetrics(ctx, tenantID, auditID, "answer_equal", "")
}

func insertMetricValue(ctx context.Context, tx *sql.Tx, tenantID, snapshotID, code string, dimensions map[string]any, numerator, denominator int) (string, error) {
	id, _ := uuid.NewV7()
	dimensionsJSON, _ := json.Marshal(dimensions)
	hash := sha256.Sum256(dimensionsJSON)
	var value, ciLow, ciHigh, numeratorValue, denominatorValue any
	if denominator > 0 {
		proportion := float64(numerator) / float64(denominator)
		low, high := wilson(numerator, denominator)
		value, ciLow, ciHigh = proportion, low, high
		numeratorValue, denominatorValue = numerator, denominator
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO metric_values (id,tenant_id,snapshot_id,metric_code,aggregation_method,dimension_hash,dimensions_json,numerator,denominator,value_decimal,ci_low,ci_high,interval_method,interval_metadata,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,'answer_equal',?,CAST(? AS JSON),?,?,?,?,?,'wilson',JSON_OBJECT('confidence',0.95),UTC_TIMESTAMP(6))`,
		id.String(), tenantID, snapshotID, code, hash[:], dimensionsJSON, numeratorValue, denominatorValue, value, ciLow, ciHigh)
	if err != nil {
		return "", fmt.Errorf("insert metric value: %w", err)
	}
	if denominator > 0 && denominator < 30 {
		flagID, _ := uuid.NewV7()
		if _, err := tx.ExecContext(ctx, `INSERT INTO metric_quality_flags (id,tenant_id,metric_value_id,flag_code,details_json) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'small_sample',JSON_OBJECT('denominator',?))
ON DUPLICATE KEY UPDATE flag_code=flag_code`, flagID.String(), tenantID, id.String(), denominator); err != nil {
			return "", err
		}
	}
	return id.String(), nil
}

func insertMetricFrequency(ctx context.Context, tx *sql.Tx, tenantID, snapshotID string, dimensions map[string]any, numerator, denominator int, value float64) (string, error) {
	id, _ := uuid.NewV7()
	dimensionsJSON, _ := json.Marshal(dimensions)
	hash := sha256.Sum256(dimensionsJSON)
	_, err := tx.ExecContext(ctx, `INSERT INTO metric_values (id,tenant_id,snapshot_id,metric_code,aggregation_method,dimension_hash,dimensions_json,numerator,denominator,value_decimal,interval_method,interval_metadata,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'mention_frequency','answer_equal',?,CAST(? AS JSON),?,?,?,'none',JSON_OBJECT(),UTC_TIMESTAMP(6))`,
		id.String(), tenantID, snapshotID, hash[:], dimensionsJSON, numerator, denominator, value)
	if err != nil {
		return "", fmt.Errorf("insert mention frequency: %w", err)
	}
	return id.String(), nil
}

// insertMetricEvidence stores the eligible answers behind one metric value.
func insertMetricEvidence(ctx context.Context, tx *sql.Tx, tenantID, metricValueID string, members []map[string]any) error {
	stored := members
	truncated := false
	if len(stored) > 1000 {
		stored = stored[:1000]
		truncated = true
	}
	content, err := json.Marshal(map[string]any{"members": stored, "truncated": truncated, "memberCount": len(members)})
	if err != nil {
		return err
	}
	queryHash := sha256.Sum256([]byte(metricValueID))
	evidenceID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO metric_evidence_sets (id,tenant_id,metric_value_id,query_hash,member_count,members_object_key,content_json)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,CAST(? AS JSON))`,
		evidenceID.String(), tenantID, metricValueID, queryHash[:], len(members), fmt.Sprintf("db://metric-evidence/%s", metricValueID), content)
	if err != nil {
		return fmt.Errorf("insert metric evidence: %w", err)
	}
	return nil
}

// GetMetricEvidence returns the answers and judge labels behind one metric value.
func (s *Store) GetMetricEvidence(ctx context.Context, tenantID, metricValueID string) (map[string]any, error) {
	var content []byte
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(me.content_json,JSON_OBJECT())
FROM metric_evidence_sets me JOIN metric_values mv ON mv.id=me.metric_value_id
WHERE me.tenant_id=UUID_TO_BIN(?) AND mv.tenant_id=UUID_TO_BIN(?) AND me.metric_value_id=UUID_TO_BIN(?)`, tenantID, tenantID, metricValueID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func wilson(numerator, denominator int) (float64, float64) {
	if denominator == 0 {
		return 0, 0
	}
	const z = 1.959963984540054
	n := float64(denominator)
	p := float64(numerator) / n
	denominatorTerm := 1 + z*z/n
	center := (p + z*z/(2*n)) / denominatorTerm
	margin := z * math.Sqrt((p*(1-p)+z*z/(4*n))/n) / denominatorTerm
	low := math.Max(0, center-margin)
	high := math.Min(1, center+margin)
	return low, high
}

func nullFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}
