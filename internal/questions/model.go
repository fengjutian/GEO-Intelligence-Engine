package questions

import "time"

type CreateInput struct {
	Text         string   `json:"text"`
	Language     string   `json:"language"`
	QuestionType string   `json:"questionType"`
	Intent       string   `json:"intent,omitempty"`
	Topic        string   `json:"topic,omitempty"`
	FunnelStage  string   `json:"funnelStage,omitempty"`
	Weight       *float64 `json:"weight,omitempty"`
}

type Question struct {
	ID           string    `json:"id"`
	Text         string    `json:"text"`
	Language     string    `json:"language"`
	QuestionType string    `json:"questionType"`
	Intent       string    `json:"intent,omitempty"`
	Topic        string    `json:"topic,omitempty"`
	FunnelStage  string    `json:"funnelStage,omitempty"`
	BrandType    string    `json:"brandType"`
	Source       string    `json:"source"`
	Weight       float64   `json:"weight"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Page struct {
	Items []Question `json:"items"`
	Page  PageMeta   `json:"page"`
}

type PageMeta struct {
	HasMore bool `json:"hasMore"`
}

// GenerationRun records one deterministic question-generation execution.
type GenerationRun struct {
	ID               string     `json:"id"`
	Status           string     `json:"status"`
	GeneratorVersion string     `json:"generatorVersion"`
	Model            string     `json:"model,omitempty"`
	Generated        int        `json:"generated"`
	Skipped          int        `json:"skipped"`
	CreatedAt        time.Time  `json:"createdAt"`
	FinishedAt       *time.Time `json:"finishedAt"`
}

// Candidate is one question proposed by a template or LLM generator.
type Candidate struct {
	Text         string `json:"text"`
	QuestionType string `json:"questionType"`
	Intent       string `json:"intent"`
	Topic        string `json:"topic"`
	FunnelStage  string `json:"funnelStage"`
	Basis        string `json:"basis"`
	Branded      bool   `json:"branded"`
}

// GenerationContext is the frozen knowledge used to build a prompt.
type GenerationContext struct {
	SnapshotID  string
	Brand       string
	Products    []string
	Competitors []string
	Entities    []string
	Language    string
}
