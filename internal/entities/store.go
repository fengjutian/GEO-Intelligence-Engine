package entities

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"unicode"
)

var ErrNotFound = errors.New("entity was not found")
var ErrInvalid = errors.New("entity input is invalid")

var supportedTypes = map[string]string{
	"organization":        "Organization",
	"corporation":         "Organization",
	"localbusiness":       "Organization",
	"brand":               "Brand",
	"product":             "Product",
	"softwareapplication": "SoftwareApplication",
}

// ParseCandidates extracts supported entities and simple relations from JSON-LD.
func ParseCandidates(items []json.RawMessage) []Candidate {
	candidates := []Candidate{}
	for index, raw := range items {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		walk(value, fmt.Sprintf("$[%d]", index), &candidates)
	}
	return mergeCandidates(candidates)
}

// mergeCandidates collapses duplicate name/type nodes (for example a brand object
// referenced both from @graph and from a parent Organization) while keeping all
// aliases and relations.
func mergeCandidates(candidates []Candidate) []Candidate {
	merged := []Candidate{}
	index := map[string]int{}
	for _, candidate := range candidates {
		key := candidate.Type + "\x00" + normalizeName(candidate.Name)
		if position, ok := index[key]; ok {
			existing := &merged[position]
			for _, alias := range candidate.Aliases {
				if !contains(existing.Aliases, alias) {
					existing.Aliases = append(existing.Aliases, alias)
				}
			}
			for _, relation := range candidate.Relations {
				found := false
				for _, current := range existing.Relations {
					if current.Predicate == relation.Predicate && current.TargetName == relation.TargetName {
						found = true
						break
					}
				}
				if !found {
					existing.Relations = append(existing.Relations, relation)
				}
			}
			continue
		}
		index[key] = len(merged)
		merged = append(merged, candidate)
	}
	return merged
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func walk(value any, locator string, candidates *[]Candidate) {
	switch node := value.(type) {
	case []any:
		for index, child := range node {
			walk(child, fmt.Sprintf("%s[%d]", locator, index), candidates)
		}
	case map[string]any:
		if graph, ok := node["@graph"]; ok {
			walk(graph, locator+".@graph", candidates)
		}
		if candidate, ok := candidateFromNode(node, locator); ok {
			*candidates = append(*candidates, candidate)
		}
		for key, child := range node {
			if key == "@graph" {
				continue
			}
			switch child.(type) {
			case map[string]any, []any:
				walk(child, locator+"."+key, candidates)
			}
		}
	}
}

func candidateFromNode(node map[string]any, locator string) (Candidate, bool) {
	name := stringValue(node["name"])
	if name == "" {
		name = stringValue(node["legalName"])
	}
	entityType, ok := supportedType(node["@type"])
	if !ok || name == "" {
		return Candidate{}, false
	}
	aliases := stringList(node["alternateName"])
	candidate := Candidate{Name: name, Type: entityType, Aliases: aliases, Locator: locator}
	for _, key := range []string{"brand", "manufacturer", "parentOrganization"} {
		targetName, targetType := relationTarget(node[key])
		if targetName == "" {
			continue
		}
		if targetType == "" {
			targetType = "Organization"
			if key == "brand" {
				targetType = "Brand"
			}
		}
		candidate.Relations = append(candidate.Relations, RelationCandidate{Predicate: key, TargetName: targetName, TargetType: targetType})
	}
	return candidate, true
}

func supportedType(value any) (string, bool) {
	for _, rawType := range stringList(value) {
		if mapped, ok := supportedTypes[strings.ToLower(strings.TrimSpace(rawType))]; ok {
			return mapped, true
		}
	}
	return "", false
}

func relationTarget(value any) (string, string) {
	switch node := value.(type) {
	case string:
		return strings.TrimSpace(node), ""
	case map[string]any:
		name := stringValue(node["name"])
		entityType, _ := supportedType(node["@type"])
		return name, entityType
	default:
		return "", ""
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	case []any:
		values := []string{}
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				values = append(values, strings.TrimSpace(text))
			}
		}
		return values
	default:
		return nil
	}
}

