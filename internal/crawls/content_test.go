package crawls

import "testing"

func findingCodes(items []technicalFinding) map[string]bool {
	codes := map[string]bool{}
	for _, item := range items {
		codes[item.code] = true
	}
	return codes
}

func TestContentFindingsStructure(t *testing.T) {
	completion := Completion{
		FinalURL:   "https://example.com/guide",
		TextLength: 800,
		Headings:   []Heading{{Level: 2, Text: "Details"}, {Level: 4, Text: "Deep"}},
		TextSample: "Short answer.",
	}
	codes := findingCodes(contentFindings(completion))
	if !codes["CONTENT_H1_MISSING"] || !codes["CONTENT_HEADING_SKIP"] {
		t.Fatalf("missing structure findings: %+v", codes)
	}
}

func TestContentFindingsMultipleH1AndEvidence(t *testing.T) {
	completion := Completion{
		FinalURL:   "https://example.com/report",
		TextLength: 1200,
		Headings:   []Heading{{Level: 1, Text: "One"}, {Level: 1, Text: "Two"}},
		TextSample: "Market share reached 50% in 2025. More text follows.",
		Links:      []string{"https://example.com/internal"},
	}
	codes := findingCodes(contentFindings(completion))
	if !codes["CONTENT_H1_MULTIPLE"] {
		t.Fatal("multiple H1 must be reported")
	}
	if !codes["CONTENT_NO_EVIDENCE"] {
		t.Fatal("statistics without external links must be reported")
	}
}

func TestContentFindingsGoodPage(t *testing.T) {
	completion := Completion{
		FinalURL:   "https://example.com/",
		TextLength: 800,
		Headings:   []Heading{{Level: 1, Text: "Acme GEO"}, {Level: 2, Text: "Overview"}},
		TextSample: "Acme GEO is a visibility analytics platform for AI search answers and citations.",
		Links:      []string{"https://industry.example.org/report"},
	}
	if findings := contentFindings(completion); len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}
