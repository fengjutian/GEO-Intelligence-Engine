package audits

import (
	"context"
	"time"
)

// Answer is one stored model answer with its Judge outcome.
type Answer struct {
	ID              string    `json:"id"`
	Question        string    `json:"question"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Content         string    `json:"content"`
	ContentValidity string    `json:"contentValidity"`
	AnswerOutcome   string    `json:"answerOutcome"`
	Eligible        bool      `json:"eligible"`
	CreatedAt       time.Time `json:"createdAt"`
}

// ListAnswers returns stored answer text for one audit.
func (s *Store) ListAnswers(ctx context.Context, tenantID, auditID string, limit int) ([]Answer, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(r.id),q.text_value,COALESCE(ec.provider,''),COALESCE(r.resolved_model,''),
COALESCE(r.content_text,''),COALESCE(ae.content_validity,''),COALESCE(ae.answer_outcome,''),COALESCE(ae.eligible_for_core_metrics,FALSE),r.created_at
FROM ai_responses r
JOIN audit_samples s ON s.id=r.sample_id
JOIN questions q ON q.id=s.question_id
JOIN engine_configs ec ON ec.id=s.engine_config_id
LEFT JOIN answer_evaluations ae ON ae.response_id=r.id
WHERE r.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?)
ORDER BY r.created_at DESC,r.id DESC LIMIT ?`, tenantID, auditID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Answer{}
	for rows.Next() {
		var item Answer
		if err := rows.Scan(&item.ID, &item.Question, &item.Provider, &item.Model, &item.Content, &item.ContentValidity, &item.AnswerOutcome, &item.Eligible, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
