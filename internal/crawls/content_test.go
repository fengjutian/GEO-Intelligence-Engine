package crawls

import (
	"testing"
	"time"
)

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
		FinalURL:    "https://example.com/",
		TextLength:  800,
		Headings:    []Heading{{Level: 1, Text: "Acme GEO"}, {Level: 2, Text: "Overview"}},
		TextSample:  "Acme GEO is a visibility analytics platform for AI search answers and citations.",
		Links:       []string{"https://industry.example.org/report"},
		Author:      "Acme Team",
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if findings := contentFindings(completion); len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestContentFindingsFAQAndFreshness(t *testing.T) {
	completion := Completion{
		FinalURL:    "https://example.com/faq",
		TextLength:  600,
		Headings:    []Heading{{Level: 1, Text: "FAQ"}},
		TextSample:  "A sufficiently detailed opening answer for the page.",
		Author:      "Acme Team",
		PublishedAt: "2020-01-01",
		FAQ: []FAQItem{
			{Question: "What is GEO?", Answer: ""},
			{Question: "What is GEO?", Answer: "Answer"},
		},
	}
	codes := findingCodes(contentFindings(completion))
	for _, code := range []string{"CONTENT_FAQ_ANSWER_MISSING", "CONTENT_FAQ_DUPLICATE", "CONTENT_STALE_CONTENT"} {
		if !codes[code] {
			t.Fatalf("missing %s in %+v", code, codes)
		}
	}
}

func TestContentFindingsTermsAndAbsoluteClaims(t *testing.T) {
	completion := Completion{
		FinalURL:    "https://example.com/terms",
		TextLength:  600,
		Headings:    []Heading{{Level: 1, Text: "Terms"}},
		TextSample:  "GEO is the best way to improve AI visibility.",
		Author:      "Acme Team",
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
		Terms:       []TermSignal{{Term: "GEO", Count: 4, Defined: false}},
	}
	codes := findingCodes(contentFindings(completion))
	if !codes["CONTENT_TERM_UNDEFINED"] || !codes["CONTENT_ABSOLUTE_CLAIM"] {
		t.Fatalf("missing term/claim findings: %+v", codes)
	}
}

func TestContentFindingsOpinionAsFact(t *testing.T) {
	completion := Completion{
		FinalURL:    "https://example.com/opinion",
		TextLength:  600,
		Headings:    []Heading{{Level: 1, Text: "Review"}},
		TextSample:  "我们推荐 Acme GEO，这是行业最好的平台。",
		Author:      "Acme Team",
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
	}
	findings := contentFindings(completion)
	codes := findingCodes(findings)
	if !codes["CONTENT_OPINION_AS_FACT"] {
		t.Fatalf("missing opinion-as-fact finding: %+v", codes)
	}
	for _, finding := range findings {
		if finding.code == "CONTENT_OPINION_AS_FACT" && finding.severity != "medium" {
			t.Fatalf("unexpected opinion finding: %+v", finding)
		}
	}
}
