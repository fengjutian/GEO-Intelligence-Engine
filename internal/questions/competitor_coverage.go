package questions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// CompetitorQuestionCoverage describes how often competitors appear in the draft questions.
type CompetitorQuestionCoverage struct {
	Name            string `json:"name"`
	Domain          string `json:"domain,omitempty"`
	Mentions        int    `json:"mentions"`
	ComparisonCount int    `json:"comparisonCount"`
}

// QuestionCompetitorCoverage is the competitor coverage summary for a project.
type QuestionCompetitorCoverage struct {
	Total        int                          `json:"total"`
	Branded      int                          `json:"branded"`
	Competitors  []CompetitorQuestionCoverage `json:"competitors"`
	Uncovered    []string                     `json:"uncovered"`
	NoComparison []string                     `json:"noComparison"`
}

// CompetitorCoverage counts competitor mentions in the draft question set.
func (s *Store) CompetitorCoverage(ctx context.Context, tenantID, projectID string) (QuestionCompetitorCoverage, error) {
	result := QuestionCompetitorCoverage{Competitors: []CompetitorQuestionCoverage{}, Uncovered: []string{}, NoComparison: []string{}}
	var brandName string
	_ = s.db.QueryRowContext(ctx, `SELECT canonical_name FROM brands WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at,id LIMIT 1`, tenantID, projectID).Scan(&brandName)
	competitors := []CompetitorQuestionCoverage{}
	rows, err := s.db.QueryContext(ctx, `SELECT name,COALESCE(domain,'') FROM competitors WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND status='active' ORDER BY name`, tenantID, projectID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var item CompetitorQuestionCoverage
		if err := rows.Scan(&item.Name, &item.Domain); err != nil {
			rows.Close()
			return result, err
		}
		competitors = append(competitors, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, err
	}
	var versionID string
	err = s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(qsv.id) FROM question_sets qs
JOIN question_set_versions qsv ON qsv.question_set_id=qs.id
WHERE qs.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft'
ORDER BY qsv.version DESC LIMIT 1`, tenantID, projectID).Scan(&versionID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	questionRows, err := s.db.QueryContext(ctx, `SELECT text_value,attributes_json FROM questions WHERE tenant_id=UUID_TO_BIN(?) AND version_id=UUID_TO_BIN(?)`, tenantID, versionID)
	if err != nil {
		return result, err
	}
	type questionText struct {
		text    string
		branded bool
	}
	questions := []questionText{}
	for questionRows.Next() {
		var text string
		var attributes []byte
		if err := questionRows.Scan(&text, &attributes); err != nil {
			questionRows.Close()
			return result, err
		}
		brandType := parseAttribute(attributes, "brandType")
		questions = append(questions, questionText{text: text, branded: brandType == "branded"})
	}
	questionRows.Close()
	result.Total = len(questions)
	for _, question := range questions {
		if question.branded {
			result.Branded++
		}
	}
	for index := range competitors {
		name := strings.ToLower(competitors[index].Name)
		for _, question := range questions {
			if name == "" || !strings.Contains(strings.ToLower(question.text), name) {
				continue
			}
			competitors[index].Mentions++
			if isComparisonQuestion(question.text, brandName) {
				competitors[index].ComparisonCount++
			}
		}
		if competitors[index].Mentions == 0 {
			result.Uncovered = append(result.Uncovered, competitors[index].Name)
		}
		if competitors[index].ComparisonCount == 0 {
			result.NoComparison = append(result.NoComparison, competitors[index].Name)
		}
	}
	result.Competitors = competitors
	return result, nil
}

func parseAttribute(raw []byte, key string) string {
	value := map[string]any{}
	_ = json.Unmarshal(raw, &value)
	if text, ok := value[key].(string); ok {
		return text
	}
	return ""
}

func isComparisonQuestion(text, brandName string) bool {
	lowered := strings.ToLower(text)
	if brandName != "" && strings.Contains(lowered, strings.ToLower(brandName)) {
		for _, marker := range []string{"vs", "versus", "区别", "对比", "比较", "相比", "different", "compare"} {
			if strings.Contains(lowered, marker) {
				return true
			}
		}
	}
	return false
}
