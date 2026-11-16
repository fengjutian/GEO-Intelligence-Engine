package evidencechain

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var ErrConflict = errors.New("evidence chain state conflicts with the requested operation")

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadEvidenceSnapshot(ctx context.Context, q queryer, tenantID, questionID, responseID, pageContentID, metricCode string) (EvidenceSnapshot, error) {
	var snapshot EvidenceSnapshot
	var protocolConfig []byte
	err := q.QueryRowContext(ctx, `SELECT q.text_value,BIN_TO_UUID(r.id),BIN_TO_UUID(s.audit_id),ec.provider,s.channel,s.model_selector,COALESCE(r.content_text,''),
BIN_TO_UUID(pc.id),COALESCE(pc.final_url,''),COALESCE(pc.title,''),LOWER(HEX(pc.content_hash)),
BIN_TO_UUID(a.question_version_id),mp.semantic_version,LOWER(HEX(mp.config_hash)),mp.config_json,
COALESCE((SELECT ms.metric_version FROM metric_snapshots ms WHERE ms.audit_id=a.id ORDER BY ms.generated_at DESC LIMIT 1),''),
COALESCE((SELECT er.judge_version FROM evaluation_runs er WHERE er.audit_id=a.id ORDER BY er.created_at DESC LIMIT 1),''),
COALESCE(s.region,''),s.language
FROM ai_responses r
JOIN audit_samples s ON s.id=r.sample_id
JOIN questions q ON q.id=s.question_id
JOIN audits a ON a.id=s.audit_id
JOIN measurement_protocols mp ON mp.id=a.protocol_id
JOIN engine_configs ec ON ec.id=s.engine_config_id
JOIN page_contents pc ON pc.id=UUID_TO_BIN(?)
WHERE r.tenant_id=UUID_TO_BIN(?) AND q.id=UUID_TO_BIN(?) AND r.id=UUID_TO_BIN(?)`,
		pageContentID, tenantID, questionID, responseID).Scan(
		&snapshot.Question.Text, &snapshot.Answer.ID, &snapshot.Answer.AuditID, &snapshot.Answer.Provider, &snapshot.Answer.Channel, &snapshot.Answer.Model, &snapshot.Answer.Content,
		&snapshot.Page.ContentID, &snapshot.Page.URL, &snapshot.Page.Title, &snapshot.Page.ContentHash,
		&snapshot.Protocol.QuestionVersionID, &snapshot.Protocol.ProtocolVersion, &snapshot.Protocol.ProtocolHash, &protocolConfig,
		&snapshot.Protocol.MetricVersion, &snapshot.Protocol.JudgeVersion, &snapshot.Protocol.Region, &snapshot.Protocol.Language)
	if err != nil {
		return EvidenceSnapshot{}, fmt.Errorf("load evidence snapshot: %w", err)
	}
	snapshot.Question.ID = questionID
	snapshot.Protocol.AuditID = snapshot.Answer.AuditID
	snapshot.Protocol.Channel = snapshot.Answer.Channel
	snapshot.Protocol.ModelSelector = snapshot.Answer.Model
	answerHash := sha256.Sum256([]byte(snapshot.Answer.Content))
	snapshot.Answer.ContentHash = hex.EncodeToString(answerHash[:])
	var config map[string]any
	_ = json.Unmarshal(protocolConfig, &config)
	if value, ok := config["promptTemplateVersion"].(string); ok {
		snapshot.Protocol.PromptTemplateVersion = value
	}
	snapshot.Baseline, err = loadMetricPoint(ctx, q, tenantID, snapshot.Answer.AuditID, metricCode, snapshot.Answer.Channel)
	if err != nil {
		return EvidenceSnapshot{}, err
	}
	snapshot.CapturedAt = time.Now().UTC()
	return snapshot, nil
}

func loadMetricPoint(ctx context.Context, q queryer, tenantID, auditID, metricCode, channel string) (MetricPoint, error) {
	point := MetricPoint{MetricCode: metricCode}
	err := q.QueryRowContext(ctx, `SELECT mv.numerator,mv.denominator,mv.value_decimal,mv.ci_low,mv.ci_high
FROM metric_values mv JOIN metric_snapshots ms ON ms.id=mv.snapshot_id
WHERE mv.tenant_id=UUID_TO_BIN(?) AND ms.audit_id=UUID_TO_BIN(?) AND mv.metric_code=? AND mv.aggregation_method='answer_equal'
  AND JSON_UNQUOTE(JSON_EXTRACT(mv.dimensions_json,'$.channel'))=?
  AND JSON_EXTRACT(mv.dimensions_json,'$.engineConfigId') IS NULL
ORDER BY ms.generated_at DESC LIMIT 1`, tenantID, auditID, metricCode, channel).Scan(&point.Numerator, &point.Denominator, &point.Value, &point.CILow, &point.CIHigh)
	if errors.Is(err, sql.ErrNoRows) {
		return point, nil
	}
	if err != nil {
		return MetricPoint{}, fmt.Errorf("load metric point: %w", err)
	}
	return point, nil
}

