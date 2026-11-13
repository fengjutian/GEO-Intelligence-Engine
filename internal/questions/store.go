package questions

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrProjectNotFound = errors.New("project not found")
var ErrProjectArchived = errors.New("project archived")
var ErrDuplicate = errors.New("duplicate question")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func NormalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func (s *Store) Create(ctx context.Context, tenantID, projectID string, input CreateInput) (Question, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Question{}, fmt.Errorf("begin question transaction: %w", err)
	}
	defer tx.Rollback()

	var projectStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM projects WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, projectID).Scan(&projectStatus); errors.Is(err, sql.ErrNoRows) {
		return Question{}, ErrProjectNotFound
	} else if err != nil {
		return Question{}, fmt.Errorf("read project: %w", err)
	}
	if projectStatus == "archived" {
		return Question{}, ErrProjectArchived
	}

	var versionID string
	err = tx.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(qsv.id) FROM question_sets qs
JOIN question_set_versions qsv ON qsv.question_set_id=qs.id
WHERE qs.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft'
ORDER BY qsv.version DESC LIMIT 1 FOR UPDATE`, tenantID, projectID).Scan(&versionID)
	if errors.Is(err, sql.ErrNoRows) {
		setID, idErr := uuid.NewV7()
		if idErr != nil {
			return Question{}, idErr
		}
		versionUUID, idErr := uuid.NewV7()
		if idErr != nil {
			return Question{}, idErr
		}
		versionID = versionUUID.String()
		if _, err = tx.ExecContext(ctx, `INSERT INTO question_sets (id,tenant_id,project_id,name,status,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'默认问题集','active',UTC_TIMESTAMP(6))`, setID.String(), tenantID, projectID); err != nil {
			return Question{}, fmt.Errorf("create question set: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO question_set_versions (id,tenant_id,question_set_id,version,content_hash,status,published_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),1,UNHEX(SHA2('',256)),'draft',NULL)`, versionID, tenantID, setID.String()); err != nil {
			return Question{}, fmt.Errorf("create draft version: %w", err)
		}
	} else if err != nil {
		return Question{}, fmt.Errorf("read draft version: %w", err)
	}

	questionID, err := uuid.NewV7()
	if err != nil {
		return Question{}, err
	}
	hash := sha256.Sum256([]byte(NormalizeText(input.Text)))
	_, err = tx.ExecContext(ctx, `
INSERT INTO questions (id,tenant_id,version_id,text_value,text_hash,language,intent,question_type,weight,status,attributes_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,'draft',JSON_OBJECT('source','manual'),UTC_TIMESTAMP(6))`,
		questionID.String(), tenantID, versionID, input.Text, hash[:], input.Language, nullable(input.Intent), input.QuestionType, *input.Weight)
	if err != nil {
		if strings.Contains(err.Error(), "1062") {
			return Question{}, ErrDuplicate
		}
		return Question{}, fmt.Errorf("insert question: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Question{}, fmt.Errorf("commit question: %w", err)
	}
	return s.get(ctx, tenantID, questionID.String())
}

func (s *Store) List(ctx context.Context, tenantID, projectID string, limit int) (Page, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(q.id),q.text_value,q.language,q.question_type,COALESCE(q.intent,''),q.weight,q.status,q.created_at
FROM questions q JOIN question_set_versions qsv ON qsv.id=q.version_id JOIN question_sets qs ON qs.id=qsv.question_set_id
WHERE q.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft'
ORDER BY q.created_at DESC,q.id DESC LIMIT ?`, tenantID, projectID, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("list questions: %w", err)
	}
	defer rows.Close()
	items := make([]Question, 0, limit)
	for rows.Next() {
		var q Question
		if err = rows.Scan(&q.ID, &q.Text, &q.Language, &q.QuestionType, &q.Intent, &q.Weight, &q.Status, &q.CreatedAt); err != nil {
			return Page{}, err
		}
		items = append(items, q)
	}
	if err = rows.Err(); err != nil {
		return Page{}, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return Page{Items: items, Page: PageMeta{HasMore: hasMore}}, nil
}

func (s *Store) get(ctx context.Context, tenantID, questionID string) (Question, error) {
	var q Question
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),text_value,language,question_type,COALESCE(intent,''),weight,status,created_at FROM questions WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, questionID).Scan(&q.ID, &q.Text, &q.Language, &q.QuestionType, &q.Intent, &q.Weight, &q.Status, &q.CreatedAt)
	return q, err
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
