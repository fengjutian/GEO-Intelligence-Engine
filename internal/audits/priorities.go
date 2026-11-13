package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"github.com/google/uuid"
)

const PriorityVersion = "priority-v1"

var priorityWeights = map[string]float64{
	"impact":             0.35,
	"confidence":         0.20,
	"businessImportance": 0.20,
	"fixCost":            0.10,
	"risk":               0.15,
}

// calculatePriority turns versioned explainable components into a P0-P3 label.
func calculatePriority(severity string, confidence float64) (string, float64, map[string]any) {
	impact := map[string]float64{"critical": 1.0, "high": 0.8, "medium": 0.5, "low": 0.25}[severity]
	if impact == 0 {
		impact = 0.25
	}
	fixCost := map[string]float64{"critical": 0.9, "high": 0.7, "medium": 0.5, "low": 0.3}[severity]
	risk := map[string]float64{"critical": 1.0, "high": 0.8, "medium": 0.5, "low": 0.3}[severity]
	if confidence <= 0 {
		confidence = 1.0
	}
	confidence = math.Min(1, confidence)
	businessImportance := 0.5
	score := impact*priorityWeights["impact"] +
		confidence*priorityWeights["confidence"] +
		businessImportance*priorityWeights["businessImportance"] +
		(1-fixCost)*priorityWeights["fixCost"] +
		risk*priorityWeights["risk"]
	priority := "P3"
	switch {
	case score >= 0.80:
		priority = "P0"
	case score >= 0.70:
		priority = "P1"
	case score >= 0.55:
		priority = "P2"
	}
	components := map[string]any{
		"impact": impact, "confidence": confidence, "businessImportance": businessImportance,
		"fixCost": fixCost, "risk": risk, "score": score,
		"weights": priorityWeights, "priorityVersion": PriorityVersion,
	}
	return priority, score, components
}

// generatePriorities persists deterministic finding priorities and returns the report rows.
func generatePriorities(ctx context.Context, tx *sql.Tx, tenantID, projectID string) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(id),rule_code,severity,confidence,status,summary
FROM findings WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND status='open'
ORDER BY FIELD(severity,'critical','high','medium','low'),created_at DESC LIMIT 100`, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("load findings for priorities: %w", err)
	}
	defer rows.Close()
	type pending struct {
		id, code, severity, status, summary string
		priority, score                     any
		components                          map[string]any
	}
	items := []pending{}
	for rows.Next() {
		var item pending
		var confidence float64
		if err := rows.Scan(&item.id, &item.code, &item.severity, &confidence, &item.status, &item.summary); err != nil {
			return nil, err
		}
		priority, score, components := calculatePriority(item.severity, confidence)
		item.priority, item.score, item.components = priority, score, components
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, item := range items {
		components, _ := json.Marshal(item.components)
		priorityID, _ := uuid.NewV7()
		if _, err := tx.ExecContext(ctx, `INSERT INTO finding_priorities (id,tenant_id,finding_id,calculation_version,components_json,calculated_priority,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,CAST(? AS JSON),?,UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE components_json=VALUES(components_json),calculated_priority=VALUES(calculated_priority)`,
			priorityID.String(), tenantID, item.id, PriorityVersion, components, item.priority); err != nil {
			return nil, fmt.Errorf("persist finding priority: %w", err)
		}
		result = append(result, map[string]any{
			"findingId": item.id, "ruleCode": item.code, "severity": item.severity,
			"priority": item.priority, "score": item.score, "components": item.components,
		})
	}
	return result, nil
}
