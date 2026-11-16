package evidencechain

import "time"

const (
	OutcomePending      = "pending"
	OutcomeImproved     = "improved"
	OutcomeUnchanged    = "unchanged"
	OutcomeRegressed    = "regressed"
	OutcomeInconclusive = "inconclusive"

	StatusEvidenceReady  = "evidence_ready"
	StatusPlanned        = "planned"
	StatusImplementing   = "implementing"
	StatusAwaitingRetest = "awaiting_retest"
	StatusEvaluating     = "evaluating"
	StatusVerified       = "verified"
	StatusIneffective    = "ineffective"
	StatusInconclusive   = "inconclusive"
)

type UpsertInput struct {
	QuestionID       string  `json:"questionId"`
	ResponseID       string  `json:"responseId"`
	PageContentID    string  `json:"pageContentId"`
	TaskID           string  `json:"taskId"`
	RetestAuditID    string  `json:"retestAuditId"`
	ChangeSummary    string  `json:"changeSummary"`
	ExpectedOutcome  string  `json:"expectedOutcome"`
	VerificationPlan string  `json:"verificationPlan"`
	MetricCode       string  `json:"metricCode"`
	TargetDelta      float64 `json:"targetDelta"`
}

type TransitionInput struct {
	TargetStatus  string `json:"targetStatus"`
	Reason        string `json:"reason"`
	RetestAuditID string `json:"retestAuditId"`
}

type QuestionEvidence struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type AnswerEvidence struct {
	ID          string `json:"id"`
	AuditID     string `json:"auditId"`
	Provider    string `json:"provider"`
	Channel     string `json:"channel"`
	Model       string `json:"model"`
	Content     string `json:"content"`
	ContentHash string `json:"contentHash"`
}

type PageEvidence struct {
	ContentID   string `json:"contentId"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	ContentHash string `json:"contentHash"`
}

type TaskEvidence struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type RetestEvidence struct {
	AuditID string `json:"auditId"`
	Status  string `json:"status"`
	Outcome string `json:"outcome"`
}

type ProtocolSnapshot struct {
	AuditID               string `json:"auditId"`
	QuestionVersionID     string `json:"questionVersionId"`
	ProtocolVersion       string `json:"protocolVersion"`
	ProtocolHash          string `json:"protocolHash"`
	MetricVersion         string `json:"metricVersion"`
	JudgeVersion          string `json:"judgeVersion"`
	PromptTemplateVersion string `json:"promptTemplateVersion"`
	Channel               string `json:"channel"`
	ModelSelector         string `json:"modelSelector"`
	Region                string `json:"region"`
	Language              string `json:"language"`
}

type MetricPoint struct {
	MetricCode  string   `json:"metricCode"`
	Numerator   *float64 `json:"numerator"`
	Denominator *float64 `json:"denominator"`
	Value       *float64 `json:"value"`
	CILow       *float64 `json:"ciLow"`
	CIHigh      *float64 `json:"ciHigh"`
}

type EvidenceSnapshot struct {
	Question   QuestionEvidence `json:"question"`
	Answer     AnswerEvidence   `json:"answer"`
	Page       PageEvidence     `json:"page"`
	Protocol   ProtocolSnapshot `json:"protocol"`
	Baseline   MetricPoint      `json:"baseline"`
	CapturedAt time.Time        `json:"capturedAt"`
}

type Comparison struct {
	Comparable  bool        `json:"comparable"`
	Differences []string    `json:"differences"`
	Baseline    MetricPoint `json:"baseline"`
	Retest      MetricPoint `json:"retest"`
	Delta       *float64    `json:"delta"`
	Decision    string      `json:"decision"`
	Reason      string      `json:"reason"`
}

type Event struct {
	ID         string    `json:"id"`
	EventType  string    `json:"eventType"`
	FromStatus string    `json:"fromStatus"`
	ToStatus   string    `json:"toStatus"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurredAt"`
}

type Chain struct {
	FindingID        string           `json:"findingId"`
	ProjectID        string           `json:"projectId"`
	Status           string           `json:"status"`
	Version          uint64           `json:"version"`
	Question         QuestionEvidence `json:"question"`
	Answer           AnswerEvidence   `json:"answer"`
	Page             PageEvidence     `json:"page"`
	Task             *TaskEvidence    `json:"task,omitempty"`
	ChangeSummary    string           `json:"changeSummary"`
	ExpectedOutcome  string           `json:"expectedOutcome"`
	VerificationPlan string           `json:"verificationPlan"`
	MetricCode       string           `json:"metricCode"`
	TargetDelta      float64          `json:"targetDelta"`
	Snapshot         EvidenceSnapshot `json:"snapshot"`
	Comparison       *Comparison      `json:"comparison,omitempty"`
	Retest           *RetestEvidence  `json:"retest,omitempty"`
	Events           []Event          `json:"events"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

type AnswerCandidate struct {
	QuestionID string  `json:"questionId"`
	Question   string  `json:"question"`
	ResponseID string  `json:"responseId"`
	AuditID    string  `json:"auditId"`
	Provider   string  `json:"provider"`
	Channel    string  `json:"channel"`
	Model      string  `json:"model"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
}

type Candidates struct {
	FindingID string            `json:"findingId"`
	Summary   string            `json:"summary"`
	Page      PageEvidence      `json:"page"`
	Answers   []AnswerCandidate `json:"answers"`
}
