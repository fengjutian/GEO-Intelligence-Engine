package questions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrDraftNotEmpty = errors.New("current draft version is not empty")

// CloneResult describes a draft cloned from a published version.
type CloneResult struct {
	QuestionSetID string `json:"questionSetId"`
	VersionID     string `json:"versionId"`
	Version       int    `json:"version"`
	QuestionCount int    `json:"questionCount"`
}

// CloneVersion copies a published version into the current empty draft.
func (s *Store) CloneVersion(ctx context.Context, tenantID, questionSetID, sourceVersionID string) (CloneResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CloneResult{}, err
	}
	defer tx.Rollback()
	var sourceVersion int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM question_set_versions
WHERE tenant_id=UUID_TO_BIN(?) AND question_set_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND status='published'`,
		tenantID, questionSetID, sourceVersionID).Scan(&sourceVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CloneResult{}, ErrQuestionNotFound
		}
		return CloneResult{}, err
	}
	var draftID string
	var draftVersion int
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),version FROM question_set_versions
WHERE tenant_id=UUID_TO_BIN(?) AND question_set_id=UUID_TO_BIN(?) AND status='draft' ORDER BY version DESC LIMIT 1 FOR UPDATE`,
		tenantID, questionSetID).Scan(&draftID, &draftVersion)
	if errors.Is(err, sql.ErrNoRows) {
		draftVersion = sourceVersion + 1
		draftID = uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO question_set_versions (id,tenant_id,question_set_id,version,content_hash,status,published_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,UNHEX(SHA2('',256)),'draft',NULL)`, draftID, tenantID, questionSetID, draftVersion); err != nil {
			return CloneResult{}, err
		}
	} else if err != nil {
		return CloneResult{}, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM questions WHERE version_id=UUID_TO_BIN(?)`, draftID).Scan(&count); err != nil {
		return CloneResult{}, err
	}
	if count > 0 {
		return CloneResult{}, ErrDraftNotEmpty
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO questions
(id,tenant_id,version_id,text_value,text_hash,language,topic,intent,funnel_stage,question_type,weight,status,attributes_json,created_at)
SELECT UUID_TO_BIN(UUID()),tenant_id,UUID_TO_BIN(?),text_value,text_hash,language,topic,intent,funnel_stage,question_type,weight,status,attributes_json,UTC_TIMESTAMP(6)
FROM questions WHERE tenant_id=UUID_TO_BIN(?) AND version_id=UUID_TO_BIN(?)`, draftID, tenantID, sourceVersionID)
	if err != nil {
		return CloneResult{}, fmt.Errorf("clone questions: %w", err)
	}
	affected, _ := result.RowsAffected()
	if err := tx.Commit(); err != nil {
		return CloneResult{}, err
	}
	return CloneResult{QuestionSetID: questionSetID, VersionID: draftID, Version: draftVersion, QuestionCount: int(affected)}, nil
}

// CompetitorAliases returns enabled alias terms grouped by competitor id.
func (s *Store) CompetitorAliases(ctx context.Context, tenantID, projectID string) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(a.competitor_id),a.alias
FROM competitor_aliases a JOIN competitors c ON c.id=a.competitor_id
WHERE a.tenant_id=UUID_TO_BIN(?) AND c.project_id=UUID_TO_BIN(?) AND a.enabled=TRUE`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]string{}
	for rows.Next() {
		var competitorID, alias string
		if err := rows.Scan(&competitorID, &alias); err != nil {
			return nil, err
		}
		alias = strings.TrimSpace(alias)
		if alias != "" {
			result[competitorID] = append(result[competitorID], alias)
		}
	}
	return result, rows.Err()
}
