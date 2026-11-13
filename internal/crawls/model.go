package crawls

import "time"

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
}

type Claim struct {
	RunID      string `json:"runId"`
	CrawlURLID string `json:"crawlUrlId"`
	WebsiteID  string `json:"websiteId"`
	URL        string `json:"url"`
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
