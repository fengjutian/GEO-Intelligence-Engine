package signals

import (
	"strings"
	"time"
)

// BotLogEvent is one crawler hit imported from a web server log.
type BotLogEvent struct {
	ID        string    `json:"id"`
	BotName   string    `json:"botName"`
	Path      string    `json:"path"`
	Status    int       `json:"httpStatus"`
	OccurredAt time.Time `json:"occurredAt"`
}

// BotLogInput is the caller-supplied shape of one log line.
type BotLogInput struct {
	UserAgent  string    `json:"userAgent"`
	Path       string    `json:"path"`
	HTTPStatus int       `json:"httpStatus"`
	OccurredAt time.Time `json:"occurredAt"`
}

// BotStat aggregates one bot over a window.
type BotStat struct {
	BotName     string    `json:"botName"`
	Hits        int       `json:"hits"`
	Pages       int       `json:"pages"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	IsAIBot     bool      `json:"isAIBot"`
}

// aiBots is the set of user-agent markers that matter for GEO.
var aiBots = []struct {
	marker string
	name   string
}{
	{"gptbot", "GPTBot"},
	{"oai-searchbot", "OAI-SearchBot"},
	{"chatgpt-user", "ChatGPT-User"},
	{"claudebot", "ClaudeBot"},
	{"anthropic-ai", "Anthropic"},
	{"perplexitybot", "PerplexityBot"},
	{"perplexity-user", "Perplexity-User"},
	{"google-extended", "Google-Extended"},
	{"bingbot", "Bingbot"},
	{"bytespider", "Bytespider"},
	{"yisouspider", "YisouSpider"},
	{"baiduspider", "Baiduspider"},
	{"ccbot", "CCBot"},
	{"applebot", "Applebot"},
	{"amazonbot", "Amazonbot"},
	{"facebookbot", "FacebookBot"},
	{"googlebot", "Googlebot"},
}

// aiBotNames marks which classified bots represent AI answer surfaces.
var aiBotNames = map[string]bool{
	"GPTBot": true, "OAI-SearchBot": true, "ChatGPT-User": true,
	"ClaudeBot": true, "Anthropic": true, "PerplexityBot": true, "Perplexity-User": true,
	"Google-Extended": true, "Bytespider": true, "YisouSpider": true, "CCBot": true,
}

// ClassifyBot maps a user agent to a stable bot name.
func ClassifyBot(userAgent string) string {
	lower := strings.ToLower(userAgent)
	for _, bot := range aiBots {
		if strings.Contains(lower, bot.marker) {
			return bot.name
		}
	}
	return "unknown"
}

// IsAIBot reports whether a classified bot belongs to an AI answer surface.
func IsAIBot(name string) bool { return aiBotNames[name] }

// Mention is one third-party source that mentions the brand.
type Mention struct {
	ID          string     `json:"id"`
	SourceURL   string     `json:"sourceUrl"`
	Title       string     `json:"title"`
	Snippet     string     `json:"snippet"`
	SourceType  string     `json:"sourceType"`
	Sentiment   string     `json:"sentiment"`
	PublishedAt *time.Time `json:"publishedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// MentionInput is the caller-supplied shape of one external mention.
type MentionInput struct {
	SourceURL   string     `json:"sourceUrl"`
	Title       string     `json:"title"`
	Snippet     string     `json:"snippet"`
	SourceType  string     `json:"sourceType"`
	Sentiment   string     `json:"sentiment"`
	PublishedAt *time.Time `json:"publishedAt"`
}

// SourceTypeCount is the per-channel share of the mention graph.
type SourceTypeCount struct {
	SourceType string `json:"sourceType"`
	Count      int    `json:"count"`
	Positive   int    `json:"positive"`
	Negative   int    `json:"negative"`
}
