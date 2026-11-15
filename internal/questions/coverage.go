package questions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Coverage summarizes the draft question space and its obvious gaps.
type Coverage struct {
	Total         int            `json:"total"`
	Branded       int            `json:"branded"`
	NonBranded    int            `json:"nonBranded"`
	ByType        map[string]int `json:"byType"`
	ByFunnel      map[string]int `json:"byFunnel"`
	ByLanguage    map[string]int `json:"byLanguage"`
	BySource      map[string]int `json:"bySource"`
	Clusters      int            `json:"clusters"`
	MissingTypes  []string       `json:"missingTypes"`
	MissingStages []string       `json:"missingStages"`
}

var coverageTypes = []string{"brand", "category", "comparison", "scenario", "decision"}
var coverageStages = []string{"awareness", "consideration", "decision"}

// Coverage computes draft question distribution and missing categories.
func (s *Store) Coverage(ctx context.Context, tenantID, projectID string) (Coverage, error) {
	var versionID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(qsv.id) FROM question_sets qs
JOIN question_set_versions qsv ON qsv.question_set_id=qs.id
WHERE qs.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft'
ORDER BY qsv.version DESC LIMIT 1`, tenantID, projectID).Scan(&versionID)
	if errors.Is(err, sql.ErrNoRows) {
		return Coverage{ByType: map[string]int{}, ByFunnel: map[string]int{}, ByLanguage: map[string]int{}, BySource: map[string]int{}, MissingTypes: coverageTypes, MissingStages: coverageStages}, nil
	}
	if err != nil {
		return Coverage{}, err
	}
	result := Coverage{ByType: map[string]int{}, ByFunnel: map[string]int{}, ByLanguage: map[string]int{}, BySource: map[string]int{}}
	rows, err := s.db.QueryContext(ctx, `SELECT question_type,COALESCE(funnel_stage,''),language,attributes_json
FROM questions WHERE tenant_id=UUID_TO_BIN(?) AND version_id=UUID_TO_BIN(?)`, tenantID, versionID)
	if err != nil {
		return Coverage{}, err
	}
	for rows.Next() {
		var questionType, funnel, language string
		var attributes []byte
		if err := rows.Scan(&questionType, &funnel, &language, &attributes); err != nil {
			rows.Close()
			return Coverage{}, err
		}
		result.Total++
		result.ByType[questionType]++
		if funnel == "" {
			funnel = "unknown"
		}
		result.ByFunnel[funnel]++
		result.ByLanguage[language]++
		source, brandType := "manual", "non_branded"
		parsed := map[string]any{}
		_ = json.Unmarshal(attributes, &parsed)
		if value, ok := parsed["source"].(string); ok && value != "" {
			source = value
		}
		if value, ok := parsed["brandType"].(string); ok && value != "" {
			brandType = value
		}
		result.BySource[source]++
		if brandType == "branded" {
			result.Branded++
		} else {
			result.NonBranded++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Coverage{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT qc.id) FROM query_clusters qc WHERE qc.tenant_id=UUID_TO_BIN(?) AND qc.question_version_id=UUID_TO_BIN(?)`, tenantID, versionID).Scan(&result.Clusters); err != nil {
		return Coverage{}, err
	}
	for _, questionType := range coverageTypes {
		if result.ByType[questionType] == 0 {
			result.MissingTypes = append(result.MissingTypes, questionType)
		}
	}
	for _, stage := range coverageStages {
		if result.ByFunnel[stage] == 0 {
			result.MissingStages = append(result.MissingStages, stage)
		}
	}
	return result, nil
}
