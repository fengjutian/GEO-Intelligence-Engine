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