func normalizeName(value string) string {
	lowered := strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	space := false
	for _, character := range lowered {
		if unicode.IsSpace(character) {
			space = true
			continue
		}
		if space && builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		space = false
		builder.WriteRune(character)
	}
	return builder.String()
}

// Extract persists structured data items, entities, aliases, evidence and relations.
func Extract(ctx context.Context, tx *sql.Tx, tenantID, projectID, contentID string, items []json.RawMessage) error {
	for _, raw := range items {
		if !json.Valid(raw) {
			continue
		}
		var node any
		_ = json.Unmarshal(raw, &node)
		schemaType := ""
		if object, ok := node.(map[string]any); ok {
			if types := stringList(object["@type"]); len(types) > 0 {
				schemaType = types[0]
			}
		}
		itemID, _ := uuid.NewV7()
		hash := sha256.Sum256(raw)
		if _, err := tx.ExecContext(ctx, `INSERT INTO structured_data_items (id,tenant_id,content_id,schema_type,payload_object_key,validation_status,content_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'parsed',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
			itemID.String(), tenantID, contentID, schemaType, "inline://structured-data/"+hex.EncodeToString(hash[:8]), string(raw)); err != nil {
			return fmt.Errorf("insert structured data: %w", err)
		}
	}
	for _, candidate := range ParseCandidates(items) {
		entityID, err := upsertEntity(ctx, tx, tenantID, projectID, candidate)
		if err != nil {
			return err
		}
		evidenceID, _ := uuid.NewV7()
		if _, err := tx.ExecContext(ctx, `INSERT INTO entity_evidence (id,tenant_id,target_type,target_id,source_type,source_id,quote_text,locator,extractor_version,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'entity',UUID_TO_BIN(?),'page_content',UUID_TO_BIN(?),?,?,'jsonld-v1',UTC_TIMESTAMP(6))`,
			evidenceID.String(), tenantID, entityID, contentID, candidate.Name, candidate.Locator); err != nil {
			return fmt.Errorf("insert entity evidence: %w", err)
		}
		for _, relation := range candidate.Relations {
			target, err := upsertEntity(ctx, tx, tenantID, projectID, Candidate{Name: relation.TargetName, Type: relation.TargetType, Locator: candidate.Locator})
			if err != nil {
				return err
			}
			relationID, _ := uuid.NewV7()
			if _, err := tx.ExecContext(ctx, `INSERT INTO entity_relations (id,tenant_id,subject_id,predicate,object_id,status,confidence,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,UUID_TO_BIN(?),'proposed',1.0,UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE status=status`, relationID.String(), tenantID, entityID, relation.Predicate, target); err != nil {
				return fmt.Errorf("insert entity relation: %w", err)
			}
		}
	}
	return nil
}

func upsertEntity(ctx context.Context, tx *sql.Tx, tenantID, projectID string, candidate Candidate) (string, error) {
	normalized := normalizeName(candidate.Name)
	var entityID string
	err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM entities WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND entity_type=? AND normalized_name=? LIMIT 1`,
		tenantID, projectID, candidate.Type, normalized).Scan(&entityID)
	if errors.Is(err, sql.ErrNoRows) {
		entityID = uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO entities (id,tenant_id,project_id,entity_type,canonical_name,normalized_name,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'proposed',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
			entityID, tenantID, projectID, candidate.Type, candidate.Name, normalized); err != nil {
			return "", fmt.Errorf("insert entity: %w", err)
		}
	} else if err != nil {
		return "", err
	}
	for _, alias := range candidate.Aliases {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entity_aliases WHERE tenant_id=UUID_TO_BIN(?) AND entity_id=UUID_TO_BIN(?) AND alias=?`,
			tenantID, entityID, alias).Scan(&exists); err != nil {
			return "", err
		}
		if exists > 0 {
			continue
		}
		aliasID, _ := uuid.NewV7()
		if _, err := tx.ExecContext(ctx, `INSERT INTO entity_aliases (id,tenant_id,entity_id,alias,confidence,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,1.0,UTC_TIMESTAMP(6))`,
			aliasID.String(), tenantID, entityID, alias); err != nil {
			return "", err
		}
	}
	return entityID, nil
}

// Store reads and confirms extracted entities.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context, tenantID, projectID, entityType, status string) ([]Entity, error) {
	query := `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),entity_type,canonical_name,normalized_name,status,created_at,updated_at
FROM entities WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`
	args := []any{tenantID, projectID}
	if entityType != "" {
		query += " AND entity_type=?"
		args = append(args, entityType)
	}
	if status != "" {
		query += " AND status=?"
		args = append(args, status)
	}
	query += " ORDER BY canonical_name,id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Entity{}
	for rows.Next() {
		var entity Entity
		if err := rows.Scan(&entity.ID, &entity.ProjectID, &entity.EntityType, &entity.CanonicalName, &entity.NormalizedName, &entity.Status, &entity.CreatedAt, &entity.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, entity)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, tenantID, entityID string) (Detail, error) {
	var detail Detail
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),entity_type,canonical_name,normalized_name,status,created_at,updated_at
FROM entities WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, entityID).Scan(&detail.ID, &detail.ProjectID, &detail.EntityType, &detail.CanonicalName, &detail.NormalizedName, &detail.Status, &detail.CreatedAt, &detail.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	aliasRows, err := s.db.QueryContext(ctx, `SELECT alias FROM entity_aliases WHERE tenant_id=UUID_TO_BIN(?) AND entity_id=UUID_TO_BIN(?) ORDER BY alias`, tenantID, entityID)
	if err != nil {
		return Detail{}, err
	}
	detail.Aliases = []string{}
	for aliasRows.Next() {
		var alias string
		if err := aliasRows.Scan(&alias); err != nil {
			aliasRows.Close()
			return Detail{}, err
		}
		detail.Aliases = append(detail.Aliases, alias)
	}
	aliasRows.Close()
	evidenceRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),source_type,BIN_TO_UUID(source_id),COALESCE(quote_text,''),COALESCE(locator,''),extractor_version,created_at
FROM entity_evidence WHERE tenant_id=UUID_TO_BIN(?) AND target_type='entity' AND target_id=UUID_TO_BIN(?) ORDER BY created_at`, tenantID, entityID)
	if err != nil {
		return Detail{}, err
	}
	detail.Evidence = []Evidence{}
	for evidenceRows.Next() {
		var evidence Evidence
		if err := evidenceRows.Scan(&evidence.ID, &evidence.SourceType, &evidence.SourceID, &evidence.Quote, &evidence.Locator, &evidence.ExtractorVersion, &evidence.CreatedAt); err != nil {
			evidenceRows.Close()
			return Detail{}, err
		}
		detail.Evidence = append(detail.Evidence, evidence)
	}
	evidenceRows.Close()
	relationRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(r.id),r.predicate,BIN_TO_UUID(r.subject_id),s.canonical_name,BIN_TO_UUID(r.object_id),o.canonical_name,r.status,COALESCE(r.confidence,0)
FROM entity_relations r JOIN entities s ON s.id=r.subject_id JOIN entities o ON o.id=r.object_id
WHERE r.tenant_id=UUID_TO_BIN(?) AND (r.subject_id=UUID_TO_BIN(?) OR r.object_id=UUID_TO_BIN(?)) ORDER BY r.predicate`, tenantID, entityID, entityID)
	if err != nil {
		return Detail{}, err
	}
	detail.Relations = []Relation{}
	for relationRows.Next() {
		var relation Relation
		if err := relationRows.Scan(&relation.ID, &relation.Predicate, &relation.SubjectID, &relation.SubjectName, &relation.ObjectID, &relation.ObjectName, &relation.Status, &relation.Confidence); err != nil {
			relationRows.Close()
			return Detail{}, err
		}
		detail.Relations = append(detail.Relations, relation)
	}
	relationRows.Close()
	return detail, nil
}

func (s *Store) UpdateStatus(ctx context.Context, tenantID, entityID, status string) (Entity, error) {
	switch status {
	case "proposed", "confirmed", "rejected":
	default:
		return Entity{}, ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE entities SET status=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, status, tenantID, entityID)
	if err != nil {
		return Entity{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return Entity{}, ErrNotFound
	}
	detail, err := s.Get(ctx, tenantID, entityID)
	if err != nil {
		return Entity{}, err
	}
	return detail.Entity, nil
}
