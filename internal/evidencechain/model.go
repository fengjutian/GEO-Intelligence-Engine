package evidencechain

import "time"

const (
	OutcomePending      = "pending"
	OutcomeImproved     = "improved"
	OutcomeUnchanged    = "unchanged"
	OutcomeRegressed    = "regressed"
	OutcomeInconclusive = "inconclusive"
)

type UpsertInput struct {
	QuestionID       string `json:"questionId"`
	ResponseID       string `json:"responseId"`
	PageContentID    string `json:"pageContentId"`
	TaskID           string `json:"taskId"`
	RetestAuditID    string `json:"retestAuditId"`
	ChangeSummary    string `json:"changeSummary"`
	ExpectedOutcome  string `json:"expectedOutcome"`
	VerificationPlan string `json:"verificationPlan"`
	RetestOutcome    string `json:"retestOutcome"`
}

type QuestionEvidence struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type AnswerEvidence struct {
	ID       string `json:"id"`
	AuditID  string `json:"auditId"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Content  string `json:"content"`
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

type Chain struct {
	FindingID        string           `json:"findingId"`
	ProjectID        string           `json:"projectId"`
	Question         QuestionEvidence `json:"question"`
	Answer           AnswerEvidence   `json:"answer"`
	Page             PageEvidence     `json:"page"`
	Task             *TaskEvidence    `json:"task,omitempty"`
	ChangeSummary    string           `json:"changeSummary"`
	ExpectedOutcome  string           `json:"expectedOutcome"`
	VerificationPlan string           `json:"verificationPlan"`
	Retest           *RetestEvidence  `json:"retest,omitempty"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}
