package audits

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestCSVCellNeutralizesFormulaInjection(t *testing.T) {
	for _, value := range []string{"=cmd()", "+1+1", "-2+3", "@SUM(A1)"} {
		if got := csvCell(value); !strings.HasPrefix(got, "'") {
			t.Fatalf("csvCell(%q) = %q, want a leading apostrophe", value, got)
		}
	}
	if got := csvCell("normal"); got != "normal" {
		t.Fatalf("csvCell(normal) = %q", got)
	}
}

func TestBuildCSVIncludesBOMAndMetricRows(t *testing.T) {
	report := Report{
		ID:              "report-1",
		Version:         2,
		Status:          "ready",
		TemplateVersion: "report-v1",
		CreatedAt:       time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC),
		Sections: []ReportSection{
			{Code: "summary", Content: map[string]any{"eligibleCount": 1}},
			{Code: "metrics", Content: map[string]any{"values": []any{
				map[string]any{"metricCode": "mention_rate", "dimensions": map[string]any{"channel": "model_api"}, "numerator": 1.0, "denominator": 1.0, "value": 1.0, "ciLow": 0.2, "ciHigh": 1.0, "intervalMethod": "wilson"},
			}}},
			{Code: "limitations", Content: map[string]any{"items": []any{"=malicious"}}},
		},
	}
	content := buildCSV(report)
	if !bytes.HasPrefix(content, []byte("\uFEFF")) {
		t.Fatal("CSV must start with a UTF-8 BOM")
	}
	text := string(content)
	for _, expected := range []string{"mention_rate", "model_api", "'=malicious", "report-1"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("CSV missing %q:\n%s", expected, text)
		}
	}
}

func TestBuildGeoInsightsReadsNullableMetricValues(t *testing.T) {
	share := func(name, entityType string, value float64) map[string]any {
		typed := value
		return map[string]any{
			"metricCode": "share_of_voice",
			"dimensions": map[string]any{"entity": name, "entityType": entityType},
			"value":      &typed,
		}
	}
	shares := []map[string]any{
		share("飞书", "brand", 1.0),
		share("钉钉", "competitor", 0.75),
		share("Notion", "competitor", 0.25),
	}
	geoSummary, competitive, _ := buildGeoInsights(nil, shares, nil, map[string]any{"total": 0, "bySeverity": map[string]int{}})
	if got := geoSummary["brandShare"]; got != 1.0 {
		t.Fatalf("brandShare = %v, want 1.0", got)
	}
	if got := geoSummary["competitorShare"]; got != 0.0 {
		t.Fatalf("competitorShare = %v, want 0.0", got)
	}
	leader, ok := geoSummary["leader"].(map[string]any)
	if !ok || leader["name"] != "飞书" {
		t.Fatalf("leader = %v, want 飞书", geoSummary["leader"])
	}
	entities, _ := competitive["entities"].([]map[string]any)
	if len(entities) != 3 || entities[0]["name"] != "飞书" || entities[1]["name"] != "钉钉" {
		t.Fatalf("entities are not sorted by share: %v", entities)
	}
	if got := competitive["brandShare"]; got != 1.0 {
		t.Fatalf("competitive brandShare = %v, want 1.0", got)
	}
}

func TestBuildGeoInsightsTreatsMissingValueAsZero(t *testing.T) {
	shares := []map[string]any{{
		"metricCode": "share_of_voice",
		"dimensions": map[string]any{"entity": "Acme", "entityType": "brand"},
		"value":      (*float64)(nil),
	}}
	geoSummary, _, _ := buildGeoInsights(nil, shares, nil, map[string]any{})
	if got := geoSummary["brandShare"]; got != 0.0 {
		t.Fatalf("brandShare = %v, want 0.0", got)
	}
}

func TestCitationAvailabilityDistinguishesMissingCitations(t *testing.T) {
	coverage, availability := citationAvailability(AuditCitations{Total: 0})
	if coverage != nil {
		t.Fatalf("coverage = %v, want nil when the answers carry no URLs", coverage)
	}
	if availability != "no_citations_in_answers" {
		t.Fatalf("availability = %q", availability)
	}

	coverage, availability = citationAvailability(AuditCitations{Total: 4, BrandDomainCount: 1, BrandDomainCoverage: 0.25})
	if coverage != 0.25 || availability != "available" {
		t.Fatalf("coverage = %v availability = %q", coverage, availability)
	}
}
