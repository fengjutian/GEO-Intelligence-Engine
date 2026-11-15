package signals

import (
	"regexp"
	"strings"
	"time"
)

// ParsedLogLine is one usable access-log entry.
type ParsedLogLine struct {
	Path       string
	HTTPStatus int
	UserAgent  string
	OccurredAt time.Time
}

var accessLogPattern = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "(\S+) ([^"]*?) (?:\S+)" (\d{3}) (\S+)(?: "([^"]*)" "([^"]*)")?`)

var assetExtensions = []string{".css", ".js", ".mjs", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".map", ".webp", ".avif"}

// IsAssetPath reports whether a request targets a static asset rather than a page.
func IsAssetPath(path string) bool {
	lower := strings.ToLower(strings.SplitN(path, "?", 2)[0])
	for _, extension := range assetExtensions {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return false
}

// ParseAccessLog parses Apache/Nginx combined log lines, skipping unusable ones.
func ParseAccessLog(lines []string) ([]ParsedLogLine, int) {
	parsed := []ParsedLogLine{}
	skipped := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		matches := accessLogPattern.FindStringSubmatch(line)
		if len(matches) < 8 {
			skipped++
			continue
		}
		userAgent := strings.TrimSpace(matches[8])
		path := strings.TrimSpace(strings.SplitN(matches[4], "?", 2)[0])
		if userAgent == "" || path == "" || IsAssetPath(path) {
			skipped++
			continue
		}
		occurred, err := time.Parse("02/Jan/2006:15:04:05 -0700", matches[2])
		if err != nil {
			occurred = time.Time{}
		}
		status := 0
		for _, value := range matches[5] {
			if value < '0' || value > '9' {
				status = 0
				break
			}
			status = status*10 + int(value-'0')
		}
		parsed = append(parsed, ParsedLogLine{Path: path, HTTPStatus: status, UserAgent: userAgent, OccurredAt: occurred})
	}
	return parsed, skipped
}

// StatusBreakdown counts log entries per HTTP status class.
type StatusBreakdown struct {
	OK          int `json:"ok"`
	Forbidden   int `json:"forbidden"`
	NotFound    int `json:"notFound"`
	ServerError int `json:"serverError"`
	Other       int `json:"other"`
}

// SummarizeStatuses groups statuses into the four classes operators care about.
func SummarizeStatuses(statuses []int) StatusBreakdown {
	breakdown := StatusBreakdown{}
	for _, status := range statuses {
		switch {
		case status >= 200 && status < 300:
			breakdown.OK++
		case status == 403 || status == 401 || status == 429:
			breakdown.Forbidden++
		case status == 404 || status == 410:
			breakdown.NotFound++
		case status >= 500:
			breakdown.ServerError++
		default:
			breakdown.Other++
		}
	}
	return breakdown
}
