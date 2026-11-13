package audits

import "time"

// EngineConfigInput creates one provider/channel credential configuration.
type EngineConfigInput struct {
	Provider      string         `json:"provider"`
	Channel       string         `json:"channel"`
	CredentialRef string         `json:"credentialRef"`
	Region        string         `json:"region"`
	Limits        map[string]any `json:"limits"`
}

// EngineConfig never carries the credential reference back to callers.
type EngineConfig struct {
	ID        string         `json:"id"`
	Provider  string         `json:"provider"`
	Channel   string         `json:"channel"`
	Region    string         `json:"region"`
	Status    string         `json:"status"`
	Limits    map[string]any `json:"limits"`
	CreatedAt time.Time      `json:"createdAt"`
}

// ProtocolEngineInput binds one active engine configuration to a model selector.
type ProtocolEngineInput struct {
	EngineConfigID string         `json:"engineConfigId"`
	ModelSelector  string         `json:"modelSelector"`
	Channel        string         `json:"channel"`
	Parameters     map[string]any `json:"parameters"`
}

// ProtocolInput is the draft measurement protocol. Publishing freezes it.
type ProtocolInput struct {
	QuestionVersionID      string                `json:"questionVersionId"`
	Engines                []ProtocolEngineInput `json:"engines"`
	Region                 string                `json:"region"`
	Language               string                `json:"language"`
	SamplesPerQuestion     int                   `json:"samplesPerQuestion"`
	BudgetMicrounits       int64                 `json:"budgetMicrounits"`
	Currency               string                `json:"currency"`
	JudgeVersion           string                `json:"judgeVersion"`
	MetricVersion          string                `json:"metricVersion"`
	PromptTemplateVersion  string                `json:"promptTemplateVersion"`
	EstimatedCostPerSample int64                 `json:"estimatedCostPerSample"`
}

// Protocol is the persisted, versioned measurement protocol.
type Protocol struct {
	ID              string         `json:"id"`
	ProjectID       string         `json:"projectId"`
	SemanticVersion string         `json:"semanticVersion"`
	Status          string         `json:"status"`
	ConfigHash      string         `json:"configHash"`
	Config          map[string]any `json:"config"`
	CreatedAt       time.Time      `json:"createdAt"`
	PublishedAt     *time.Time     `json:"publishedAt"`
}

// QuestionSet summarizes the draft and published versions used to build protocols.
type QuestionSet struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Status         string            `json:"status"`
	DraftVersionID string            `json:"draftVersionId"`
	DraftVersion   int               `json:"draftVersion"`
	Versions       []QuestionVersion `json:"versions"`
}

// QuestionVersion is one immutable published question set version.
type QuestionVersion struct {
	ID            string    `json:"id"`
	Version       int       `json:"version"`
	QuestionCount int       `json:"questionCount"`
	PublishedAt   time.Time `json:"publishedAt"`
}

// Money is an integer microunit amount with an ISO currency.
type Money struct {
	AmountMicrounits int64  `json:"amountMicrounits"`
	Currency         string `json:"currency"`
}

// Estimate is returned before an audit is created.
type Estimate struct {
	PlannedSamples           int   `json:"plannedSamples"`
	EstimatedDurationSeconds int   `json:"estimatedDurationSeconds"`
	EstimatedCost            Money `json:"estimatedCost"`
}

// Audit is the orchestration aggregate.
type Audit struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"projectId"`
	ProtocolID       string     `json:"protocolId"`
	Status           string     `json:"status"`
	PlannedCount     uint32     `json:"plannedCount"`
	CompletedCount   uint32     `json:"completedCount"`
	EligibleCount    uint32     `json:"eligibleCount"`
	BudgetMicrounits int64      `json:"budgetMicrounits"`
	Currency         string     `json:"currency"`
	CreatedAt        time.Time  `json:"createdAt"`
	StartedAt        *time.Time `json:"startedAt"`
	FinishedAt       *time.Time `json:"finishedAt"`
}

// Sample is one expanded question × engine × sample-number unit.
type Sample struct {
	ID             string    `json:"id"`
	QuestionID     string    `json:"questionId"`
	EngineConfigID string    `json:"engineConfigId"`
	ModelSelector  string    `json:"modelSelector"`
	Channel        string    `json:"channel"`
	Region         string    `json:"region"`
	Language       string    `json:"language"`
	SampleNo       int       `json:"sampleNo"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
}
