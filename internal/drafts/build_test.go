package drafts

import (
	"strings"
	"testing"
)

func TestBuildJSONLDIncludesOrganizationAndProduct(t *testing.T) {
	payload := BuildJSONLD(Brand{Name: "飞书", OfficialURL: "https://www.feishu.cn", Description: "AI 工作平台"})
	if payload["@context"] != "https://schema.org" {
		t.Fatalf("context = %v", payload["@context"])
	}
	graph, ok := payload["@graph"].([]any)
	if !ok || len(graph) != 2 {
		t.Fatalf("graph = %#v", payload["@graph"])
	}
	organization, _ := graph[0].(map[string]any)
	if organization["@type"] != "Organization" || organization["name"] != "飞书" {
		t.Fatalf("organization = %#v", organization)
	}
	product, _ := graph[1].(map[string]any)
	if product["@type"] != "Product" {
		t.Fatalf("product = %#v", product)
	}
}

func TestBuildLLMsTxtDeduplicatesAndSorts(t *testing.T) {
	content := BuildLLMsTxt(Brand{Name: "Acme", Description: "GEO platform"}, []string{
		"https://acme.test/b", "https://acme.test/a", "https://acme.test/b", "",
	})
	for _, expected := range []string{"# Acme", "> GEO platform", "- https://acme.test/a", "- https://acme.test/b"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("missing %q in:\n%s", expected, content)
		}
	}
	if strings.Count(content, "https://acme.test/b") != 1 {
		t.Fatalf("duplicate URL kept:\n%s", content)
	}
}

func TestBuildMetaFallsBackToBrand(t *testing.T) {
	payload := BuildMeta(Brand{Name: "Acme", OfficialURL: "https://acme.test"}, "https://acme.test/pricing", "")
	if payload["canonical"] != "https://acme.test/pricing" || payload["title"] != "Acme" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["description"] == "" {
		t.Fatal("description fallback missing")
	}
}

func TestDiffReportsChangedFields(t *testing.T) {
	changes := Diff(
		map[string]any{"canonical": "/old", "title": "same"},
		map[string]any{"canonical": "/new", "title": "same", "extra": 1},
	)
	if len(changes) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	if changes[0].Field != "canonical" || changes[0].After != "/new" {
		t.Fatalf("first change = %#v", changes[0])
	}
}

func TestAllowedTransition(t *testing.T) {
	allowed := [][2]string{{"draft", "approved"}, {"draft", "discarded"}, {"approved", "published"}, {"approved", "discarded"}}
	for _, pair := range allowed {
		if !allowedTransition(pair[0], pair[1]) {
			t.Fatalf("%s -> %s should be allowed", pair[0], pair[1])
		}
	}
	rejected := [][2]string{{"draft", "published"}, {"published", "draft"}, {"discarded", "approved"}}
	for _, pair := range rejected {
		if allowedTransition(pair[0], pair[1]) {
			t.Fatalf("%s -> %s should be rejected", pair[0], pair[1])
		}
	}
}
