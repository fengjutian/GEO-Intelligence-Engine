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
	DiscoveredCount uint32     `json:"discoveredCount"`
	FetchedCount    uint32     `json:"fetchedCount"`
	FailedCount     uint32     `json:"failedCount"`
	StartedAt       *time.Time `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type Claim struct {
	RunID      string `json:"runId"`
	CrawlURLID string `json:"crawlUrlId"`
	WebsiteID  string `json:"websiteId"`
	URL        string `json:"url"`
	MaxBytes   int64  `json:"maxBytes"`
}

type Completion struct {
	RunID         string  `json:"runId"`
	CrawlURLID    string  `json:"crawlUrlId"`
	FinalURL      string  `json:"finalUrl"`
	HTTPStatus    int     `json:"httpStatus"`
	Title         string  `json:"title"`
	Language      *string `json:"language"`
	ContentHash   string  `json:"contentHash"`
	ParserVersion string  `json:"parserVersion"`
}

type Failure struct {
	RunID      string `json:"runId"`
	CrawlURLID string `json:"crawlUrlId"`
	ErrorCode  string `json:"errorCode"`
}
