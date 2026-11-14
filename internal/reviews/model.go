package reviews

import "time"

// Item is one answer evaluation waiting for human review.
type Item struct {
	ID              string    `json:"id"`
	EvaluationID    string    `json:"evaluationId"`
	AuditID         string    `json:"auditId"`
	Question        string    `json:"question"`
	ReasonCode      string    `json:"reasonCode"`
	Priority        string    `json:"priority"`
	Status          string    `json:"status"`
	AnswerOutcome   string    `json:"answerOutcome"`
	ContentValidity string    `json:"contentValidity"`
	Recommendation  string    `json:"recommendationLabel"`
	AssigneeID      *string   `json:"assigneeId"`
	CreatedAt       time.Time `json:"createdAt"`
}

// Annotation is one human review decision.
type Annotation struct {
	ID           string         `json:"id"`
	ReviewItemID string         `json:"reviewItemId"`
	AnnotatorID  string         `json:"annotatorId"`
	Labels       map[string]any `json:"labels"`
	Reason       string         `json:"reason"`
	CreatedAt    time.Time      `json:"createdAt"`
}

// Adjudication is the final decision for a reviewed item.
type Adjudication struct {
	ID            string         `json:"id"`
	ReviewItemID  string         `json:"reviewItemId"`
	AdjudicatorID string         `json:"adjudicatorId"`
	Result        map[string]any `json:"result"`
	Reason        string         `json:"reason"`
	CreatedAt     time.Time      `json:"createdAt"`
}
