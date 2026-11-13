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
