package questions

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrQuestionNotFound = errors.New("question was not found")
var ErrQuestionLocked = errors.New("question version is not editable")
var ErrInvalidStatus = errors.New("question status transition is invalid")

// UpdateInput patches editable question attributes.
type UpdateInput struct {
	Text         *string  `json:"text,omitempty"`
	Intent       *string  `json:"intent,omitempty"`
	Topic        *string  `json:"topic,omitempty"`
	FunnelStage  *string  `json:"funnelStage,omitempty"`
	QuestionType *string  `json:"questionType,omitempty"`
	Weight       *float64 `json:"weight,omitempty"`
	Status       *string  `json:"status,omitempty"`
}

var statusTransitions = map[string]map[string]bool{
	"draft":    {"draft": true, "reviewed": true, "active": true, "retired": true},
	"reviewed": {"reviewed": true, "active": true, "retired": true},
	"active":   {"active": true, "retired": true},
	"retired":  {"retired": true},
}

// Update mutates one draft question and applies status transitions.
func (s *Store) Update(ctx context.Context, tenantID, questionID string, input UpdateInput) (Question, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Question{}, err
	}
	defer tx.Rollback()
	var current Question
	var versionID, versionStatus, attributesRaw string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(q.id),q.text_value,q.language,q.question_type,COALESCE(q.intent,''),COALESCE(q.topic,''),COALESCE(q.funnel_stage,''),q.weight,q.status,COALESCE(q.attributes_json,'{}'),BIN_TO_UUID(q.version_id),qsv.status
FROM questions q JOIN question_set_versions qsv ON qsv.id=q.version_id
WHERE q.tenant_id=UUID_TO_BIN(?) AND q.id=UUID_TO_BIN(?)`, tenantID, questionID).Scan(&current.ID, &current.Text, &current.Language, &current.QuestionType, &current.Intent, &current.Topic, &current.FunnelStage, &current.Weight, &current.Status, &attributesRaw, &versionID, &versionStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, ErrQuestionNotFound
	}
	if err != nil {
		return Question{}, err
	}
	if versionStatus != "draft" {
		return Question{}, ErrQuestionLocked
	}
	status := current.Status
	if input.Status != nil {
		status = strings.TrimSpace(*input.Status)
	}
	if !statusTransitions[current.Status][status] {
		return Question{}, ErrInvalidStatus
	}
	if input.QuestionType != nil {
		value := strings.TrimSpace(*input.QuestionType)
		switch value {
		case "brand", "category", "comparison", "scenario", "decision":
			current.QuestionType = value
		default:
			return Question{}, ErrInvalidImport
		}
	}
	if input.Intent != nil {
		current.Intent = strings.TrimSpace(*input.Intent)
	}
	if input.Topic != nil {
		current.Topic = strings.TrimSpace(*input.Topic)
	}
	if input.FunnelStage != nil {
		current.FunnelStage = strings.TrimSpace(*input.FunnelStage)
	}
	if input.Weight != nil {
		if *input.Weight < 0 || *input.Weight > 1000 {
			return Question{}, ErrInvalidImport
		}
		current.Weight = *input.Weight
	}
	if input.Text != nil {
		value := strings.TrimSpace(*input.Text)
		if value == "" || len([]rune(value)) > 2000 {
			return Question{}, ErrInvalidImport
		}
		if NormalizeText(value) != NormalizeText(current.Text) {
			existing, err := existingQuestionTexts(ctx, tx, versionID)
			if err != nil {
				return Question{}, err
			}
			if existing[NormalizeText(value)] {
				return Question{}, ErrDuplicate
			}
			current.Text = value
		}
	}
	var attributes map[string]any
	_ = json.Unmarshal([]byte(attributesRaw), &attributes)
	if attributes == nil {
		attributes = map[string]any{}
	}
	if isBrandedQuestion(ctx, tx, tenantID, projectIDForQuestion(ctx, tx, versionID), current.Text) {
		attributes["brandType"] = "branded"
	} else {
		attributes["brandType"] = "non_branded"
	}
	encoded, _ := json.Marshal(attributes)
	hash := sha256.Sum256([]byte(NormalizeText(current.Text)))
	_, err = tx.ExecContext(ctx, `UPDATE questions SET text_value=?,text_hash=?,question_type=?,intent=?,topic=?,funnel_stage=?,weight=?,status=?,attributes_json=CAST(? AS JSON) WHERE id=UUID_TO_BIN(?)`,
		current.Text, hash[:], current.QuestionType, nullable(current.Intent), nullable(current.Topic), nullable(current.FunnelStage), current.Weight, status, encoded, questionID)
	if err != nil {
		if strings.Contains(err.Error(), "1062") {
			return Question{}, ErrDuplicate
		}
		return Question{}, fmt.Errorf("update question: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Question{}, err
	}
	return s.get(ctx, tenantID, questionID)
}

func projectIDForQuestion(ctx context.Context, tx *sql.Tx, versionID string) string {
	var projectID string
	_ = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(qs.project_id) FROM question_sets qs JOIN question_set_versions qsv ON qsv.question_set_id=qs.id WHERE qsv.id=UUID_TO_BIN(?)`, versionID).Scan(&projectID)
	return projectID
}

