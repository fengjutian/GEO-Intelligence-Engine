package questions

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var ErrProjectNotFound = errors.New("project not found")
var ErrProjectArchived = errors.New("project archived")
var ErrDuplicate = errors.New("duplicate question")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func NormalizeText(value string) string {
	normalized := norm.NFKC.String(strings.TrimSpace(value))
	return strings.ToLower(strings.Join(strings.Fields(normalized), " "))
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
	versionID, err := ensureDraftVersion(ctx, tx, tenantID, projectID)
	if err != nil {
		return Question{}, err
	}
	existing, err := existingQuestionTexts(ctx, tx, versionID)
	if err != nil {
		return Question{}, err
	}
	if existing[NormalizeText(input.Text)] {
		return Question{}, ErrDuplicate
	}
	questionID, err := uuid.NewV7()
	if err != nil {
		return Question{}, err
	}
	hash := sha256.Sum256([]byte(NormalizeText(input.Text)))
	brandType, source := "non_branded", "manual"
	if isBrandedQuestion(ctx, tx, tenantID, projectID, input.Text) {
		brandType = "branded"
	}
	attributes, _ := json.Marshal(map[string]any{"source": source, "brandType": brandType})
	_, err = tx.ExecContext(ctx, `
INSERT INTO questions (id,tenant_id,version_id,text_value,text_hash,language,intent,topic,funnel_stage,question_type,weight,status,attributes_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,?,?,'draft',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		questionID.String(), tenantID, versionID, input.Text, hash[:], input.Language, nullable(input.Intent), nullable(input.Topic), nullable(input.FunnelStage), input.QuestionType, *input.Weight, attributes)
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

func existingQuestionTexts(ctx context.Context, tx *sql.Tx, versionID string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT text_value FROM questions WHERE version_id=UUID_TO_BIN(?)`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, err
		}
		existing[NormalizeText(text)] = true
	}
	return existing, rows.Err()
}

func ensureDraftVersion(ctx context.Context, tx *sql.Tx, tenantID, projectID string) (string, error) {
	var versionID string
	err := tx.QueryRowContext(ctx, `
SELECT BIN_TO_UUID(qsv.id) FROM question_sets qs
JOIN question_set_versions qsv ON qsv.question_set_id=qs.id
WHERE qs.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft'
ORDER BY qsv.version DESC LIMIT 1 FOR UPDATE`, tenantID, projectID).Scan(&versionID)
	if err == nil {
		return versionID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read draft version: %w", err)
	}
	setID, _ := uuid.NewV7()
	versionUUID, _ := uuid.NewV7()
	versionID = versionUUID.String()
	if _, err := tx.ExecContext(ctx, `INSERT INTO question_sets (id,tenant_id,project_id,name,status,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'默认问题集','active',UTC_TIMESTAMP(6))`, setID.String(), tenantID, projectID); err != nil {
		return "", fmt.Errorf("create question set: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO question_set_versions (id,tenant_id,question_set_id,version,content_hash,status,published_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),1,UNHEX(SHA2('',256)),'draft',NULL)`, versionID, tenantID, setID.String()); err != nil {
		return "", fmt.Errorf("create draft version: %w", err)
	}
	return versionID, nil
}

func isBrandedQuestion(ctx context.Context, tx *sql.Tx, tenantID, projectID, text string) bool {
	names := []string{}
	var brandName string
	if err := tx.QueryRowContext(ctx, `SELECT canonical_name FROM brands WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at,id LIMIT 1`, tenantID, projectID).Scan(&brandName); err == nil {
		names = append(names, brandName)
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.alias FROM brand_aliases a JOIN brands b ON b.id=a.brand_id WHERE a.tenant_id=UUID_TO_BIN(?) AND b.project_id=UUID_TO_BIN(?) AND a.enabled=TRUE`, tenantID, projectID)
	if err == nil {
		for rows.Next() {
			var alias string
			if rows.Scan(&alias) == nil {
				names = append(names, alias)
			}
		}
		rows.Close()
	}
	lowered := strings.ToLower(text)
	for _, name := range names {
		if name != "" && strings.Contains(lowered, strings.ToLower(name)) {
			return true
		}
	}
	return false
}

func (s *Store) List(ctx context.Context, tenantID, projectID string, limit int) (Page, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(q.id),q.text_value,q.language,q.question_type,COALESCE(q.intent,''),COALESCE(q.topic,''),COALESCE(q.funnel_stage,''),q.weight,q.status,q.attributes_json,q.created_at
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
		var attributes []byte
		if err = rows.Scan(&q.ID, &q.Text, &q.Language, &q.QuestionType, &q.Intent, &q.Topic, &q.FunnelStage, &q.Weight, &q.Status, &attributes, &q.CreatedAt); err != nil {
			return Page{}, err
		}
		applyAttributes(&q, attributes)
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
	var attributes []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),text_value,language,question_type,COALESCE(intent,''),COALESCE(topic,''),COALESCE(funnel_stage,''),weight,status,attributes_json,created_at FROM questions WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, questionID).Scan(&q.ID, &q.Text, &q.Language, &q.QuestionType, &q.Intent, &q.Topic, &q.FunnelStage, &q.Weight, &q.Status, &attributes, &q.CreatedAt)
	applyAttributes(&q, attributes)
	return q, err
}

func applyAttributes(question *Question, raw []byte) {
	attributes := map[string]any{}
	_ = json.Unmarshal(raw, &attributes)
	if source, ok := attributes["source"].(string); ok {
		question.Source = source
	}
	if brandType, ok := attributes["brandType"].(string); ok {
		question.BrandType = brandType
	}
	if question.Source == "" {
		question.Source = "manual"
	}
	if question.BrandType == "" {
		question.BrandType = "non_branded"
	}
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
