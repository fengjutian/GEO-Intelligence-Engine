package signals

import "testing"

func TestClassifyBot(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 AppleWebKit/537.36 (compatible; GPTBot/1.0; +https://openai.com/gptbot)":             "GPTBot",
		"Mozilla/5.0 (compatible; OAI-SearchBot/1.0; +https://openai.com/searchbot)":                      "OAI-SearchBot",
		"Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)":                               "ClaudeBot",
		"Mozilla/5.0 (compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)":               "PerplexityBot",
		"Mozilla/5.0 (compatible; Google-Extended; +http://www.google.com/bot.html)":                      "Google-Extended",
		"Mozilla/5.0 (compatible; Bytespider; spider-feedback@bytedance.com)":                             "Bytespider",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0 Safari/537.36":                             "unknown",
	}
	for userAgent, expected := range cases {
		if got := ClassifyBot(userAgent); got != expected {
			t.Fatalf("ClassifyBot(%q) = %q, want %q", userAgent, got, expected)
		}
	}
}

func TestIsAIBot(t *testing.T) {
	for _, name := range []string{"GPTBot", "ClaudeBot", "PerplexityBot", "Google-Extended", "Bytespider"} {
		if !IsAIBot(name) {
			t.Fatalf("%s should be an AI crawler", name)
		}
	}
	for _, name := range []string{"Googlebot", "Bingbot", "unknown"} {
		if IsAIBot(name) {
			t.Fatalf("%s should not be an AI crawler", name)
		}
	}
}

func TestNormalizeSourceAndSentiment(t *testing.T) {
	if got := normalizeSourceType(" REVIEW "); got != "review" {
		t.Fatalf("source type = %q", got)
	}
	if got := normalizeSourceType("blog"); got != "other" {
		t.Fatalf("unknown source type = %q", got)
	}
	if got := normalizeSentiment("Positive"); got != "positive" {
		t.Fatalf("sentiment = %q", got)
	}
	if got := normalizeSentiment("mixed"); got != "neutral" {
		t.Fatalf("unknown sentiment = %q", got)
	}
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	value := "飞书是一个协作平台"
	got := truncate(value, 3)
	if got != "飞书是" {
		t.Fatalf("truncate = %q", got)
	}
}
