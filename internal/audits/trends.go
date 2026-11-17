package audits

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// TrendValue is one metric at a point in time.
type TrendValue struct {
	Value       *float64 `json:"value"`
	Numerator   *float64 `json:"numerator"`
	Denominator *float64 `json:"denominator"`
	CILow       *float64 `json:"ciLow"`
	CIHigh      *float64 `json:"ciHigh"`
}

// TrendPoint is one finished audit with its latest metric snapshot.
type TrendPoint struct {
	AuditID       string                `json:"auditId"`
	FinishedAt    *time.Time            `json:"finishedAt"`
	MetricVersion string                `json:"metricVersion"`
	Planned       int                   `json:"plannedCount"`
	Completed     int                   `json:"completedCount"`
	Eligible      int                   `json:"eligibleCount"`
	Metrics       map[string]TrendValue `json:"metrics"`
	Delta         map[string]*float64   `json:"delta"`
}

// AuditTrend is the cross-audit series for one project.
type AuditTrend struct {
	ProjectID string       `json:"projectId"`
	Metrics   []string     `json:"metrics"`
	Points    []TrendPoint `json:"points"`
}

var trendMetricCodes = []string{
	"mention_rate", "recommendation_rate", "refusal_rate", "positive_mention_rate",
	"official_citation_rate", "third_party_citation_rate", "eligible_response_rate",
	"official_citation_word_count", "official_citation_word_share", "official_position_adjusted_word_share", "official_first_citation_sentence_index",
}

// AuditTrends returns chronological metric points across finished audits.
func (s *Store) AuditTrends(ctx context.Context, tenantID, projectID string, limit int) (AuditTrend, error) {
	if limit < 1 || limit > 50 {
		limit = 12
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(id),planned_count,completed_count,eligible_count,finished_at
FROM audits
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND finished_at IS NOT NULL
ORDER BY finished_at DESC,id DESC LIMIT ?`, tenantID, projectID, limit)
	if err != nil {
		return AuditTrend{}, err
	}
	points := []TrendPoint{}
	for rows.Next() {
		var point TrendPoint
		var finishedAt sql.NullTime
		if err := rows.Scan(&point.AuditID, &point.Planned, &point.Completed, &point.Eligible, &finishedAt); err != nil {
			rows.Close()
			return AuditTrend{}, err
		}
		if finishedAt.Valid {
			value := finishedAt.Time
			point.FinishedAt = &value
		}
		point.Metrics = map[string]TrendValue{}
		point.Delta = map[string]*float64{}
		points = append(points, point)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return AuditTrend{}, err
	}
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
	for index := range points {
		snapshotID, version, err := latestMetricSnapshot(ctx, s.db, tenantID, points[index].AuditID)
		if err != nil {
			return AuditTrend{}, err
		}
		if snapshotID == "" {
			continue
		}
		points[index].MetricVersion = version
		values, err := overallMetricValues(ctx, s.db, tenantID, snapshotID)
		if err != nil {
			return AuditTrend{}, err
		}
		points[index].Metrics = values
		if index > 0 {
			for code, value := range values {
				if value.Value == nil {
					continue
				}
				if previous, ok := points[index-1].Metrics[code]; ok && previous.Value != nil {
					delta := *value.Value - *previous.Value
					points[index].Delta[code] = &delta
				}
			}
		}
	}
	return AuditTrend{ProjectID: projectID, Metrics: trendMetricCodes, Points: points}, nil
}

func latestMetricSnapshot(ctx context.Context, db *sql.DB, tenantID, auditID string) (string, string, error) {
	var snapshotID, version string
	err := db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),metric_version FROM metric_snapshots
WHERE tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?) AND status='ready'
ORDER BY generated_at DESC,id DESC LIMIT 1`, tenantID, auditID).Scan(&snapshotID, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return snapshotID, version, err
}

func overallMetricValues(ctx context.Context, db *sql.DB, tenantID, snapshotID string) (map[string]TrendValue, error) {
	rows, err := db.QueryContext(ctx, `SELECT metric_code,SUM(COALESCE(numerator,0)),SUM(COALESCE(denominator,0))
FROM metric_values WHERE tenant_id=UUID_TO_BIN(?) AND snapshot_id=UUID_TO_BIN(?)
AND aggregation_method='answer_equal' AND JSON_LENGTH(dimensions_json)<=1
GROUP BY metric_code`, tenantID, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]TrendValue{}
	allowed := map[string]bool{}
	for _, code := range trendMetricCodes {
		allowed[code] = true
	}
	for rows.Next() {
		var code string
		var numerator, denominator float64
		if err := rows.Scan(&code, &numerator, &denominator); err != nil {
			return nil, err
		}
		if !allowed[code] {
			continue
		}
		var value *float64
		if denominator > 0 {
			computed := numerator / denominator
			value = &computed
		}
		values[code] = TrendValue{Value: value, Numerator: &numerator, Denominator: &denominator}
	}
	return values, rows.Err()
}
