package monitoring

import (
	"fmt"
	"time"
)

// Signal alert kinds.
const (
	AlertShareDrop          = "share_drop"
	AlertCompetitorOvertake = "competitor_overtake"
	AlertCrawlerSilent      = "crawler_silent"
)

// ShareDropThreshold is the absolute share drop that raises an alert.
const ShareDropThreshold = 0.15

// CrawlerSilenceDays is how long without an AI crawl raises an alert.
const CrawlerSilenceDays = 7

// SignalAlert is one project-level condition worth operator attention.
type SignalAlert struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Severity   string         `json:"severity"`
	Message    string         `json:"message"`
	Status     string         `json:"status"`
	Details    map[string]any `json:"details,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	ResolvedAt *time.Time     `json:"resolvedAt,omitempty"`
}

// EntityShare is one entity's share in an audit.
type EntityShare struct {
	Name  string
	Share float64
}

// ShouldAlertShareDrop reports whether the brand share fell far enough.
func ShouldAlertShareDrop(previous, current float64) bool {
	return previous-current >= ShareDropThreshold
}

// CompetitorOvertake returns the strongest competitor that outranks the brand.
func CompetitorOvertake(brandShare float64, competitors []EntityShare) (string, bool) {
	best := ""
	bestShare := brandShare
	for _, item := range competitors {
		if item.Share > bestShare {
			best, bestShare = item.Name, item.Share
		}
	}
	return best, best != ""
}

// ShareDropMessage renders the operator-facing alert text.
func ShareDropMessage(previous, current float64) string {
	return fmt.Sprintf("品牌份额从 %.1f%% 下降到 %.1f%%，降幅 %.1f 个百分点（阈值 %.0f）",
		previous*100, current*100, (previous-current)*100, ShareDropThreshold*100)
}
