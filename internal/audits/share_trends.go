package audits

import (
	"context"
	"database/sql"
	"time"
)

// ShareSeries is one entity's share at one audit point.
type ShareSeries struct {
	Entity      string   `json:"entity"`
	EntityType  string   `json:"entityType"`
	Share       *float64 `json:"share"`
	Answers     *float64 `json:"answers"`
	Denominator *float64 `json:"denominator"`
	Delta       *float64 `json:"delta"`
}

// ShareTrendPoint is one audit with its entity share series.
type ShareTrendPoint struct {
	AuditID    string        `json:"auditId"`
	FinishedAt *time.Time    `json:"finishedAt"`
	Entities   []ShareSeries `json:"entities"`
}

// ShareTrend is the cross-audit share-of-voice series for one project.
type ShareTrend struct {
	ProjectID string            `json:"projectId"`
	Points    []ShareTrendPoint `json:"points"`
}

// AuditShareTrends returns share_of_voice series across finished audits.
func (s *Store) AuditShareTrends(ctx context.Context, tenantID, projectID string, limit int) (ShareTrend, error) {
	if limit < 1 || limit > 50 {
		limit = 12
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),finished_at FROM audits
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND finished_at IS NOT NULL
ORDER BY finished_at DESC,id DESC LIMIT ?`, tenantID, projectID, limit)
	if err != nil {
		return ShareTrend{}, err
	}
	points := []ShareTrendPoint{}
	for rows.Next() {
		var point ShareTrendPoint
		var finishedAt sql.NullTime
		if err := rows.Scan(&point.AuditID, &finishedAt); err != nil {
			rows.Close()
			return ShareTrend{}, err
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
			return ShareTrend{}, err
		}
		if snapshotID == "" {
			points[index].Entities = []ShareSeries{}
			continue
		}
		entities, err := shareSeries(ctx, s.db, tenantID, snapshotID)
		if err != nil {
			return ShareTrend{}, err
		}
		points[index].Entities = entities
		if index > 0 {
			previous := map[string]ShareSeries{}
			for _, entity := range points[index-1].Entities {
				previous[entity.EntityType+"|"+entity.Entity] = entity
			}
			for entityIndex := range points[index].Entities {
				entity := &points[index].Entities[entityIndex]
				before, ok := previous[entity.EntityType+"|"+entity.Entity]
				if !ok || entity.Share == nil || before.Share == nil {
					continue
				}
				delta := *entity.Share - *before.Share
				entity.Delta = &delta
			}
		}
	}
	return ShareTrend{ProjectID: projectID, Points: points}, nil
}

func shareSeries(ctx context.Context, db *sql.DB, tenantID, snapshotID string) ([]ShareSeries, error) {
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(dimensions_json,'$.entity')),''),
COALESCE(JSON_UNQUOTE(JSON_EXTRACT(dimensions_json,'$.entityType')),''),
value_decimal,numerator,denominator
FROM metric_values WHERE tenant_id=UUID_TO_BIN(?) AND snapshot_id=UUID_TO_BIN(?)
AND metric_code='share_of_voice' AND aggregation_method='answer_equal'
ORDER BY value_decimal DESC`, tenantID, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ShareSeries{}
	for rows.Next() {
		var item ShareSeries
		var value, numerator, denominator sql.NullFloat64
		if err := rows.Scan(&item.Entity, &item.EntityType, &value, &numerator, &denominator); err != nil {
			return nil, err
		}
		if item.Entity == "" {
			continue
		}
		item.Share = floatPtr(value)
		item.Answers = floatPtr(numerator)
		item.Denominator = floatPtr(denominator)
		items = append(items, item)
	}
	return items, rows.Err()
}
