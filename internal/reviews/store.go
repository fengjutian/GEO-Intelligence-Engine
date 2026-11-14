package reviews

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

var ErrNotFound = errors.New("review item was not found")
var ErrInvalid = errors.New("review input is invalid")
var ErrConflict = errors.New("review item is not pending")

// Enqueue creates a review item once per evaluation.
func Enqueue(ctx context.Context, tx *sql.Tx, tenantID, evaluationID, reasonCode, priority string) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_queue_items WHERE tenant_id=UUID_TO_BIN(?) AND evaluation_id=UUID_TO_BIN(?)`, tenantID, evaluationID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	id := uuid.NewString()
	_, err := tx.ExecContext(ctx, `INSERT INTO review_queue_items (id,tenant_id,evaluation_id,reason_code,priority,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'pending',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id, tenantID, evaluationID, reasonCode, priority)
	return err
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// EnqueueManual adds an evaluation to the review queue on demand.
func (s *Store) EnqueueManual(ctx context.Context, tenantID, evaluationID, reasonCode, priority string) error {
	if strings.TrimSpace(reasonCode) == "" {
		return ErrInvalid
	}
	switch priority {
	case "high", "medium", "low":
	default:
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM answer_evaluations WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, evaluationID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	if err := Enqueue(ctx, tx, tenantID, evaluationID, reasonCode, priority); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) List(ctx context.Context, tenantID, status, priority string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT BIN_TO_UUID(r.id),BIN_TO_UUID(r.evaluation_id),BIN_TO_UUID(s.audit_id),q.text_value,
r.reason_code,r.priority,r.status,COALESCE(ae.answer_outcome,''),COALESCE(ae.content_validity,''),
COALESCE(ae.recommendation_label,''),r.created_at
FROM review_queue_items r
JOIN answer_evaluations ae ON ae.id=r.evaluation_id
JOIN ai_responses resp ON resp.id=ae.response_id
JOIN audit_samples s ON s.id=resp.sample_id
JOIN questions q ON q.id=s.question_id
WHERE r.tenant_id=UUID_TO_BIN(?)`
	args := []any{tenantID}
	if status != "" {
		query += " AND r.status=?"
		args = append(args, status)
	}
	if priority != "" {
		query += " AND r.priority=?"
		args = append(args, priority)
	}
	query += " ORDER BY FIELD(r.priority,'high','medium','low'),r.created_at,r.id LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.EvaluationID, &item.AuditID, &item.Question, &item.ReasonCode, &item.Priority, &item.Status, &item.AnswerOutcome, &item.ContentValidity, &item.Recommendation, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Annotate(ctx context.Context, tenantID, itemID, annotatorID string, labels map[string]any, reason string) (Annotation, error) {
	reason = strings.TrimSpace(reason)
	if len(labels) == 0 || reason == "" {
		return Annotation{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Annotation{}, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM review_queue_items WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, itemID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Annotation{}, ErrNotFound
		}
		return Annotation{}, err
	}
	if status != "pending" {
		return Annotation{}, ErrConflict
	}
	labelsJSON, _ := json.Marshal(labels)
	annotation := Annotation{ID: uuid.NewString(), ReviewItemID: itemID, AnnotatorID: annotatorID, Labels: labels, Reason: reason, CreatedAt: time.Now().UTC()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO human_annotations (id,tenant_id,review_item_id,annotator_id,labels_json,reason,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),CAST(? AS JSON),?,UTC_TIMESTAMP(6))`,
		annotation.ID, tenantID, itemID, annotatorID, labelsJSON, reason); err != nil {
		return Annotation{}, fmt.Errorf("insert annotation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE review_queue_items SET status='reviewed',updated_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, itemID); err != nil {
		return Annotation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Annotation{}, err
	}
	return annotation, nil
}
