package audits

import (
	"context"
	"database/sql"
	"time"
)

// QuestionTrendPoint is one question's result inside one audit.
type QuestionTrendPoint struct {
	AuditID         string     `json:"auditId"`
	FinishedAt      *time.Time `json:"finishedAt"`
	Total           int        `json:"total"`
	Eligible        int        `json:"eligible"`
	Recommendations int        `json:"recommendations"`
	Refusals        int        `json:"refusals"`
}

// QuestionTrend is one question's history across audits.
type QuestionTrend struct {
	QuestionID string               `json:"questionId"`
	Text       string               `json:"text"`
	Points     []QuestionTrendPoint `json:"points"`
}

// QuestionTrends aggregates per-question outcomes across finished audits.
func (s *Store) QuestionTrends(ctx context.Context, tenantID, projectID string, limit int) ([]QuestionTrend, error) {
	if limit < 1 || limit > 50 {
		limit = 12
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),finished_at FROM audits
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND finished_at IS NOT NULL
ORDER BY finished_at DESC,id DESC LIMIT ?`, tenantID, projectID, limit)
	if err != nil {
		return nil, err
	}
	type auditPoint struct {
		id         string
		finishedAt *time.Time
	}
	audits := []auditPoint{}
	for rows.Next() {
		var item auditPoint
		var finishedAt sql.NullTime
		if err := rows.Scan(&item.id, &finishedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if finishedAt.Valid {
			value := finishedAt.Time
			item.finishedAt = &value
		}
		audits = append(audits, item)
	}
	rows.Close()
	trends := map[string]*QuestionTrend{}
	order := []string{}
	for _, audit := range audits {
		sampleRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(q.id),q.text_value,
COALESCE(ae.answer_outcome,''),COALESCE(ae.eligible_for_core_metrics,FALSE)
FROM audit_samples s
JOIN questions q ON q.id=s.question_id
LEFT JOIN ai_responses r ON r.sample_id=s.id
LEFT JOIN answer_evaluations ae ON ae.response_id=r.id
AND ae.evaluation_run_id=(SELECT er.id FROM evaluation_runs er WHERE er.audit_id=s.audit_id ORDER BY er.created_at DESC,er.id DESC LIMIT 1)
WHERE s.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?)`, tenantID, audit.id)
		if err != nil {
			return nil, err
		}
		byQuestion := map[string]*QuestionTrendPoint{}
		for sampleRows.Next() {
			var questionID, text, outcome string
			var eligible bool
			if err := sampleRows.Scan(&questionID, &text, &outcome, &eligible); err != nil {
				sampleRows.Close()
				return nil, err
			}
			trend := trends[questionID]
			if trend == nil {
				trend = &QuestionTrend{QuestionID: questionID, Text: text}
				trends[questionID] = trend
				order = append(order, questionID)
			}
			point := byQuestion[questionID]
			if point == nil {
				point = &QuestionTrendPoint{AuditID: audit.id, FinishedAt: audit.finishedAt}
				byQuestion[questionID] = point
			}
			point.Total++
			if eligible {
				point.Eligible++
			}
			switch outcome {
			case "recommendation":
				point.Recommendations++
			case "refusal":
				point.Refusals++
			}
		}
		sampleRows.Close()
		for questionID, point := range byQuestion {
			trends[questionID].Points = append(trends[questionID].Points, *point)
		}
	}
	result := make([]QuestionTrend, 0, len(order))
	for _, questionID := range order {
		result = append(result, *trends[questionID])
	}
	return result, nil
}