func (s *Store) Candidates(ctx context.Context, tenantID, findingID string, limit int) (Candidates, error) {
	if limit < 1 || limit > 50 {
		limit = 20
	}
	var result Candidates
	result.FindingID = findingID
	var projectID, ruleCode, evidenceText string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(f.project_id),f.rule_code,f.summary,CAST(f.evidence_json AS CHAR),COALESCE(BIN_TO_UUID(pc.id),''),COALESCE(pc.final_url,''),COALESCE(pc.title,''),COALESCE(LOWER(HEX(pc.content_hash)),'')
FROM findings f LEFT JOIN page_contents pc ON f.source_type='page_content' AND pc.id=f.source_id
WHERE f.tenant_id=UUID_TO_BIN(?) AND f.id=UUID_TO_BIN(?)`, tenantID, findingID).Scan(&projectID, &ruleCode, &result.Summary, &evidenceText, &result.Page.ContentID, &result.Page.URL, &result.Page.Title, &result.Page.ContentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Candidates{}, ErrNotFound
	}
	if err != nil {
		return Candidates{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(q.id),q.text_value,BIN_TO_UUID(r.id),BIN_TO_UUID(a.id),ec.provider,s.channel,s.model_selector,COALESCE(r.content_text,'')
FROM ai_responses r JOIN audit_samples s ON s.id=r.sample_id JOIN questions q ON q.id=s.question_id
JOIN audits a ON a.id=s.audit_id JOIN engine_configs ec ON ec.id=s.engine_config_id
WHERE r.tenant_id=UUID_TO_BIN(?) AND a.project_id=UUID_TO_BIN(?) AND a.status IN ('succeeded','partial_success')
ORDER BY a.finished_at DESC,r.created_at DESC LIMIT 200`, tenantID, projectID)
	if err != nil {
		return Candidates{}, err
	}
	defer rows.Close()
	needle := result.Summary + " " + ruleCode + " " + evidenceText + " " + result.Page.Title
	for rows.Next() {
		var item AnswerCandidate
		if err := rows.Scan(&item.QuestionID, &item.Question, &item.ResponseID, &item.AuditID, &item.Provider, &item.Channel, &item.Model, &item.Content); err != nil {
			return Candidates{}, err
		}
		item.Score = relevanceScore(needle, item.Question+" "+item.Content)
		result.Answers = append(result.Answers, item)
	}
	sort.SliceStable(result.Answers, func(i, j int) bool { return result.Answers[i].Score > result.Answers[j].Score })
	if len(result.Answers) > limit {
		result.Answers = result.Answers[:limit]
	}
	return result, rows.Err()
}

func relevanceScore(a, b string) float64 {
	left, right := shingles(a), shingles(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	matches := 0
	for token := range left {
		if right[token] {
			matches++
		}
	}
	return math.Round((float64(matches)/math.Sqrt(float64(len(left)*len(right))))*1000) / 1000
}

func shingles(value string) map[string]bool {
	runes := []rune(strings.ToLower(value))
	words := map[string]bool{}
	current := []rune{}
	flush := func() {
		if len(current) > 1 {
			words[string(current)] = true
		}
		current = current[:0]
	}
	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
		} else {
			flush()
		}
	}
	flush()
	for i := 0; i+1 < len(runes); i++ {
		if unicode.Is(unicode.Han, runes[i]) && unicode.Is(unicode.Han, runes[i+1]) {
			words[string(runes[i:i+2])] = true
		}
	}
	return words
}

var allowedTransitions = map[string]map[string]bool{
	StatusEvidenceReady:  {StatusPlanned: true},
	StatusPlanned:        {StatusImplementing: true},
	StatusImplementing:   {StatusAwaitingRetest: true},
	StatusAwaitingRetest: {StatusEvaluating: true},
	StatusEvaluating:     {StatusVerified: true, StatusIneffective: true, StatusInconclusive: true},
}

