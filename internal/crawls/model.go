package crawls

import (
	"encoding/json"
	"time"
)

type CreateInput struct {
	WebsiteID   string   `json:"websiteId"`
	StartURLs   []string `json:"startUrls"`
	MaxPages    int      `json:"maxPages"`
	MaxDepth    int      `json:"maxDepth"`
	Concurrency int      `json:"concurrency"`
}

type Run struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"projectId"`
	WebsiteID       string     `json:"websiteId"`
	Status          string     `json:"status"`
	DiscoveredCount uint32     `json:"-"`
	FetchedCount    uint32     `json:"-"`
	FailedCount     uint32     `json:"-"`
	Counts          Counts     `json:"counts"`
	StartedAt       *time.Time `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type Counts struct {
	Discovered uint32 `json:"discovered"`
	Fetched    uint32 `json:"fetched"`
	Failed     uint32 `json:"failed"`
	Blocked    uint32 `json:"blocked"`
}

type Claim struct {
	RunID      string `json:"runId"`
	CrawlURLID string `json:"crawlUrlId"`
	WebsiteID  string `json:"websiteId"`
	URL        string `json:"url"`
	Depth      int    `json:"depth"`
	MaxBytes   int64  `json:"maxBytes"`
}

type Completion struct {
	RunID         string   `json:"runId"`
	CrawlURLID    string   `json:"crawlUrlId"`
	FinalURL      string   `json:"finalUrl"`
	HTTPStatus    int      `json:"httpStatus"`
	Title         string   `json:"title"`
	Language      *string  `json:"language"`
	ContentHash   string   `json:"contentHash"`
	ParserVersion string   `json:"parserVersion"`
	CanonicalURL  *string  `json:"canonicalUrl"`
	Links         []string `json:"links"`
	NoIndex       bool     `json:"noIndex"`
	TextLength    int      `json:"textLength"`
}

type Failure struct {
	RunID      string `json:"runId"`
	CrawlURLID string `json:"crawlUrlId"`
	ErrorCode  string `json:"errorCode"`
}

type PageSummary struct {
	PageID        string    `json:"pageId"`
	ContentID     string    `json:"contentId"`
	CanonicalURL  *string   `json:"canonicalUrl"`
	FinalURL      string    `json:"finalUrl"`
	HTTPStatus    uint16    `json:"httpStatus"`
	Title         string    `json:"title"`
	Language      *string   `json:"language"`
	ContentHash   string    `json:"contentHash"`
	ParserVersion string    `json:"parserVersion"`
	CreatedAt     time.Time `json:"createdAt"`
}

type PageDetail struct {
	PageSummary
	Links []string `json:"links"`
}

type Finding struct {
	ID        string         `json:"id"`
	RuleCode  string         `json:"ruleCode"`
	Severity  string         `json:"severity"`
	Status    string         `json:"status"`
	Summary   string         `json:"summary"`
	Evidence  map[string]any `json:"evidence"`
	CreatedAt time.Time      `json:"createdAt"`
}

// RobotsSnapshot is the worker-reported evidence for one host's robots.txt.
type RobotsSnapshot struct {
	Host          string          `json:"host"`
	RequestedURL  string          `json:"requestedUrl"`
	HTTPStatus    uint16          `json:"httpStatus"`
	ContentHash   string          `json:"contentHash"`
	ParserVersion string          `json:"parserVersion"`
	Rules         json.RawMessage `json:"rules"`
	Content       string          `json:"content"`
}

// BlockedURL records a URL that the run must not fetch because robots.txt disallows it.
type BlockedURL struct {
	RunID      string `json:"runId"`
	CrawlURLID string `json:"crawlUrlId"`
	Reason     string `json:"reason"`
}

// RobotsEvidence is the persisted robots.txt snapshot returned to the management UI.
type RobotsEvidence struct {
	Host          string          `json:"host"`
	RequestedURL  string          `json:"requestedUrl"`
	HTTPStatus    uint16          `json:"httpStatus"`
	ContentHash   string          `json:"contentHash"`
	ParserVersion string          `json:"parserVersion"`
	Rules         json.RawMessage `json:"rules"`
	Content       string          `json:"content"`
	FetchedAt     time.Time       `json:"fetchedAt"`
}

// SitemapEvidence is one persisted sitemap document for a crawl run.
type SitemapEvidence struct {
	URL           string    `json:"url"`
	ParentURL     string    `json:"parentUrl,omitempty"`
	HTTPStatus    uint16    `json:"httpStatus"`
	ContentHash   string    `json:"contentHash"`
	URLCount      uint32    `json:"urlCount"`
	ParserVersion string    `json:"parserVersion"`
	ErrorCode     string    `json:"errorCode,omitempty"`
	FetchedAt     time.Time `json:"fetchedAt"`
}

// SitemapDocument is one fetched sitemap (or sitemap index) and its parse outcome.
type SitemapDocument struct {
	URL           string `json:"url"`
	ParentURL     string `json:"parentUrl,omitempty"`
	HTTPStatus    uint16 `json:"httpStatus"`
	ContentHash   string `json:"contentHash"`
	URLCount      uint32 `json:"urlCount"`
	ParserVersion string `json:"parserVersion"`
	ErrorCode     string `json:"errorCode,omitempty"`
}

// SitemapReport carries sitemap evidence plus the URLs discovered from them.
type SitemapReport struct {
	RunID     string            `json:"runId"`
	Documents []SitemapDocument `json:"documents"`
	URLs      []string          `json:"urls"`
}
