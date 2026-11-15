package signals

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// discoveryClient fetches watchlist pages with a hard timeout.
var discoveryClient = &http.Client{
	Timeout: 12 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var (
	titlePattern  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	scriptPattern = regexp.MustCompile(`(?is)<(script|style|noscript|template)[^>]*>.*?</(script|style|noscript|template)>`)
	tagPattern    = regexp.MustCompile(`(?s)<[^>]+>`)
	spacePattern  = regexp.MustCompile(`\s+`)
)

// NormalizeForDedupe reduces a URL to host+path so tracking parameters and
// trailing slashes do not create duplicate mentions.
func NormalizeForDedupe(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	host := strings.ToLower(parsed.Host)
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	return host + path
}

// DedupeKey hashes the normalized URL and title.
func DedupeKey(rawURL, title string) []byte {
	sum := sha256.Sum256([]byte(NormalizeForDedupe(rawURL) + "|" + strings.ToLower(strings.TrimSpace(title))))
	return sum[:]
}

// DedupeHashHex is the storage-friendly form of DedupeKey.
func DedupeHashHex(rawURL, title string) string {
	return hex.EncodeToString(DedupeKey(rawURL, title))
}

var authoritySuffixes = []struct {
	suffix string
	score  float64
}{
	{"wikipedia.org", 0.95},
	{"g2.com", 0.85}, {"capterra.com", 0.85}, {"trustradius.com", 0.85},
	{"techcrunch.com", 0.75}, {"theverge.com", 0.75}, {"wired.com", 0.75},
	{"36kr.com", 0.75}, {"sspai.com", 0.75}, {"ifanr.com", 0.75}, {"huxiu.com", 0.7},
	{"zhihu.com", 0.55}, {"reddit.com", 0.55}, {"v2ex.com", 0.55}, {"stackoverflow.com", 0.6},
	{"medium.com", 0.45}, {"substack.com", 0.45}, {"blogspot.com", 0.35},
}

// AuthorityScore rates a source domain from 0 to 1. Self-published brand pages
// score lower so they cannot dominate the mention graph.
func AuthorityScore(host, brandHost string) float64 {
	clean := strings.ToLower(strings.TrimSpace(host))
	if clean == "" {
		return 0.3
	}
	if brandHost != "" && (clean == brandHost || strings.HasSuffix(clean, "."+brandHost)) {
		return 0.4
	}
	for _, label := range strings.Split(clean, ".") {
		if label == "gov" || label == "edu" {
			return 0.9
		}
	}
	for _, entry := range authoritySuffixes {
		if clean == entry.suffix || strings.HasSuffix(clean, "."+entry.suffix) {
			return entry.score
		}
	}
	return 0.5
}

// ExtractMention pulls the page title and a snippet around the first brand hit.
func ExtractMention(html, brand string, aliases []string) (string, string, bool) {
	title := ""
	if matches := titlePattern.FindStringSubmatch(html); len(matches) == 2 {
		title = strings.TrimSpace(spacePattern.ReplaceAllString(matches[1], " "))
	}
	text := scriptPattern.ReplaceAllString(html, " ")
	text = tagPattern.ReplaceAllString(text, " ")
	text = strings.TrimSpace(spacePattern.ReplaceAllString(text, " "))
	names := append([]string{brand}, aliases...)
	lower := strings.ToLower(text)
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		index := strings.Index(lower, strings.ToLower(trimmed))
		if index < 0 {
			continue
		}
		return title, truncate(strings.TrimSpace(safeSlice(text, index-160, index+len(trimmed)+160)), 400), true
	}
	return title, "", false
}

// safeSlice trims byte offsets back to rune boundaries so a mention snippet is valid UTF-8.
func safeSlice(text string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	for start > 0 && start < len(text) && !utf8.RuneStart(text[start]) {
		start--
	}
	for end > 0 && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	if end <= start {
		return ""
	}
	return text[start:end]
}

// isFetchableURL blocks internal targets before the discovery fetch runs.
func isFetchableURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "169.254.") {
		return false
	}
	if strings.HasPrefix(host, "172.") {
		parts := strings.Split(host, ".")
		if len(parts) > 1 && parts[1] >= "16" && parts[1] <= "31" {
			return false
		}
	}
	return true
}

// DiscoverResult reports what a watchlist scan did.
type DiscoverResult struct {
	Fetched    int      `json:"fetched"`
	Discovered int      `json:"discovered"`
	Duplicates int      `json:"duplicates"`
	Skipped    int      `json:"skipped"`
	Errors     []string `json:"errors"`
}

func fetchPage(rawURL string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "geo-intelligence-engine/1.0 (source discovery)")
	response, err := discoveryClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