func (s *Store) Transition(ctx context.Context, tenantID, actorID, findingID string, input TransitionInput) (Chain, error) {
	input.TargetStatus, input.Reason = strings.TrimSpace(input.TargetStatus), strings.TrimSpace(input.Reason)
	input.RetestAuditID = strings.TrimSpace(input.RetestAuditID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Chain{}, err
	}
	defer tx.Rollback()
	var current, projectID string
	if err := tx.QueryRowContext(ctx, `SELECT status,BIN_TO_UUID(project_id) FROM finding_evidence_chains WHERE tenant_id=UUID_TO_BIN(?) AND finding_id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, findingID).Scan(&current, &projectID); errors.Is(err, sql.ErrNoRows) {
		return Chain{}, ErrNotFound
	} else if err != nil {
		return Chain{}, err
	}
	if !allowedTransitions[current][input.TargetStatus] || input.Reason == "" {
		return Chain{}, ErrConflict
	}
	if input.TargetStatus == StatusEvaluating {
		if input.RetestAuditID == "" {
			return Chain{}, ErrConflict
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`, tenantID, input.RetestAuditID, projectID).Scan(&count); err != nil || count != 1 {
			return Chain{}, ErrInvalidReference
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE finding_evidence_chains SET status=?,retest_audit_id=CASE WHEN ?='' THEN retest_audit_id ELSE UUID_TO_BIN(?) END,version=version+1,updated_by=UUID_TO_BIN(?),updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=UUID_TO_BIN(?) AND finding_id=UUID_TO_BIN(?)`, input.TargetStatus, input.RetestAuditID, input.RetestAuditID, actorID, tenantID, findingID); err != nil {
		return Chain{}, err
	}
	if err := appendChainEvent(ctx, tx, tenantID, findingID, actorID, "transitioned", current, input.TargetStatus, input.Reason, nil); err != nil {
		return Chain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Chain{}, err
	}
	return s.Get(ctx, tenantID, findingID)
}

func (s *Store) Evaluate(ctx context.Context, tenantID, actorID, findingID string) (Chain, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Chain{}, err
	}
	defer tx.Rollback()
	var status, retestAuditID, metricCode string
	var targetDelta float64
	var snapshotJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT status,BIN_TO_UUID(retest_audit_id),metric_code,target_delta,evidence_snapshot_json FROM finding_evidence_chains WHERE tenant_id=UUID_TO_BIN(?) AND finding_id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, findingID).Scan(&status, &retestAuditID, &metricCode, &targetDelta, &snapshotJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Chain{}, ErrNotFound
	}
	if err != nil {
		return Chain{}, err
	}
	if status != StatusEvaluating {
		return Chain{}, ErrConflict
	}
	var snapshot EvidenceSnapshot
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		return Chain{}, err
	}
	retestProtocol, retestMetric, err := loadRetestSnapshot(ctx, tx, tenantID, retestAuditID, metricCode, snapshot.Protocol.Channel)
	if err != nil {
		return Chain{}, err
	}
	comparison := compareSnapshots(snapshot.Protocol, retestProtocol, snapshot.Baseline, retestMetric, targetDelta)
	comparisonJSON, _ := json.Marshal(comparison)
	finalStatus := StatusInconclusive
	switch comparison.Decision {
	case OutcomeImproved:
		finalStatus = StatusVerified
	case OutcomeUnchanged, OutcomeRegressed:
		finalStatus = StatusIneffective
	}
	_, err = tx.ExecContext(ctx, `UPDATE finding_evidence_chains SET status=?,comparison_json=CAST(? AS JSON),retest_outcome=?,decision_source='automatic',decision_reason=?,version=version+1,updated_by=UUID_TO_BIN(?),updated_at=UTC_TIMESTAMP(6) WHERE finding_id=UUID_TO_BIN(?) AND tenant_id=UUID_TO_BIN(?)`, finalStatus, comparisonJSON, comparison.Decision, comparison.Reason, actorID, findingID, tenantID)
	if err != nil {
		return Chain{}, err
	}
	if err := appendChainEvent(ctx, tx, tenantID, findingID, actorID, "evaluated", status, finalStatus, comparison.Reason, comparison); err != nil {
		return Chain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Chain{}, err
	}
	return s.Get(ctx, tenantID, findingID)
}

func loadRetestSnapshot(ctx context.Context, q queryer, tenantID, auditID, metricCode, channel string) (ProtocolSnapshot, MetricPoint, error) {
	var p ProtocolSnapshot
	var config []byte
	err := q.QueryRowContext(ctx, `SELECT BIN_TO_UUID(a.id),BIN_TO_UUID(a.question_version_id),mp.semantic_version,LOWER(HEX(mp.config_hash)),mp.config_json,
COALESCE((SELECT ms.metric_version FROM metric_snapshots ms WHERE ms.audit_id=a.id ORDER BY ms.generated_at DESC LIMIT 1),''),
COALESCE((SELECT er.judge_version FROM evaluation_runs er WHERE er.audit_id=a.id ORDER BY er.created_at DESC LIMIT 1),''),
COALESCE(s.channel,''),COALESCE(s.model_selector,''),COALESCE(s.region,''),COALESCE(s.language,'')
FROM audits a JOIN measurement_protocols mp ON mp.id=a.protocol_id
LEFT JOIN audit_samples s ON s.audit_id=a.id AND s.channel=?
WHERE a.tenant_id=UUID_TO_BIN(?) AND a.id=UUID_TO_BIN(?) AND a.status IN ('succeeded','partial_success') LIMIT 1`, channel, tenantID, auditID).Scan(&p.AuditID, &p.QuestionVersionID, &p.ProtocolVersion, &p.ProtocolHash, &config, &p.MetricVersion, &p.JudgeVersion, &p.Channel, &p.ModelSelector, &p.Region, &p.Language)
	if errors.Is(err, sql.ErrNoRows) {
		return p, MetricPoint{}, ErrConflict
	}
	if err != nil {
		return p, MetricPoint{}, err
	}
	var decoded map[string]any
	_ = json.Unmarshal(config, &decoded)
	p.PromptTemplateVersion, _ = decoded["promptTemplateVersion"].(string)
	metric, err := loadMetricPoint(ctx, q, tenantID, auditID, metricCode, channel)
	return p, metric, err
}

func compareSnapshots(base, retest ProtocolSnapshot, baseline, retestMetric MetricPoint, target float64) Comparison {
	differences := []string{}
	checks := []struct{ name, a, b string }{{"questionVersion", base.QuestionVersionID, retest.QuestionVersionID}, {"metricVersion", base.MetricVersion, retest.MetricVersion}, {"judgeVersion", base.JudgeVersion, retest.JudgeVersion}, {"promptTemplateVersion", base.PromptTemplateVersion, retest.PromptTemplateVersion}, {"channel", base.Channel, retest.Channel}, {"modelSelector", base.ModelSelector, retest.ModelSelector}, {"region", base.Region, retest.Region}, {"language", base.Language, retest.Language}}
	for _, check := range checks {
		if check.a != check.b {
			differences = append(differences, check.name)
		}
	}
	result := Comparison{Comparable: len(differences) == 0, Differences: differences, Baseline: baseline, Retest: retestMetric, Decision: OutcomeInconclusive}
	if len(differences) > 0 {
		result.Reason = "Measurement protocols are not comparable"
		return result
	}
	if baseline.Value == nil || retestMetric.Value == nil || baseline.Denominator == nil || retestMetric.Denominator == nil || *baseline.Denominator < 10 || *retestMetric.Denominator < 10 {
		result.Reason = "Metric is missing or sample size is below 10"
		return result
	}
	delta := *retestMetric.Value - *baseline.Value
	result.Delta = &delta
	if baseline.CILow == nil || baseline.CIHigh == nil || retestMetric.CILow == nil || retestMetric.CIHigh == nil {
		result.Reason = "Confidence interval is unavailable"
		return result
	}
	if delta >= target && *retestMetric.CILow > *baseline.CIHigh {
		result.Decision = OutcomeImproved
		result.Reason = "Retest confidence interval is above baseline"
		return result
	}
	if -delta >= target && *retestMetric.CIHigh < *baseline.CILow {
		result.Decision = OutcomeRegressed
		result.Reason = "Retest confidence interval is below baseline"
		return result
	}
	if math.Abs(delta) < target {
		result.Decision = OutcomeUnchanged
		result.Reason = "Change is below the minimum target delta"
		return result
	}
	result.Reason = "Observed change is not statistically conclusive"
	return result
}

func appendChainEvent(ctx context.Context, tx *sql.Tx, tenantID, findingID, actorID, eventType, fromStatus, toStatus, reason string, payload any) error {
	id, _ := uuid.NewV7()
	encoded, _ := json.Marshal(payload)
	_, err := tx.ExecContext(ctx, `INSERT INTO finding_evidence_chain_events (id,tenant_id,finding_id,actor_id,event_type,from_status,to_status,reason,payload_json,occurred_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,CAST(? AS JSON),UTC_TIMESTAMP(6))`, id.String(), tenantID, findingID, actorID, eventType, fromStatus, toStatus, reason, encoded)
	return err
}

func (s *Store) listEvents(ctx context.Context, tenantID, findingID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),event_type,COALESCE(from_status,''),COALESCE(to_status,''),COALESCE(reason,''),occurred_at FROM finding_evidence_chain_events WHERE tenant_id=UUID_TO_BIN(?) AND finding_id=UUID_TO_BIN(?) ORDER BY occurred_at,id`, tenantID, findingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.EventType, &event.FromStatus, &event.ToStatus, &event.Reason, &event.OccurredAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
