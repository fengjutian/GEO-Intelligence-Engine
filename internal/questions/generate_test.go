package questions

import "testing"

func TestGenerateCandidatesUsesSnapshot(t *testing.T) {
	var frozen snapshot
	frozen.Brand.CanonicalName = "Acme GEO"
	frozen.Products = append(frozen.Products, struct {
		Name string `json:"name"`
	}{Name: "Acme Insight"})
	frozen.Competitors = append(frozen.Competitors, struct {
		Name string `json:"name"`
	}{Name: "Globex"})
	frozen.Entities = append(frozen.Entities, struct {
		CanonicalName string `json:"canonicalName"`
		Type          string `json:"type"`
	}{CanonicalName: "Acme GEO", Type: "Organization"})
	items := generateCandidates(frozen, "zh-CN")
	if len(items) < 4 {
		t.Fatalf("expected generated questions, got %+v", items)
	}
	branded := 0
	for _, item := range items {
		if item.branded {
			branded++
		}
		if item.kind == "" || item.basis == "" {
			t.Fatalf("missing metadata: %+v", item)
		}
	}
	if branded == 0 {
		t.Fatal("expected branded questions")
	}
}

func TestSimilarityGroupsRelatedQuestions(t *testing.T) {
	left := shingles("Acme GEO 是什么？")
	right := shingles("Acme GEO 靠谱吗？")
	other := shingles("如何选择 CRM 系统？")
	if similarity(left, right) <= similarity(left, other) {
		t.Fatalf("related questions should be closer: related=%.3f other=%.3f", similarity(left, right), similarity(left, other))
	}
}

func TestSemanticTokensCanonicalizeSynonyms(t *testing.T) {
	left := semanticTokens("AI 搜索可见度如何提升？")
	right := semanticTokens("生成式引擎可见性怎么提高？")
	idf := map[string]float64{}
	for token := range left {
		idf[token] = 1
	}
	for token := range right {
		idf[token] = 1
	}
	if score := weightedSimilarity(left, right, idf); score < 0.8 {
		t.Fatalf("expected high semantic similarity, got %.3f (%v vs %v)", score, left, right)
	}
	if normalizeAlgorithm("jaccard-v1") != ClusterAlgorithmV1 {
		t.Fatal("jaccard-v1 must be preserved")
	}
	if normalizeAlgorithm("") != ClusterAlgorithmSemantic {
		t.Fatal("default algorithm must be semantic-v2")
	}
}
