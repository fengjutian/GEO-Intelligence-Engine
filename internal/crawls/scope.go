package crawls

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Default crawl limits keep a single run bounded when the caller only supplies a website.
const (
	DefaultMaxPages    = 20
	DefaultMaxDepth    = 2
	DefaultConcurrency = 1
	MaxConcurrency     = 8
	MaxSupportedDepth  = 20
)

// NormalizeLimits fills in defaults and clamps the crawl scope to supported bounds.
func NormalizeLimits(input CreateInput) CreateInput {
	if input.MaxPages <= 0 {
		input.MaxPages = DefaultMaxPages
	}
	if input.MaxPages > 10000 {
		input.MaxPages = 10000
	}
	if input.MaxDepth < 0 {
		input.MaxDepth = 0
	}
	if input.MaxDepth > MaxSupportedDepth {
		input.MaxDepth = MaxSupportedDepth
	}
	if input.Concurrency <= 0 {
		input.Concurrency = DefaultConcurrency
	}
	if input.Concurrency > MaxConcurrency {
		input.Concurrency = MaxConcurrency
	}
	return input
}

// NormalizeCrawlURL canonicalizes a discovered URL for deduplication: lowercase scheme and
// host, default ports removed, fragments and userinfo dropped, empty path promoted to "/".
// Query strings are preserved because business parameters can change the page identity.
func NormalizeCrawlURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("empty URL")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return "", errors.New("missing host")
	}
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	}
	parsed.Scheme = scheme
	parsed.Host = host
	parsed.User = nil
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

// SameSite reports whether candidate belongs to the same website as base. The comparison
// ignores a leading "www." so that example.com and www.example.com share one crawl scope.
func SameSite(candidate, base string) bool {
	candidateHost, ok := urlHost(candidate)
	if !ok {
		return false
	}
	baseHost, ok := urlHost(base)
	if !ok {
		return false
	}
	return strings.TrimPrefix(candidateHost, "www.") == strings.TrimPrefix(baseHost, "www.")
}

func urlHost(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	return host, host != ""
}

// PlanDiscovery filters discovered links down to the links this run may enqueue: absolute
// http(s) URLs on the website host, deeper than the current page but within maxDepth, unique
// within the page and capped by the remaining page budget.
func PlanDiscovery(baseURL, pageURL string, links []string, depth, maxDepth, remaining int) []string {
	if remaining <= 0 || depth >= maxDepth {
		return nil
	}
	seen := make(map[string]struct{}, len(links))
	planned := make([]string, 0, minInt(remaining, len(links)))
	for _, raw := range links {
		if len(planned) >= remaining {
			break
		}
		normalized, err := NormalizeCrawlURL(raw)
		if err != nil {
			continue
		}
		if !SameSite(normalized, baseURL) {
			continue
		}
		if pageURL != "" {
			if pageNormalized, err := NormalizeCrawlURL(pageURL); err == nil && normalized == pageNormalized {
				continue
			}
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		planned = append(planned, normalized)
	}
	return planned
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
