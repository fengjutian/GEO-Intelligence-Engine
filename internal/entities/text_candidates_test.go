package entities

import "testing"

func TestParseTextCandidatesFindsEnglishDefinition(t *testing.T) {
	candidates := ParseTextCandidates("Acme GEO is a visibility analytics platform for AI search answers. Try it today.", 5)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %+v", len(candidates), candidates)
	}
	if candidates[0].Name != "Acme GEO" {
		t.Fatalf("name = %q", candidates[0].Name)
	}
	if candidates[0].Type != "SoftwareApplication" {
		t.Fatalf("type = %q", candidates[0].Type)
	}
	if candidates[0].Quote == "" || candidates[0].Locator != "text:def" {
		t.Fatalf("missing evidence: %+v", candidates[0])
	}
}

func TestParseTextCandidatesFindsOrganizationAndProduct(t *testing.T) {
	candidates := ParseTextCandidates("Northwind Traders is a global company. Contoso Cloud is a hosted service. Nothing else here.", 5)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(candidates), candidates)
	}
	if candidates[0].Type != "Organization" || candidates[1].Type != "Product" {
		t.Fatalf("unexpected types: %+v", candidates)
	}
}

func TestParseTextCandidatesRejectsStopwordsAndNonDefinitions(t *testing.T) {
	cases := []string{
		"This is a great tool for teams.",
		"It is a platform that helps.",
		"We recommend Acme for GEO work.",
		"Shipped in 2025.",
	}
	for _, input := range cases {
		if candidates := ParseTextCandidates(input, 5); len(candidates) != 0 {
			t.Fatalf("input %q produced %+v", input, candidates)
		}
	}
}

func TestParseTextCandidatesHonoursLimitAndDeduplicates(t *testing.T) {
	text := "Alpha Systems is a software company. Beta Systems is a software company. Alpha Systems is a software company again."
	candidates := ParseTextCandidates(text, 1)
	if len(candidates) != 1 {
		t.Fatalf("limit ignored: %+v", candidates)
	}
	all := ParseTextCandidates(text, 5)
	if len(all) != 2 {
		t.Fatalf("deduplication failed: %+v", all)
	}
}

func TestFindWordRespectsBoundaries(t *testing.T) {
	if index := findWord("acmecorp is here", "acme"); index != -1 {
		t.Fatalf("expected no match inside acmecorp, got %d", index)
	}
	if index := findWord("use acme geo now", "acme"); index != 4 {
		t.Fatalf("expected match at 4, got %d", index)
	}
}

func TestEntityTypeFromDescriptor(t *testing.T) {
	cases := map[string]string{
		"visibility analytics platform": "SoftwareApplication",
		"software company":              "SoftwareApplication",
		"global company":                "Organization",
		"hosted service":                "Product",
	}
	for descriptor, expected := range cases {
		got, ok := entityTypeFromDescriptor(descriptor)
		if !ok || got != expected {
			t.Fatalf("descriptor %q => %q/%v, want %q", descriptor, got, ok, expected)
		}
	}
	if _, ok := entityTypeFromDescriptor("purple elephant"); ok {
		t.Fatal("unexpected type for unrelated descriptor")
	}
}
