package crawls

import "testing"

func TestContentFindingsFlagsJavaScriptOnlyPages(t *testing.T) {
	completion := Completion{
		FinalURL:         "https://www.feishu.cn/",
		TextLength:       1100,
		StaticTextLength: 22,
		Rendered:         true,
		Headings:         []Heading{{Level: 1, Text: "飞书"}},
		TextSample:       "飞书是字节跳动旗下的 AI 工作平台。",
	}
	findings := contentFindings(completion)
	codes := findingCodes(findings)
	if !codes["CONTENT_JS_ONLY"] {
		t.Fatalf("expected CONTENT_JS_ONLY, got %+v", codes)
	}
	if codes["CONTENT_THIN"] {
		t.Fatalf("rendered page with rich text must not be thin: %+v", codes)
	}
	for _, finding := range findings {
		if finding.code != "CONTENT_JS_ONLY" {
			continue
		}
		if finding.evidence["staticTextLength"] != 22 || finding.evidence["renderedTextLength"] != 1100 {
			t.Fatalf("unexpected evidence: %+v", finding.evidence)
		}
	}
}

func TestContentFindingsIgnoresRenderedFlagWhenPageWasAlreadyRich(t *testing.T) {
	completion := Completion{
		FinalURL:         "https://example.com/rich",
		TextLength:       1200,
		StaticTextLength: 900,
		Rendered:         false,
		Headings:         []Heading{{Level: 1, Text: "Guide"}},
		TextSample:       "A long enough answer.",
	}
	if codes := findingCodes(contentFindings(completion)); codes["CONTENT_JS_ONLY"] {
		t.Fatalf("static page must not be flagged as JS only: %+v", codes)
	}
}

func TestFindingVersionCoversJavaScriptOnly(t *testing.T) {
	if got := findingVersion("CONTENT_JS_ONLY"); got != "content-rules-v1" {
		t.Fatalf("findingVersion(CONTENT_JS_ONLY) = %q", got)
	}
}
