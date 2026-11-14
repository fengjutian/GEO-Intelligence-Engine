package entities

import (
	"encoding/json"
	"testing"
)

func TestParseCandidatesHandlesGraphAndRelations(t *testing.T) {
	raw := json.RawMessage(`{
	  "@context":"https://schema.org",
	  "@graph":[
	    {"@type":"Organization","name":"Acme GEO","alternateName":["Acme","Acme Inc"],"brand":{"@type":"Brand","name":"Acme Insight"}},
	    {"@type":["Product"],"name":"Acme Insight Pro","manufacturer":{"@type":"Organization","name":"Acme GEO"}}
	  ]
	}`)
	candidates := ParseCandidates([]json.RawMessage{raw})
	unique := map[string]Candidate{}
	for _, candidate := range candidates {
		unique[candidate.Name] = candidate
	}
	if len(unique) != 3 {
		t.Fatalf("expected 3 unique candidates, got %d (%+v)", len(unique), candidates)
	}
	if unique["Acme GEO"].Type != "Organization" {
		t.Fatalf("unexpected organization: %+v", unique["Acme GEO"])
	}
	if len(unique["Acme GEO"].Aliases) != 2 || unique["Acme GEO"].Relations[0].Predicate != "brand" {
		t.Fatalf("missing aliases/relations: %+v", unique["Acme GEO"])
	}
	if unique["Acme Insight Pro"].Relations[0].TargetType != "Organization" {
		t.Fatalf("unexpected product: %+v", unique["Acme Insight Pro"])
	}
}

func TestParseCandidatesSkipsUnsupportedAndMalformed(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"@type":"WebPage","name":"Ignore me"}`),
		json.RawMessage(`{"@type":"Organization"}`),
		json.RawMessage(`{oops`),
	}
	if candidates := ParseCandidates(items); len(candidates) != 0 {
		t.Fatalf("expected no candidates, got %+v", candidates)
	}
}

func TestNormalizeNameCollapsesWhitespaceAndCase(t *testing.T) {
	if got := normalizeName("  Acme   GEO  "); got != "acme geo" {
		t.Fatalf("normalizeName = %q", got)
	}
}
