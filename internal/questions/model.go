package questions

import "time"

type CreateInput struct {
	Text         string   `json:"text"`
	Language     string   `json:"language"`
	QuestionType string   `json:"questionType"`
	Intent       string   `json:"intent,omitempty"`
	Weight       *float64 `json:"weight,omitempty"`
}

type Question struct {
	ID           string    `json:"id"`
	Text         string    `json:"text"`
	Language     string    `json:"language"`
	QuestionType string    `json:"questionType"`
	Intent       string    `json:"intent,omitempty"`
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
