package signals

import (
	"strings"
	"testing"
)

func TestNormalizeForDedupeDropsQueryAndTrailingSlash(t *testing.T) {
	left := NormalizeForDedupe("https://Example.COM/pricing/?utm_source=x#top")
	right := NormalizeForDedupe("https://example.com/pricing")
	if left != right {
		t.Fatalf("%q != %q", left, right)
	}
}

func TestDedupeKeyDiffersByTitle(t *testing.T) {
	a := DedupeHashHex("https://example.com/a", "Title A")
	b := DedupeHashHex("https://example.com/a", "Title B")
	c := DedupeHashHex("https://example.com/a?utm=1", "Title A")
	if a == b {
		t.Fatal("different titles must hash differently")
	}
	if a != c {
		t.Fatal("tracking parameters must not change the hash")
	}
}

func TestAuthorityScoreTiers(t *testing.T) {
	cases := map[string]float64{
		"zh.wikipedia.org": 0.95,
		"www.g2.com":       0.85,
		"techcrunch.com":   0.75,
		"www.zhihu.com":    0.55,
		"medium.com":       0.45,
		"unknown.example":  0.5,
		"agency.gov.cn":    0.9,
	}
	for host, expected := range cases {
		if got := AuthorityScore(host, ""); got != expected {
			t.Fatalf("AuthorityScore(%q) = %v, want %v", host, got, expected)
		}
	}
}

func TestAuthorityScoreMarksBrandOwnDomain(t *testing.T) {
	if got := AuthorityScore("www.feishu.cn", "feishu.cn"); got != 0.4 {
		t.Fatalf("brand domain score = %v", got)
	}
}

func TestExtractMentionFindsBrandAndSnippet(t *testing.T) {
	html := `<html><head><title>飞书评测</title></head><body><script>var x=1</script><p>我们认为飞书在文档协作上表现突出</p></body></html>`
	title, snippet, found := ExtractMention(html, "飞书", nil)
	if !found {
		t.Fatal("expected a mention")
	}
	if title != "飞书评测" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(snippet, "飞书") {
		t.Fatalf("snippet = %q", snippet)
	}
	if strings.Contains(snippet, "var x=1") {
		t.Fatal("script content leaked into the snippet")
	}
}

func TestExtractMentionUsesAliasesAndSkipsMissing(t *testing.T) {
	if _, _, found := ExtractMention("<p>Acme GEO is great</p>", "飞书", []string{"Acme GEO"}); !found {
		t.Fatal("alias should be matched")
	}
	if _, _, found := ExtractMention("<p>nothing relevant</p>", "飞书", nil); found {
		t.Fatal("unrelated page must not match")
	}
}

func TestSafeSliceKeepsValidUTF8(t *testing.T) {
	text := "飞书是一个协作平台"
	got := safeSlice(text, 1, 4)
	if !strings.Contains("飞书是一个协作平台", got) {
		t.Fatalf("slice = %q", got)
	}
	for _, value := range got {
		if value == '\uFFFD' {
			t.Fatalf("invalid rune in %q", got)
		}
	}
}

func TestStatusAndCoverageHelpers(t *testing.T) {
	if got := SummarizeStatuses([]int{200, 403}); got.OK != 1 || got.Forbidden != 1 {
		t.Fatalf("status = %#v", got)
	}
	if URLPath("https://example.com/a/b?x=1") != "/a/b" {
		t.Fatalf("path = %q", URLPath("https://example.com/a/b?x=1"))
	}
	if URLPath("https://example.com") != "/" {
		t.Fatal("root path mismatch")
	}
}
