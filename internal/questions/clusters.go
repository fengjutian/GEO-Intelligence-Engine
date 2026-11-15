package questions

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const (
	ClusterAlgorithmV1        = "jaccard-v1"
	ClusterAlgorithmSemantic  = "semantic-v2"
	ClusterAlgorithmEmbedding = "embedding-v1"
	ClusterAlgorithm          = ClusterAlgorithmSemantic
)

// ClusterMember is one question grouped under a representative.
type ClusterMember struct {
	QuestionID     string  `json:"questionId"`
	Text           string  `json:"text"`
	Similarity     float64 `json:"similarity"`
	ManuallyLocked bool    `json:"manuallyLocked"`
}

// Cluster is one semantic question group.
type Cluster struct {
	ID                       string          `json:"id"`
	Name                     string          `json:"name"`
	AlgorithmVersion         string          `json:"algorithmVersion"`
	RepresentativeQuestionID string          `json:"representativeQuestionId"`
	Locked                   bool            `json:"locked"`
	Members                  []ClusterMember `json:"members"`
}

type clusterQuestion struct {
	id   string
	text string
}

// RebuildClusters groups draft questions by deterministic token similarity.
func (s *Store) RebuildClusters(ctx context.Context, tenantID, projectID, algorithm string, vectors map[string][]float64) ([]Cluster, error) {
	algorithm = normalizeAlgorithm(algorithm)
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
	lockedQuestions := map[string]bool{}
	lockedRows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(cm.question_id) FROM cluster_members cm
JOIN query_clusters qc ON qc.id=cm.cluster_id
WHERE qc.tenant_id=UUID_TO_BIN(?) AND qc.question_version_id=UUID_TO_BIN(?) AND qc.locked=TRUE`, tenantID, versionID)
	if err != nil {
		return nil, err
	}
	for lockedRows.Next() {
		var questionID string
		if err := lockedRows.Scan(&questionID); err != nil {
			lockedRows.Close()
			return nil, err
		}
		lockedQuestions[questionID] = true
	}
	lockedRows.Close()
	documents := map[string]map[string]bool{}
	documentFrequency := map[string]int{}
	for _, item := range items {
		if algorithm == ClusterAlgorithmV1 {
			documents[item.id] = shingles(item.text)
		} else {
			documents[item.id] = semanticTokens(item.text)
		}
		for token := range documents[item.id] {
			documentFrequency[token]++
		}
	}
	idf := map[string]float64{}
	for token, frequency := range documentFrequency {
		idf[token] = 1 + math.Log(float64(len(items)+1)/float64(frequency))
	}
	if _, err := tx.ExecContext(ctx, `DELETE cm FROM cluster_members cm JOIN query_clusters qc ON qc.id=cm.cluster_id
WHERE qc.tenant_id=UUID_TO_BIN(?) AND qc.question_version_id=UUID_TO_BIN(?) AND qc.locked=FALSE`, tenantID, versionID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM query_clusters WHERE tenant_id=UUID_TO_BIN(?) AND question_version_id=UUID_TO_BIN(?) AND locked=FALSE`, tenantID, versionID); err != nil {
		return nil, err
	}
	type pendingCluster struct {
		name           string
		representative string
		members        []ClusterMember
	}
	clusters := []pendingCluster{}
	for _, item := range items {
		if lockedQuestions[item.id] {
			continue
		}
		bestIndex, bestScore := -1, 0.0
		itemShingles := documents[item.id]
		for index, cluster := range clusters {
			score := weightedSimilarity(itemShingles, documents[cluster.representative], idf)
			if algorithm == ClusterAlgorithmV1 {
				score = similarity(itemShingles, documents[cluster.representative])
			}
			if algorithm == ClusterAlgorithmEmbedding {
				score = cosineSimilarity(vectors[item.id], vectors[cluster.representative])
			}
			if score > bestScore {
				bestIndex, bestScore = index, score
			}
		}
		threshold := 0.3
		if algorithm == ClusterAlgorithmEmbedding {
			threshold = 0.8
		}
		if bestIndex >= 0 && bestScore >= threshold {
			clusters[bestIndex].members = append(clusters[bestIndex].members, ClusterMember{QuestionID: item.id, Text: item.text, Similarity: bestScore})
			continue
		}
		clusters = append(clusters, pendingCluster{name: item.text, representative: item.id, members: []ClusterMember{{QuestionID: item.id, Text: item.text, Similarity: 1}}})
	}
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
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,UUID_TO_BIN(?),?,FALSE)`, clusterID, tenantID, versionID, name, representative, algorithm); err != nil {
			return nil, err
		}
		for _, member := range cluster.members {
			if _, err := tx.ExecContext(ctx, `INSERT INTO cluster_members (cluster_id,question_id,similarity,manually_locked) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),?,FALSE)`, clusterID, member.QuestionID, member.Similarity); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListClusters(ctx, tenantID, projectID)
}

// DraftQuestionTexts returns id→text for the project's draft version.
func (s *Store) DraftQuestionTexts(ctx context.Context, tenantID, projectID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(q.id),q.text_value
FROM questions q JOIN question_set_versions qsv ON qsv.id=q.version_id JOIN question_sets qs ON qs.id=qsv.question_set_id
WHERE q.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft' ORDER BY q.created_at,q.id`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	texts := map[string]string{}
	for rows.Next() {
		var id, text string
		if err := rows.Scan(&id, &text); err != nil {
			return nil, err
		}
		texts[id] = text
	}
	return texts, rows.Err()
}

