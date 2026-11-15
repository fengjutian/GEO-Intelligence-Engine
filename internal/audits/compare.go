package audits

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MetricDelta compares one metric between a base and target audit.
type MetricDelta struct {
	MetricCode        string   `json:"metricCode"`
	BaseValue         *float64 `json:"baseValue"`
	TargetValue       *float64 `json:"targetValue"`
	Delta             *float64 `json:"delta"`
	BaseNumerator     *float64 `json:"baseNumerator"`
	BaseDenominator   *float64 `json:"baseDenominator"`
	TargetNumerator   *float64 `json:"targetNumerator"`
	TargetDenominator *float64 `json:"targetDenominator"`
}

// AuditComparison is the delta between two finished audits.
type AuditComparison struct {
	BaseAuditID      string        `json:"baseAuditId"`
	TargetAuditID    string        `json:"targetAuditId"`
	BaseFinishedAt   *time.Time    `json:"baseFinishedAt"`
	TargetFinishedAt *time.Time    `json:"targetFinishedAt"`
	BaseCounts       AuditCounts   `json:"baseCounts"`
	TargetCounts     AuditCounts   `json:"targetCounts"`
	Metrics          []MetricDelta `json:"metrics"`
}

// AuditCounts is the sample accounting for one audit.
type AuditCounts struct {
	Planned   int `json:"plannedCount"`
	Completed int `json:"completedCount"`
	Eligible  int `json:"eligibleCount"`
}

var ErrAuditNotFound = errors.New("audit was not found")
var ErrAuditComparisonInvalid = errors.New("audits cannot be compared")

// CompareAudits compares the latest metric snapshots of two audits in one project.
func (s *Store) CompareAudits(ctx context.Context, tenantID, baseAuditID, targetAuditID string) (AuditComparison, error) {
	base, err := loadAuditSummary(ctx, s.db, tenantID, baseAuditID)
	if err != nil {
		return AuditComparison{}, err
	}
	target, err := loadAuditSummary(ctx, s.db, tenantID, targetAuditID)
	if err != nil {
		return AuditComparison{}, err
	}
	if base.projectID != target.projectID {
		return AuditComparison{}, ErrAuditComparisonInvalid
	}
	result := AuditComparison{
		BaseAuditID: baseAuditID, TargetAuditID: targetAuditID,
		BaseFinishedAt: base.finishedAt, TargetFinishedAt: target.finishedAt,
		BaseCounts: base.counts, TargetCounts: target.counts,
		Metrics: []MetricDelta{},
	}
	baseValues, err := auditOverallValues(ctx, s.db, tenantID, baseAuditID)
	if err != nil {
		return AuditComparison{}, err
	}
	targetValues, err := auditOverallValues(ctx, s.db, tenantID, targetAuditID)
	if err != nil {
		return AuditComparison{}, err
	}
	for _, code := range trendMetricCodes {
		baseValue, targetValue := baseValues[code], targetValues[code]
		item := MetricDelta{
			MetricCode: code, BaseNumerator: baseValue.Numerator, BaseDenominator: baseValue.Denominator,
			TargetNumerator: targetValue.Numerator, TargetDenominator: targetValue.Denominator,
			BaseValue: baseValue.Value, TargetValue: targetValue.Value,
		}
		if baseValue.Value != nil && targetValue.Value != nil {
			delta := *targetValue.Value - *baseValue.Value
			item.Delta = &delta
		}
		result.Metrics = append(result.Metrics, item)
	}
	return result, nil
}

type auditSummary struct {
	projectID  string
	counts     AuditCounts
	finishedAt *time.Time
}

func loadAuditSummary(ctx context.Context, db *sql.DB, tenantID, auditID string) (auditSummary, error) {
	var summary auditSummary
	var finishedAt sql.NullTime
	err := db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),planned_count,completed_count,eligible_count,finished_at
FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, auditID).Scan(&summary.projectID, &summary.counts.Planned, &summary.counts.Completed, &summary.counts.Eligible, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auditSummary{}, ErrAuditNotFound
	}
	if err != nil {
		return auditSummary{}, err
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		summary.finishedAt = &value
	}
	return summary, nil
}

func auditOverallValues(ctx context.Context, db *sql.DB, tenantID, auditID string) (map[string]TrendValue, error) {
	snapshotID, _, err := latestMetricSnapshot(ctx, db, tenantID, auditID)
	if err != nil {
		return nil, err
	}
	if snapshotID == "" {
		return map[string]TrendValue{}, nil
	}
	return overallMetricValues(ctx, db, tenantID, snapshotID)
}