// VersionChange is one question difference between two versions.
type VersionChange struct {
	Kind       string   `json:"kind"`
	Text       string   `json:"text"`
	QuestionID string   `json:"questionId"`
	FromStatus string   `json:"fromStatus,omitempty"`
	ToStatus   string   `json:"toStatus,omitempty"`
	FromWeight *float64 `json:"fromWeight,omitempty"`
	ToWeight   *float64 `json:"toWeight,omitempty"`
}

// VersionDiff compares two versions of one question set by normalized text.
type VersionDiff struct {
	FromVersion int             `json:"fromVersion"`
	ToVersion   int             `json:"toVersion"`
	Added       []VersionChange `json:"added"`
	Removed     []VersionChange `json:"removed"`
	Changed     []VersionChange `json:"changed"`
}

// DiffVersions compares two question-set versions.
func (s *Store) DiffVersions(ctx context.Context, tenantID, questionSetID, fromVersionID, toVersionID string) (VersionDiff, error) {
	var fromVersion, toVersion int
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM question_set_versions WHERE tenant_id=UUID_TO_BIN(?) AND question_set_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, questionSetID, fromVersionID).Scan(&fromVersion); err != nil {
		return VersionDiff{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM question_set_versions WHERE tenant_id=UUID_TO_BIN(?) AND question_set_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, questionSetID, toVersionID).Scan(&toVersion); err != nil {
		return VersionDiff{}, err
	}
	from, err := loadVersionQuestions(ctx, s.db, tenantID, fromVersionID)
	if err != nil {
		return VersionDiff{}, err
	}
	to, err := loadVersionQuestions(ctx, s.db, tenantID, toVersionID)
	if err != nil {
		return VersionDiff{}, err
	}
	diff := VersionDiff{FromVersion: fromVersion, ToVersion: toVersion, Added: []VersionChange{}, Removed: []VersionChange{}, Changed: []VersionChange{}}
	for key, item := range to {
		before, ok := from[key]
		if !ok {
			diff.Added = append(diff.Added, VersionChange{Kind: "added", Text: item.text, QuestionID: item.id, ToStatus: item.status, ToWeight: &item.weight})
			continue
		}
		if before.status != item.status || before.weight != item.weight || before.questionType != item.questionType {
			fromStatus, toStatus := before.status, item.status
			fromWeight, toWeight := before.weight, item.weight
			diff.Changed = append(diff.Changed, VersionChange{Kind: "changed", Text: item.text, QuestionID: item.id, FromStatus: fromStatus, ToStatus: toStatus, FromWeight: &fromWeight, ToWeight: &toWeight})
		}
	}
	for key, item := range from {
		if _, ok := to[key]; !ok {
			diff.Removed = append(diff.Removed, VersionChange{Kind: "removed", Text: item.text, QuestionID: item.id, FromStatus: item.status, FromWeight: &item.weight})
		}
	}
	return diff, nil
}

type versionQuestion struct {
	id           string
	text         string
	status       string
	questionType string
	weight       float64
}

func loadVersionQuestions(ctx context.Context, db *sql.DB, tenantID, versionID string) (map[string]versionQuestion, error) {
	rows, err := db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),text_value,status,question_type,weight FROM questions WHERE tenant_id=UUID_TO_BIN(?) AND version_id=UUID_TO_BIN(?)`, tenantID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := map[string]versionQuestion{}
	for rows.Next() {
		var item versionQuestion
		if err := rows.Scan(&item.id, &item.text, &item.status, &item.questionType, &item.weight); err != nil {
			return nil, err
		}
		items[NormalizeText(item.text)] = item
	}
	return items, rows.Err()
}