// ListClusters returns current clusters for the project's draft version.
func (s *Store) ListClusters(ctx context.Context, tenantID, projectID string) ([]Cluster, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(qc.id),qc.name,qc.algorithm_version,BIN_TO_UUID(qc.representative_question_id),qc.locked
FROM query_clusters qc JOIN question_set_versions qsv ON qsv.id=qc.question_version_id JOIN question_sets qs ON qs.id=qsv.question_set_id
WHERE qc.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qsv.status='draft' ORDER BY qc.name`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clusters := []Cluster{}
	for rows.Next() {
		var cluster Cluster
		if err := rows.Scan(&cluster.ID, &cluster.Name, &cluster.AlgorithmVersion, &cluster.RepresentativeQuestionID, &cluster.Locked); err != nil {
			return nil, err
		}
		cluster.Members = []ClusterMember{}
		memberRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(cm.question_id),q.text_value,COALESCE(cm.similarity,0),cm.manually_locked
FROM cluster_members cm JOIN questions q ON q.id=cm.question_id WHERE cm.cluster_id=UUID_TO_BIN(?) ORDER BY cm.similarity DESC,q.created_at`, cluster.ID)
		if err != nil {
			return nil, err
		}
		for memberRows.Next() {
			var member ClusterMember
			if err := memberRows.Scan(&member.QuestionID, &member.Text, &member.Similarity, &member.ManuallyLocked); err != nil {
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

func normalizeAlgorithm(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ClusterAlgorithmV1:
		return ClusterAlgorithmV1
	case ClusterAlgorithmEmbedding:
		return ClusterAlgorithmEmbedding
	default:
		return ClusterAlgorithmSemantic
	}
}

func cosineSimilarity(left, right []float64) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	dot, leftNorm, rightNorm := 0.0, 0.0, 0.0
	for index := range left {
		dot += left[index] * right[index]
		leftNorm += left[index] * left[index]
		rightNorm += right[index] * right[index]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

var semanticSynonyms = []struct{ from, to string }{
	{"生成式引擎", "ai-search"}, {"生成式搜索", "ai-search"}, {"ai 搜索", "ai-search"}, {"ai搜索", "ai-search"},
	{"ai search", "ai-search"}, {"ai引擎", "ai-search"}, {"ai 引擎", "ai-search"},
	{"ai 回答", "ai-answer"}, {"ai回答", "ai-answer"}, {"ai answer", "ai-answer"},
	{"可见性", "visibility"}, {"可见度", "visibility"}, {"visibility", "visibility"},
	{"推荐率", "recommendation"}, {"recommendation", "recommendation"}, {"recommend", "recommendation"},
	{"引用率", "citation"}, {"citation", "citation"}, {"citations", "citation"},
	{"品牌", "brand"}, {"brand", "brand"},
	{"提升", "improve"}, {"提高", "improve"}, {"improve", "improve"}, {"increase", "improve"},
	{"工具", "tool"}, {"tools", "tool"}, {"tool", "tool"},
	{"优化", "optimize"}, {"optimization", "optimize"}, {"optimize", "optimize"},
}

var questionStopwords = map[string]bool{
	"什么": true, "如何": true, "怎么": true, "哪些": true, "哪个": true, "是否": true,
	"可以": true, "需要": true, "以及": true, "一个": true, "进行": true, "通过": true,
	"我们": true, "你们": true, "他们": true, "的": true, "了": true, "吗": true, "呢": true,
	"what": true, "how": true, "which": true, "is": true, "are": true, "the": true,
	"a": true, "an": true, "to": true, "for": true, "of": true, "and": true, "or": true,
	"do": true, "does": true, "can": true, "should": true, "with": true,
}

// semanticTokens canonicalizes GEO vocabulary and removes question filler.
func semanticTokens(text string) map[string]bool {
	normalized := strings.ToLower(norm.NFKC.String(text))
	for _, synonym := range semanticSynonyms {
		normalized = strings.ReplaceAll(normalized, synonym.from, " "+synonym.to+" ")
	}
	set := map[string]bool{}
	words := strings.FieldsFunc(normalized, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for _, word := range words {
		if questionStopwords[word] {
			continue
		}
		if !containsCJK(word) {
			set["w:"+strings.TrimSuffix(word, "s")] = true
			continue
		}
		runes := []rune(word)
		for index := 0; index < len(runes); index++ {
			if index+1 < len(runes) {
				bigram := string(runes[index : index+2])
				if !questionStopwords[bigram] {
					set["b:"+bigram] = true
				}
			}
		}
		if len(runes) <= 6 {
			set["w:"+word] = true
		}
	}
	return set
}

func containsCJK(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func weightedSimilarity(left, right map[string]bool, idf map[string]float64) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection, union := 0.0, 0.0
	for token := range left {
		weight := idf[token]
		if weight == 0 {
			weight = 1
		}
		union += weight
		if right[token] {
			intersection += weight
		}
	}
	for token := range right {
		if !left[token] {
			weight := idf[token]
			if weight == 0 {
				weight = 1
			}
			union += weight
		}
	}
	if union == 0 {
		return 0
	}
	return intersection / union
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
