package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound          = errors.New("task resource not found")
	ErrConflict          = errors.New("task state conflict")
	ErrInvalidInput      = errors.New("task input is invalid")
	ErrInvalidTransition = errors.New("transition is not allowed")
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

var allowedTransitions = map[string]map[string]bool{
	"todo":              {"in_progress": true, "cancelled": true},
	"in_progress":       {"in_review": true, "cancelled": true},
	"in_review":         {"done": true, "changes_requested": true, "in_progress": true},
	"changes_requested": {"in_progress": true, "cancelled": true},
	"done":              {},
	"cancelled":         {},
}

func (s *Store) CreateFromFinding(ctx context.Context, tenantID, actorID, findingID string, input CreateInput) (Task, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len(input.Title) > 500 || len(input.AcceptanceItems) == 0 {
		return Task{}, ErrInvalidInput
	}
	for _, item := range input.AcceptanceItems {
		if strings.TrimSpace(item) == "" {
			return Task{}, ErrInvalidInput
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	var projectID, severity, findingStatus string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),severity,status FROM findings WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, findingID).Scan(&projectID, &severity, &findingStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	priority := priorityForSeverity(severity)
	var inherited string
	if err := tx.QueryRowContext(ctx, `SELECT calculated_priority FROM finding_priorities WHERE finding_id=UUID_TO_BIN(?) ORDER BY created_at DESC LIMIT 1`, findingID).Scan(&inherited); err == nil && inherited != "" {
		priority = inherited
	}
	var assignee any
	if strings.TrimSpace(input.AssigneeID) != "" {
		if _, err := uuid.Parse(input.AssigneeID); err != nil {
			return Task{}, ErrInvalidInput
		}
		assignee = input.AssigneeID
	}
	var dueAt any
	if strings.TrimSpace(input.DueAt) != "" {
		parsed, err := time.Parse(time.RFC3339, input.DueAt)
		if err != nil {
			return Task{}, ErrInvalidInput
		}
		dueAt = parsed
	}
	taskID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO optimization_tasks (id,tenant_id,project_id,finding_id,title,description,status,priority,assignee_id,due_at,version,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'todo',?,UUID_TO_BIN(?),?,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		taskID.String(), tenantID, projectID, findingID, input.Title, input.Description, priority, assignee, dueAt)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}
	for index, description := range input.AcceptanceItems {
		itemID, _ := uuid.NewV7()
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_acceptance_items (id,tenant_id,task_id,description,status,position) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,'pending',?)`,
			itemID.String(), tenantID, taskID.String(), strings.TrimSpace(description), index); err != nil {
			return Task{}, fmt.Errorf("insert acceptance item: %w", err)
		}
	}
	linkID, _ := uuid.NewV7()
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_evidence_links (id,tenant_id,task_id,source_type,source_id,source_version) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'finding',UUID_TO_BIN(?),'v1')`,
		linkID.String(), tenantID, taskID.String(), findingID); err != nil {
		return Task{}, fmt.Errorf("link finding evidence: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{"findingId": findingID, "severity": severity, "findingStatus": findingStatus})
	if err := appendEvent(ctx, tx, tenantID, taskID.String(), actorID, "created", "", "todo", "", payload); err != nil {
		return Task{}, err
	}
	outboxID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events (id,tenant_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'task.created','optimization_task',UUID_TO_BIN(?),CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		outboxID.String(), tenantID, taskID.String(), payload)
	if err != nil {
		return Task{}, fmt.Errorf("enqueue task event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, tenantID, taskID.String())
}

func (s *Store) Transition(ctx context.Context, tenantID, actorID, taskID string, expectedVersion int64, input TransitionInput) (Task, error) {
	target := strings.TrimSpace(input.TargetState)
	reason := strings.TrimSpace(input.Reason)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	var status string
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT status,version FROM optimization_tasks WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, taskID).Scan(&status, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	if expectedVersion > 0 && version != expectedVersion {
		return Task{}, ErrConflict
	}
	if !allowedTransitions[status][target] {
		return Task{}, ErrInvalidTransition
	}
	if (target == "changes_requested" || target == "cancelled") && reason == "" {
		return Task{}, ErrInvalidInput
	}
	result, err := tx.ExecContext(ctx, `UPDATE optimization_tasks SET status=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?) AND version=?`, target, taskID, version)
	if err != nil {
		return Task{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Task{}, err
	}
	if affected == 0 {
		return Task{}, ErrConflict
	}
	if err := appendEvent(ctx, tx, tenantID, taskID, actorID, "transition", status, target, reason, nil); err != nil {
		return Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, tenantID, taskID)
}

func appendEvent(ctx context.Context, tx *sql.Tx, tenantID, taskID, actorID, eventType, from, to, reason string, payload []byte) error {
	eventID, _ := uuid.NewV7()
	if payload == nil {
		payload = []byte(`{}`)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_events (id,tenant_id,task_id,from_state,to_state,actor_id,event_type,reason,payload_json,occurred_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,UUID_TO_BIN(?),?,?,CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		eventID.String(), tenantID, taskID, nullable(from), nullable(to), actorID, eventType, nullable(reason), payload)
	if err != nil {
		return fmt.Errorf("append task event: %w", err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, tenantID, taskID string) (Task, error) {
	var task Task
	var assignee []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),BIN_TO_UUID(finding_id),title,COALESCE(description,''),status,priority,assignee_id,due_at,version,created_at,updated_at
FROM optimization_tasks WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, taskID).Scan(&task.ID, &task.ProjectID, &task.FindingID, &task.Title, &task.Description, &task.Status, &task.Priority, &assignee, &task.DueAt, &task.Version, &task.CreatedAt, &task.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	if len(assignee) > 0 {
		value := string(assignee)
		task.AssigneeID = &value
	}
	itemRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),description,status,position FROM task_acceptance_items WHERE tenant_id=UUID_TO_BIN(?) AND task_id=UUID_TO_BIN(?) ORDER BY position`, tenantID, taskID)
	if err != nil {
		return Task{}, err
	}
	task.Acceptance = []AcceptanceItem{}
	for itemRows.Next() {
		var item AcceptanceItem
		if err := itemRows.Scan(&item.ID, &item.Description, &item.Status, &item.Position); err != nil {
			itemRows.Close()
			return Task{}, err
		}
		task.Acceptance = append(task.Acceptance, item)
	}
	itemRows.Close()
	eventRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),event_type,COALESCE(from_state,''),COALESCE(to_state,''),BIN_TO_UUID(actor_id),COALESCE(reason,''),occurred_at
FROM task_events WHERE tenant_id=UUID_TO_BIN(?) AND task_id=UUID_TO_BIN(?) ORDER BY occurred_at,id`, tenantID, taskID)
	if err != nil {
		return Task{}, err
	}
	task.Events = []Event{}
	for eventRows.Next() {
		var event Event
		if err := eventRows.Scan(&event.ID, &event.EventType, &event.FromState, &event.ToState, &event.ActorID, &event.Reason, &event.OccurredAt); err != nil {
			eventRows.Close()
			return Task{}, err
		}
		task.Events = append(task.Events, event)
	}
	eventRows.Close()
	return task, nil
}

func (s *Store) List(ctx context.Context, tenantID, projectID, status string) ([]Task, error) {
	query := `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),BIN_TO_UUID(finding_id),title,status,priority,version,created_at,updated_at FROM optimization_tasks WHERE tenant_id=UUID_TO_BIN(?)`
	args := []any{tenantID}
	if projectID != "" {
		query += ` AND project_id=UUID_TO_BIN(?)`
		args = append(args, projectID)
	}
	if status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Task{}
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.FindingID, &task.Title, &task.Status, &task.Priority, &task.Version, &task.CreatedAt, &task.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, task)
	}
	return items, rows.Err()
}

func priorityForSeverity(severity string) string {
	switch severity {
	case "critical":
		return "P0"
	case "high":
		return "P1"
	case "medium":
		return "P2"
	default:
		return "P3"
	}
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
