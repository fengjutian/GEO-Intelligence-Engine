package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// KnowledgeSnapshot is one immutable published view of a project's knowledge.
type KnowledgeSnapshot struct {
	ID          string    `json:"id"`
	Version     int       `json:"version"`
	ContentHash string    `json:"contentHash"`
	PublishedAt time.Time `json:"publishedAt"`
}

// SnapshotDiff is the structural difference between two published snapshots.
type SnapshotDiff struct {
	From      KnowledgeSnapshot `json:"from"`
	To        KnowledgeSnapshot `json:"to"`
	Entities  SnapshotChange    `json:"entities"`
	Relations SnapshotChange    `json:"relations"`
	Summary   map[string]int    `json:"summary"`
}

// SnapshotChange groups added, removed and changed members of one snapshot part.
type SnapshotChange struct {
	Added   []map[string]any `json:"added"`
	Removed []map[string]any `json:"removed"`
	Changed []map[string]any `json:"changed,omitempty"`
}

type snapshotContent struct {
	Entities  []snapshotEntity   `json:"entities"`
	Relations []snapshotRelation `json:"relations"`
}

type snapshotEntity struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	CanonicalName string   `json:"canonicalName"`
	Status        string   `json:"status"`
	Aliases       []string `json:"aliases"`
}

type snapshotRelation struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
	Status    string `json:"status"`
}

// ListKnowledgeSnapshots returns published snapshots newest first.
func (s *Store) ListKnowledgeSnapshots(ctx context.Context, tenantID, projectID string) ([]KnowledgeSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),version,HEX(content_hash),published_at
FROM knowledge_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY version DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	snapshots := []KnowledgeSnapshot{}
	for rows.Next() {
		var item KnowledgeSnapshot
		if err := rows.Scan(&item.ID, &item.Version, &item.ContentHash, &item.PublishedAt); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, item)
	}
	return snapshots, rows.Err()
}

// CompareKnowledgeSnapshots diffs two published snapshots of the same project.
// It never merges or rewrites either side; differences are reported verbatim.
func (s *Store) CompareKnowledgeSnapshots(ctx context.Context, tenantID, projectID, fromID, toID string) (SnapshotDiff, error) {
	from, fromContent, err := s.loadSnapshot(ctx, tenantID, projectID, fromID)
	if err != nil {
		return SnapshotDiff{}, err
	}
	to, toContent, err := s.loadSnapshot(ctx, tenantID, projectID, toID)
	if err != nil {
		return SnapshotDiff{}, err
	}
	diff := SnapshotDiff{From: from, To: to}
	diff.Entities, diff.Relations = diffSnapshots(fromContent, toContent)
	diff.Summary = map[string]int{
		"entitiesAdded":    len(diff.Entities.Added),
		"entitiesRemoved":  len(diff.Entities.Removed),
		"entitiesChanged":  len(diff.Entities.Changed),
		"relationsAdded":   len(diff.Relations.Added),
		"relationsRemoved": len(diff.Relations.Removed),
	}
	return diff, nil
}

func (s *Store) loadSnapshot(ctx context.Context, tenantID, projectID, snapshotID string) (KnowledgeSnapshot, snapshotContent, error) {
	var meta KnowledgeSnapshot
	var content []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),version,HEX(content_hash),published_at,content_json
FROM knowledge_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`,
		tenantID, projectID, snapshotID).Scan(&meta.ID, &meta.Version, &meta.ContentHash, &meta.PublishedAt, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return KnowledgeSnapshot{}, snapshotContent{}, ErrNotFound
	}
	if err != nil {
		return KnowledgeSnapshot{}, snapshotContent{}, err
	}
	parsed := snapshotContent{}
	if len(content) > 0 {
		if err := json.Unmarshal(content, &parsed); err != nil {
			return KnowledgeSnapshot{}, snapshotContent{}, err
		}
	}
	return meta, parsed, nil
}

func diffSnapshots(from, to snapshotContent) (SnapshotChange, SnapshotChange) {
	entities := SnapshotChange{Added: []map[string]any{}, Removed: []map[string]any{}, Changed: []map[string]any{}}
	fromEntities := indexEntities(from.Entities)
	toEntities := indexEntities(to.Entities)
	for key, entity := range toEntities {
		previous, exists := fromEntities[key]
		if !exists {
			entities.Added = append(entities.Added, entityView(entity))
			continue
		}
		if change, ok := entityChange(previous, entity); ok {
			entities.Changed = append(entities.Changed, change)
		}
	}
	for key, entity := range fromEntities {
		if _, exists := toEntities[key]; !exists {
			entities.Removed = append(entities.Removed, entityView(entity))
		}
	}
	relations := SnapshotChange{Added: []map[string]any{}, Removed: []map[string]any{}}
	fromRelations := indexRelations(from.Relations)
	toRelations := indexRelations(to.Relations)
	for key, relation := range toRelations {
		if _, exists := fromRelations[key]; !exists {
			relations.Added = append(relations.Added, relationView(relation))
		}
	}
	for key, relation := range fromRelations {
		if _, exists := toRelations[key]; !exists {
			relations.Removed = append(relations.Removed, relationView(relation))
		}
	}
	sortChanges(&entities)
	sortChanges(&relations)
	return entities, relations
}

func indexEntities(items []snapshotEntity) map[string]snapshotEntity {
	indexed := make(map[string]snapshotEntity, len(items))
	for _, item := range items {
		indexed[strings.ToLower(item.Type)+"|"+strings.ToLower(item.CanonicalName)] = item
	}
	return indexed
}

func indexRelations(items []snapshotRelation) map[string]snapshotRelation {
	indexed := make(map[string]snapshotRelation, len(items))
	for _, item := range items {
		indexed[strings.ToLower(item.Subject)+"|"+strings.ToLower(item.Predicate)+"|"+strings.ToLower(item.Object)] = item
	}
	return indexed
}

func entityView(entity snapshotEntity) map[string]any {
	return map[string]any{
		"id":            entity.ID,
		"type":          entity.Type,
		"canonicalName": entity.CanonicalName,
		"status":        entity.Status,
		"aliases":       entity.Aliases,
	}
}

func relationView(relation snapshotRelation) map[string]any {
	return map[string]any{
		"subject":   relation.Subject,
		"predicate": relation.Predicate,
		"object":    relation.Object,
		"status":    relation.Status,
	}
}

func entityChange(previous, current snapshotEntity) (map[string]any, bool) {
	fields := map[string]any{}
	if previous.Status != current.Status {
		fields["status"] = map[string]any{"from": previous.Status, "to": current.Status}
	}
	if !sameStrings(previous.Aliases, current.Aliases) {
		fields["aliases"] = map[string]any{"from": previous.Aliases, "to": current.Aliases}
	}
	if len(fields) == 0 {
		return nil, false
	}
	return map[string]any{
		"type":          current.Type,
		"canonicalName": current.CanonicalName,
		"fields":        fields,
	}, true
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sortChanges(change *SnapshotChange) {
	sort.SliceStable(change.Added, func(i, j int) bool { return changeKey(change.Added[i]) < changeKey(change.Added[j]) })
	sort.SliceStable(change.Removed, func(i, j int) bool { return changeKey(change.Removed[i]) < changeKey(change.Removed[j]) })
	sort.SliceStable(change.Changed, func(i, j int) bool { return changeKey(change.Changed[i]) < changeKey(change.Changed[j]) })
}

func changeKey(value map[string]any) string {
	if name, ok := value["canonicalName"].(string); ok {
		return name
	}
	if subject, ok := value["subject"].(string); ok {
		return subject
	}
	return ""
}
