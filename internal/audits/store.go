package audits

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound       = errors.New("audit resource not found")
	ErrConflict       = errors.New("audit request conflicts with existing state")
	ErrInvalidInput   = errors.New("audit input is invalid")
	ErrArchived       = errors.New("project is archived")
	ErrPlanTooLarge   = errors.New("audit plan exceeds the expansion limit")
	ErrBudgetExceeded = errors.New("estimated audit cost exceeds the protocol budget")
)

const MaxExpandedSamples = 5000

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) ListQuestionSets(ctx context.Context, tenantID, projectID string) ([]QuestionSet, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),name,status FROM question_sets WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at,id`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sets := []QuestionSet{}
	for rows.Next() {
		var set QuestionSet
		if err := rows.Scan(&set.ID, &set.Name, &set.Status); err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range sets {
		draftErr := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),version FROM question_set_versions WHERE question_set_id=UUID_TO_BIN(?) AND status='draft' ORDER BY version DESC LIMIT 1`, sets[index].ID).Scan(&sets[index].DraftVersionID, &sets[index].DraftVersion)
		if draftErr != nil && !errors.Is(draftErr, sql.ErrNoRows) {
			return nil, draftErr
		}
		versionRows, versionErr := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(qsv.id),qsv.version,COUNT(q.id),qsv.published_at
FROM question_set_versions qsv LEFT JOIN questions q ON q.version_id=qsv.id
WHERE qsv.question_set_id=UUID_TO_BIN(?) AND qsv.status='published'
GROUP BY qsv.id,qsv.version,qsv.published_at ORDER BY qsv.version DESC`, sets[index].ID)
		if versionErr != nil {
			return nil, versionErr
		}
		sets[index].Versions = []QuestionVersion{}
		for versionRows.Next() {
			var version QuestionVersion
			if err := versionRows.Scan(&version.ID, &version.Version, &version.QuestionCount, &version.PublishedAt); err != nil {
				versionRows.Close()
				return nil, err
			}
			sets[index].Versions = append(sets[index].Versions, version)
		}
		versionRows.Close()
	}
	return sets, nil
}

func (s *Store) CreateEngineConfig(ctx context.Context, tenantID string, input EngineConfigInput) (EngineConfig, error) {
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.Channel = strings.ToLower(strings.TrimSpace(input.Channel))
	if !isProvider(input.Provider) || !isChannel(input.Channel) {
		return EngineConfig{}, ErrInvalidInput
	}
	if strings.TrimSpace(input.CredentialRef) == "" {
		return EngineConfig{}, ErrInvalidInput
	}
	if input.Region == "" {
		input.Region = "global"
	}
	if input.Limits == nil {
		input.Limits = map[string]any{}
	}
	limits, err := json.Marshal(input.Limits)
	if err != nil {
		return EngineConfig{}, ErrInvalidInput
	}
	id, _ := uuid.NewV7()
	_, err = s.db.ExecContext(ctx, `
INSERT INTO engine_configs (id,tenant_id,provider,channel,credential_ref,region,limits_json,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,CAST(? AS JSON),'active',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		id.String(), tenantID, input.Provider, input.Channel, input.CredentialRef, input.Region, limits)
	if err != nil {
		return EngineConfig{}, fmt.Errorf("insert engine config: %w", err)
	}
	return EngineConfig{ID: id.String(), Provider: input.Provider, Channel: input.Channel, Region: input.Region, Status: "active", Limits: input.Limits, CreatedAt: time.Now().UTC()}, nil
}

func (s *Store) ListEngineConfigs(ctx context.Context, tenantID string) ([]EngineConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT BIN_TO_UUID(id),provider,channel,COALESCE(region,''),status,limits_json,created_at
FROM engine_configs WHERE tenant_id=UUID_TO_BIN(?) ORDER BY created_at,id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list engine configs: %w", err)
	}
	defer rows.Close()
	items := []EngineConfig{}
	for rows.Next() {
		var item EngineConfig
		var limits []byte
		if err := rows.Scan(&item.ID, &item.Provider, &item.Channel, &item.Region, &item.Status, &limits, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan engine config: %w", err)
		}
		_ = json.Unmarshal(limits, &item.Limits)
		items = append(items, item)
	}
	return items, rows.Err()
}

func isProvider(value string) bool { return value == "deepseek" || value == "minimax" }

func isChannel(value string) bool {
	return value == "model_api" || value == "search_api" || value == "consumer_app_capture"
}

