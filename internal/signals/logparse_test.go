package signals

import "testing"

func TestParseAccessLogReadsCombinedLines(t *testing.T) {
	lines := []string{
		`203.0.113.5 - - [15/Nov/2026:10:00:00 +0800] "GET /pricing?utm=x HTTP/1.1" 200 1234 "-" "Mozilla/5.0 (compatible; GPTBot/1.0; +https://openai.com/gptbot)"`,
		`203.0.113.5 - - [15/Nov/2026:10:00:05 +0800] "GET /blog/post HTTP/1.1" 403 0 "-" "Mozilla/5.0 (compatible; ClaudeBot/1.0)"`,
		`203.0.113.5 - - [15/Nov/2026:10:00:06 +0800] "GET /assets/app.js HTTP/1.1" 200 9000 "-" "Mozilla/5.0 (compatible; GPTBot/1.0)"`,
		`broken line without structure`,
	}
	parsed, skipped := ParseAccessLog(lines)
	if len(parsed) != 2 {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed[0].Path != "/pricing" || parsed[0].HTTPStatus != 200 {
		t.Fatalf("first = %#v", parsed[0])
	}
	if parsed[0].OccurredAt.IsZero() {
		t.Fatal("timestamp was not parsed")
	}
	if parsed[1].HTTPStatus != 403 || ClassifyBot(parsed[1].UserAgent) != "ClaudeBot" {
		t.Fatalf("second = %#v", parsed[1])
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2 (asset + malformed)", skipped)
	}
}

func TestParseAccessLogSkipsLinesWithoutUserAgent(t *testing.T) {
	parsed, skipped := ParseAccessLog([]string{`203.0.113.5 - - [15/Nov/2026:10:00:00 +0800] "GET /pricing HTTP/1.1" 200 12`})
	if len(parsed) != 0 || skipped != 1 {
		t.Fatalf("parsed=%#v skipped=%d", parsed, skipped)
	}
}

func TestSummarizeStatuses(t *testing.T) {
	breakdown := SummarizeStatuses([]int{200, 204, 403, 429, 404, 500, 301})
	if breakdown.OK != 2 || breakdown.Forbidden != 2 || breakdown.NotFound != 1 || breakdown.ServerError != 1 || breakdown.Other != 1 {
		t.Fatalf("breakdown = %#v", breakdown)
	}
}

func TestIsAssetPath(t *testing.T) {
	for _, path := range []string{"/a.css", "/a/b.JS", "/img/x.png?v=1", "/font.woff2"} {
		if !IsAssetPath(path) {
			t.Fatalf("%q should be an asset", path)
		}
	}
	for _, path := range []string{"/pricing", "/blog/post.html", "/"} {
		if IsAssetPath(path) {
			t.Fatalf("%q should not be an asset", path)
		}
	}
}
