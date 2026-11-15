package audits

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

// EngineSeries is one engine's metrics at one audit point.
type EngineSeries struct {
	EngineConfigID string                `json:"engineConfigId"`
	Provider       string                `json:"provider"`
	ModelSelector  string                `json:"modelSelector"`
	Metrics        map[string]TrendValue `json:"metrics"`
	Delta          map[string]*float64   `json:"delta"`
}

// EngineTrendPoint is one audit with per-engine metric series.
type EngineTrendPoint struct {
	AuditID    string         `json:"auditId"`
	FinishedAt *time.Time     `json:"finishedAt"`
	Engines    []EngineSeries `json:"engines"`
}

// EngineTrend is a project's per-engine metric series across audits.
type EngineTrend struct {
	ProjectID string             `json:"projectId"`
	Metrics   []string           `json:"metrics"`
	Points    []EngineTrendPoint `json:"points"`
}

// AuditEngineTrends returns per-engine metric points across finished audits.
func (s *Store) AuditEngineTrends(ctx context.Context, tenantID, projectID string, limit int) (EngineTrend, error) {
	if limit < 1 || limit > 50 {
		limit = 12
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(id),finished_at FROM audits
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND finished_at IS NOT NULL
ORDER BY finished_at DESC,id DESC LIMIT ?`, tenantID, projectID, limit)
	if err != nil {
		return EngineTrend{}, err
	}
	points := []EngineTrendPoint{}
	for rows.Next() {
		var point EngineTrendPoint
		var finishedAt sql.NullTime
		if err := rows.Scan(&point.AuditID, &finishedAt); err != nil {
			rows.Close()
			return EngineTrend{}, err
		}
		if finishedAt.Valid {
			value := finishedAt.Time
			point.FinishedAt = &value
		}
		points = append(points, point)
	}
	rows.Close()
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
	for index := range points {
		snapshotID, _, err := latestMetricSnapshot(ctx, s.db, tenantID, points[index].AuditID)
		if err != nil {
			return EngineTrend{}, err
		}
		if snapshotID == "" {
			points[index].Engines = []EngineSeries{}
			continue
		}
		engines, err := engineMetricValues(ctx, s.db, tenantID, snapshotID)
		if err != nil {
			return EngineTrend{}, err
		}
		points[index].Engines = engines
		if index > 0 {
			previous := map[string]EngineSeries{}
			for _, engine := range points[index-1].Engines {
				previous[engine.EngineConfigID] = engine
			}
			for engineIndex := range points[index].Engines {
				engine := &points[index].Engines[engineIndex]
				before, ok := previous[engine.EngineConfigID]
				if !ok {
					continue
				}
				for code, value := range engine.Metrics {
					if value.Value == nil || before.Metrics[code].Value == nil {
						continue
					}
					delta := *value.Value - *before.Metrics[code].Value
					engine.Delta[code] = &delta
				}
			}
		}
	}
	return EngineTrend{ProjectID: projectID, Metrics: trendMetricCodes, Points: points}, nil
}

func engineMetricValues(ctx context.Context, db *sql.DB, tenantID, snapshotID string) ([]EngineSeries, error) {
	rows, err := db.QueryContext(ctx, `SELECT
COALESCE(JSON_UNQUOTE(JSON_EXTRACT(dimensions_json,'$.engineConfigId')),''),
COALESCE(JSON_UNQUOTE(JSON_EXTRACT(dimensions_json,'$.provider')),''),
COALESCE(JSON_UNQUOTE(JSON_EXTRACT(dimensions_json,'$.modelSelector')),''),
metric_code,numerator,denominator
FROM metric_values WHERE tenant_id=UUID_TO_BIN(?) AND snapshot_id=UUID_TO_BIN(?)
AND aggregation_method='answer_equal' AND JSON_EXTRACT(dimensions_json,'$.engineConfigId') IS NOT NULL`, tenantID, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	series := map[string]*EngineSeries{}
	allowed := map[string]bool{}
	for _, code := range trendMetricCodes {
		allowed[code] = true
	}
	for rows.Next() {
		var engineID, provider, model, code string
		var numerator, denominator sql.NullFloat64
		if err := rows.Scan(&engineID, &provider, &model, &code, &numerator, &denominator); err != nil {
			return nil, err
		}
		if engineID == "" || !allowed[code] {
			continue
		}
		entry := series[engineID]
		if entry == nil {
			entry = &EngineSeries{EngineConfigID: engineID, Provider: provider, ModelSelector: model, Metrics: map[string]TrendValue{}, Delta: map[string]*float64{}}
			series[engineID] = entry
		}
		var value *float64
		if denominator.Valid && denominator.Float64 > 0 && numerator.Valid {
			computed := numerator.Float64 / denominator.Float64
			value = &computed
		}
		entry.Metrics[code] = TrendValue{Value: value, Numerator: floatPtr(numerator), Denominator: floatPtr(denominator)}
	}
	result := make([]EngineSeries, 0, len(series))
	for _, entry := range series {
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		return result[i].ModelSelector < result[j].ModelSelector
	})
	return result, rows.Err()
}

func floatPtr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}
