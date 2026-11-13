package tasks

import "time"

// Task is an optimization task derived from a Finding.
type Task struct {
	ID          string           `json:"id"`
	ProjectID   string           `json:"projectId"`
	FindingID   string           `json:"findingId"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Status      string           `json:"status"`
	Priority    string           `json:"priority"`
	AssigneeID  *string          `json:"assigneeId"`
	DueAt       *time.Time       `json:"dueAt"`
	Version     int64            `json:"version"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	Acceptance  []AcceptanceItem `json:"acceptanceItems,omitempty"`
	Events      []Event          `json:"events,omitempty"`
}

// AcceptanceItem is one verifiable completion condition.
type AcceptanceItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Position    int    `json:"position"`
}

// Event is an immutable task history entry.
type Event struct {
	ID         string    `json:"id"`
	EventType  string    `json:"eventType"`
	FromState  string    `json:"fromState"`
	ToState    string    `json:"toState"`
	ActorID    string    `json:"actorId"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurredAt"`
}

// CreateInput is the body for creating a task from a Finding.
type CreateInput struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	AssigneeID      string   `json:"assigneeId"`
	DueAt           string   `json:"dueAt"`
	AcceptanceItems []string `json:"acceptanceItems"`
}

// TransitionInput requests one explicit state-machine edge.
type TransitionInput struct {
	TargetState string `json:"targetState"`
	Reason      string `json:"reason"`
}