// PublishQuestionSet freezes the current draft version and opens a new draft for later edits.
func (s *Store) PublishQuestionSet(ctx context.Context, tenantID, questionSetID string) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var projectID, setStatus string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),status FROM question_sets WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, questionSetID).Scan(&projectID, &setStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var versionID string
	var version int
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),version FROM question_set_versions WHERE question_set_id=UUID_TO_BIN(?) AND status='draft' ORDER BY version DESC LIMIT 1 FOR UPDATE`, questionSetID).Scan(&versionID, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(id),text_value,language,question_type,weight,COALESCE(intent,'') FROM questions WHERE version_id=UUID_TO_BIN(?) ORDER BY created_at,id`, versionID)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	count := 0
	for rows.Next() {
		var id, text, language, questionType, intent string
		var weight float64
		if err := rows.Scan(&id, &text, &language, &questionType, &weight, &intent); err != nil {
			rows.Close()
			return nil, err
		}
		fmt.Fprintf(hash, "%s|%s|%s|%s|%.4f|%s\n", id, text, language, questionType, weight, intent)
		count++
	}
	rows.Close()
	if count == 0 {
		return nil, ErrInvalidInput
	}
	_, err = tx.ExecContext(ctx, `UPDATE question_set_versions SET status='published',content_hash=?,published_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, hash.Sum(nil), versionID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE questions SET status='active' WHERE version_id=UUID_TO_BIN(?) AND status='draft'`, versionID)
	if err != nil {
		return nil, err
	}
	nextID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO question_set_versions (id,tenant_id,question_set_id,version,content_hash,status,published_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,UNHEX(SHA2('',256)),'draft',NULL)`, nextID.String(), tenantID, questionSetID, version+1)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"id": versionID, "questionSetId": questionSetID, "version": version, "status": "published", "questionCount": count}, nil
}

// createKnowledgeSnapshot freezes the brand profile so audits never read mutable configuration.
func createKnowledgeSnapshot(ctx context.Context, tx *sql.Tx, tenantID, projectID string) (string, int, error) {
	snapshot := map[string]any{}
	var brandName, domain, description string
	err := tx.QueryRowContext(ctx, `SELECT canonical_name,official_domain,COALESCE(description,'') FROM brands WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at,id LIMIT 1`, tenantID, projectID).Scan(&brandName, &domain, &description)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", 0, err
	}
	snapshot["brand"] = map[string]any{"canonicalName": brandName, "officialDomain": domain, "description": description}
	aliases := []map[string]any{}
	rows, err := tx.QueryContext(ctx, `SELECT a.alias,a.alias_type,COALESCE(a.locale,'') FROM brand_aliases a JOIN brands b ON b.id=a.brand_id WHERE a.tenant_id=UUID_TO_BIN(?) AND b.project_id=UUID_TO_BIN(?) AND a.enabled=TRUE ORDER BY a.alias`, tenantID, projectID)
	if err != nil {
		return "", 0, err
	}
	for rows.Next() {
		var alias, aliasType, locale string
		if err := rows.Scan(&alias, &aliasType, &locale); err != nil {
			rows.Close()
			return "", 0, err
		}
		aliases = append(aliases, map[string]any{"alias": alias, "type": aliasType, "locale": locale})
	}
	rows.Close()
	snapshot["aliases"] = aliases
	products := []map[string]any{}
	rows, err = tx.QueryContext(ctx, `SELECT p.name,COALESCE(p.description,''),p.verification_status FROM products p JOIN brands b ON b.id=p.brand_id WHERE p.tenant_id=UUID_TO_BIN(?) AND b.project_id=UUID_TO_BIN(?) ORDER BY p.name`, tenantID, projectID)
	if err != nil {
		return "", 0, err
	}
	for rows.Next() {
		var name, productDescription, status string
		if err := rows.Scan(&name, &productDescription, &status); err != nil {
			rows.Close()
			return "", 0, err
		}
		products = append(products, map[string]any{"name": name, "description": productDescription, "verificationStatus": status})
	}
	rows.Close()
	snapshot["products"] = products
	competitors := []map[string]any{}
	rows, err = tx.QueryContext(ctx, `SELECT name,COALESCE(domain,''),status FROM competitors WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY name`, tenantID, projectID)
	if err != nil {
		return "", 0, err
	}
	for rows.Next() {
		var name, competitorDomain, status string
		if err := rows.Scan(&name, &competitorDomain, &status); err != nil {
			rows.Close()
			return "", 0, err
		}
		competitors = append(competitors, map[string]any{"name": name, "domain": competitorDomain, "status": status})
	}
	rows.Close()
	snapshot["competitors"] = competitors
	content, err := json.Marshal(snapshot)
	if err != nil {
		return "", 0, err
	}
	hash := sha256.Sum256(content)
	var nextVersion int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM knowledge_snapshots WHERE project_id=UUID_TO_BIN(?)`, projectID).Scan(&nextVersion); err != nil {
		return "", 0, err
	}
	id, _ := uuid.NewV7()
	objectKey := fmt.Sprintf("db://knowledge/%s/%d", projectID, nextVersion)
	_, err = tx.ExecContext(ctx, `INSERT INTO knowledge_snapshots (id,tenant_id,project_id,version,content_hash,snapshot_object_key,published_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,UTC_TIMESTAMP(6))`, id.String(), tenantID, projectID, nextVersion, hash[:], objectKey)
	if err != nil {
		return "", 0, err
	}
	return id.String(), nextVersion, nil
}

func hashRequest(parts ...string) []byte {
	hash := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hash[:]
}

func hexHash(value []byte) string { return hex.EncodeToString(value) }
