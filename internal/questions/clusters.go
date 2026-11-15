package questions

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const ClusterAlgorithm = "jaccard-v1"

// ClusterMember is one question grouped under a representative.
type ClusterMember struct {
	QuestionID string  `json:"questionId"`
	Text       string  `json:"text"`
	Similarity float64 `json:"similarity"`
}

// Cluster is one semantic question group.
type Cluster struct {
	ID                       string          `json:"id"`
	Name                     string          `json:"name"`
	AlgorithmVersion         string          `json:"algorithmVersion"`
	RepresentativeQuestionID string          `json:"representativeQuestionId"`
	Members                  []ClusterMember `json:"members"`
}

type clusterQuestion struct {
	id   string
	text string
}

// RebuildClusters groups draft questions by deterministic token similarity.
func (s *Store) RebuildClusters(ctx context.Context, tenantID, projectID string) ([]Cluster, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var versionID string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(qsv.id) FROM question_sets qs JOIN question_set_versions qsv ON qsv.question_set_id=qs.id
WHERE qs.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft' ORDER BY qsv.version DESC LIMIT 1 FOR UPDATE`, tenantID, projectID).Scan(&versionID)
	if errors.Is(err, sql.ErrNoRows) {
		return []Cluster{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(id),text_value FROM questions WHERE tenant_id=UUID_TO_BIN(?) AND version_id=UUID_TO_BIN(?) ORDER BY created_at,id`, tenantID, versionID)
	if err != nil {
		return nil, err
	}
	items := []clusterQuestion{}
	for rows.Next() {
		var item clusterQuestion
		if err := rows.Scan(&item.id, &item.text); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM cluster_members WHERE cluster_id IN (SELECT id FROM query_clusters WHERE tenant_id=UUID_TO_BIN(?) AND question_version_id=UUID_TO_BIN(?))`, tenantID, versionID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM query_clusters WHERE tenant_id=UUID_TO_BIN(?) AND question_version_id=UUID_TO_BIN(?)`, tenantID, versionID); err != nil {
		return nil, err
	}
	type pendingCluster struct {
		name    string
		members []ClusterMember
	}
	clusters := []pendingCluster{}
	for _, item := range items {
		bestIndex, bestScore := -1, 0.0
		itemShingles := shingles(item.text)
		for index, cluster := range clusters {
			score := similarity(itemShingles, shingles(cluster.name))
			if score > bestScore {
				bestIndex, bestScore = index, score
			}
		}
		if bestIndex >= 0 && bestScore >= 0.3 {
			clusters[bestIndex].members = append(clusters[bestIndex].members, ClusterMember{QuestionID: item.id, Text: item.text, Similarity: bestScore})
			continue
		}
		clusters = append(clusters, pendingCluster{name: item.text, members: []ClusterMember{{QuestionID: item.id, Text: item.text, Similarity: 1}}})
	}
	result := []Cluster{}
	for _, cluster := range clusters {
		clusterID := uuid.NewString()
		representative := ""
		for _, member := range cluster.members {
			if representative == "" || len([]rune(member.Text)) > len([]rune(representative)) {
				representative = member.QuestionID
			}
		}
		name := cluster.name
		if len([]rune(name)) > 120 {
			name = string([]rune(name)[:120])
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO query_clusters (id,tenant_id,question_version_id,name,representative_question_id,algorithm_version,locked)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,UUID_TO_BIN(?),?,FALSE)`, clusterID, tenantID, versionID, name, representative, ClusterAlgorithm); err != nil {
			return nil, err
		}
		for _, member := range cluster.members {
			if _, err := tx.ExecContext(ctx, `INSERT INTO cluster_members (cluster_id,question_id,similarity,manually_locked) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),?,FALSE)`, clusterID, member.QuestionID, member.Similarity); err != nil {
				return nil, err
			}
		}
		result = append(result, Cluster{ID: clusterID, Name: name, AlgorithmVersion: ClusterAlgorithm, RepresentativeQuestionID: representative, Members: cluster.members})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// ListClusters returns current clusters for the project's draft version.
func (s *Store) ListClusters(ctx context.Context, tenantID, projectID string) ([]Cluster, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(qc.id),qc.name,qc.algorithm_version,BIN_TO_UUID(qc.representative_question_id)
FROM query_clusters qc JOIN question_set_versions qsv ON qsv.id=qc.question_version_id JOIN question_sets qs ON qs.id=qsv.question_set_id
WHERE qc.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft' ORDER BY qc.name`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clusters := []Cluster{}
	for rows.Next() {
		var cluster Cluster
		if err := rows.Scan(&cluster.ID, &cluster.Name, &cluster.AlgorithmVersion, &cluster.RepresentativeQuestionID); err != nil {
			return nil, err
		}
		cluster.Members = []ClusterMember{}
		memberRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(cm.question_id),q.text_value,COALESCE(cm.similarity,0)
FROM cluster_members cm JOIN questions q ON q.id=cm.question_id WHERE cm.cluster_id=UUID_TO_BIN(?) ORDER BY cm.similarity DESC,q.created_at`, cluster.ID)
		if err != nil {
			return nil, err
		}
		for memberRows.Next() {
			var member ClusterMember
			if err := memberRows.Scan(&member.QuestionID, &member.Text, &member.Similarity); err != nil {
				memberRows.Close()
				return nil, err
			}
			cluster.Members = append(cluster.Members, member)
		}
		memberRows.Close()
		clusters = append(clusters, cluster)
	}
	return clusters, rows.Err()
}

func shingles(text string) map[string]bool {
	normalized := strings.ToLower(text)
	words := strings.FieldsFunc(normalized, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	set := map[string]bool{}
	for _, word := range words {
		set["w:"+word] = true
	}
	runes := []rune(strings.Join(words, " "))
	for index := 0; index+1 < len(runes); index++ {
		set["b:"+string(runes[index:index+2])] = true
	}
	return set
}

func similarity(left, right map[string]bool) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	for token := range left {
		if right[token] {
			intersection++
		}
	}
	union := len(left) + len(right) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
