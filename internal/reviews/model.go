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
	AnnotationCount int       `json:"annotationCount"`
	AgreeCount      int       `json:"agreeCount"`
	DisagreeCount   int       `json:"disagreeCount"`
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

// AnnotationInput is one entry in a batch annotation request.
type AnnotationInput struct {
	ReviewItemID string         `json:"reviewItemId"`
	Labels       map[string]any `json:"labels"`
	Reason       string         `json:"reason"`
}

// AdjudicationInput is one entry in a batch adjudication request.
type AdjudicationInput struct {
	ReviewItemID string         `json:"reviewItemId"`
	Result       map[string]any `json:"result"`
	Reason       string         `json:"reason"`
}

// BatchOutcome reports the result of one item inside a batch request.
type BatchOutcome struct {
	ReviewItemID string `json:"reviewItemId"`
	Status       string `json:"status"`
	ErrorCode    string `json:"errorCode,omitempty"`
}

// BatchResult is the response body of every batch endpoint.
type BatchResult struct {
	Applied int            `json:"applied"`
	Failed  int            `json:"failed"`
	Results []BatchOutcome `json:"results"`
}

// HumanOverride is the Judge decision rewritten by a final adjudication.
type HumanOverride struct {
	AnswerOutcome       string `json:"answerOutcome,omitempty"`
	RecommendationLabel string `json:"recommendationLabel,omitempty"`
	ContentValidity     string `json:"contentValidity,omitempty"`
	Sentiment           string `json:"sentiment,omitempty"`
	Eligible            *bool  `json:"eligible,omitempty"`
}
