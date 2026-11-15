package signals

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalid marks unusable input.
var ErrInvalid = errors.New("signal input is invalid")

// MaxBatch bounds one import request.
const MaxBatch = 1000

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// IngestBotLogs stores one batch of crawler hits and returns the inserted count.
func (s *Store) IngestBotLogs(ctx context.Context, tenantID, projectID string, inputs []BotLogInput) (int, error) {
	if len(inputs) == 0 || len(inputs) > MaxBatch {
		return 0, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	inserted := 0
	for _, input := range inputs {
		path := strings.TrimSpace(input.Path)
		if path == "" {
			return 0, ErrInvalid
		}
		occurred := input.OccurredAt
		if occurred.IsZero() {
			occurred = time.Now().UTC()
		}
		id := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_log_events (id,tenant_id,project_id,bot_name,user_agent,path,http_status,occurred_at,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,UTC_TIMESTAMP(6))`,
			id, tenantID, projectID, ClassifyBot(input.UserAgent), truncate(input.UserAgent, 500), truncate(path, 1000), input.HTTPStatus, occurred.UTC()); err != nil {
			return 0, err
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// BotSummary aggregates crawler hits per bot over the last N days.
func (s *Store) BotSummary(ctx context.Context, tenantID, projectID string, days int) ([]BotStat, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	rows, err := s.db.QueryContext(ctx, `SELECT bot_name,COUNT(*),COUNT(DISTINCT path),MAX(occurred_at)
FROM bot_log_events
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND occurred_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? DAY)
GROUP BY bot_name ORDER BY COUNT(*) DESC`, tenantID, projectID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := []BotStat{}
	for rows.Next() {
		var item BotStat
		if err := rows.Scan(&item.BotName, &item.Hits, &item.Pages, &item.LastSeenAt); err != nil {
			return nil, err
		}
		item.IsAIBot = IsAIBot(item.BotName)
		stats = append(stats, item)
	}
	return stats, rows.Err()
}

// IngestMentions stores a batch of third-party mentions.
func (s *Store) IngestMentions(ctx context.Context, tenantID, projectID string, inputs []MentionInput) (int, error) {
	if len(inputs) == 0 || len(inputs) > MaxBatch {
		return 0, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	inserted := 0
	for _, input := range inputs {
		if strings.TrimSpace(input.SourceURL) == "" || strings.TrimSpace(input.Title) == "" {
			return 0, ErrInvalid
		}
		sourceType := normalizeSourceType(input.SourceType)
		sentiment := normalizeSentiment(input.Sentiment)
		authority := AuthorityScore(hostOf(input.SourceURL), s.brandHost(ctx, tenantID, projectID))
		hash := DedupeKey(input.SourceURL, input.Title)
		id := uuid.NewString()
		result, err := tx.ExecContext(ctx, `INSERT INTO source_mentions (id,tenant_id,project_id,source_url,title,snippet,source_type,sentiment,authority,discovery,dedupe_hash,published_at,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,'manual',?,?,UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE id=id`,
			id, tenantID, projectID, truncate(input.SourceURL, 1000), truncate(input.Title, 500), input.Snippet, sourceType, sentiment, authority, hash, input.PublishedAt)
		if err != nil {
			return 0, err
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// ListMentions returns the newest mentions first.
func (s *Store) ListMentions(ctx context.Context, tenantID, projectID, sourceType string, limit int) ([]Mention, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT BIN_TO_UUID(id),source_url,title,COALESCE(snippet,''),source_type,sentiment,authority,discovery,published_at,created_at
FROM source_mentions WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`
	args := []any{tenantID, projectID}
	if sourceType != "" {
		query += " AND source_type=?"
		args = append(args, sourceType)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Mention{}
	for rows.Next() {
		var item Mention
		var published sql.NullTime
		if err := rows.Scan(&item.ID, &item.SourceURL, &item.Title, &item.Snippet, &item.SourceType, &item.Sentiment, &item.Authority, &item.Discovery, &published, &item.CreatedAt); err != nil {
			return nil, err
		}
		if published.Valid {
			value := published.Time
			item.PublishedAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// MentionSummary groups mentions by source type with sentiment counts.
func (s *Store) MentionSummary(ctx context.Context, tenantID, projectID string) ([]SourceTypeCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_type,COUNT(*),
COALESCE(SUM(sentiment='positive'),0),COALESCE(SUM(sentiment='negative'),0)
FROM source_mentions WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)
GROUP BY source_type ORDER BY COUNT(*) DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := []SourceTypeCount{}
	for rows.Next() {
		var item SourceTypeCount
		if err := rows.Scan(&item.SourceType, &item.Count, &item.Positive, &item.Negative); err != nil {
			return nil, err
		}
		counts = append(counts, item)
	}
	return counts, rows.Err()
}

var sourceTypes = map[string]bool{"review": true, "media": true, "wiki": true, "forum": true, "vendor": true, "other": true}

func normalizeSourceType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if sourceTypes[normalized] {
		return normalized
	}
	return "other"
}

func normalizeSentiment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "positive":
		return "positive"
	case "negative":
		return "negative"
	default:
		return "neutral"
	}
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// IngestResult reports what one ingest call changed.
type IngestResult struct {
	Inserted int `json:"inserted"`
	Skipped  int `json:"skipped"`
}

// IngestAccessLog parses raw Apache/Nginx combined lines and stores the page hits.
func (s *Store) IngestAccessLog(ctx context.Context, tenantID, projectID, content string) (IngestResult, error) {
	if strings.TrimSpace(content) == "" {
		return IngestResult{}, ErrInvalid
	}
	lines := strings.Split(content, "\n")
	if len(lines) > MaxBatch {
		lines = lines[:MaxBatch]
	}
	parsed, skipped := ParseAccessLog(lines)
	inputs := make([]BotLogInput, 0, len(parsed))
	for _, line := range parsed {
		inputs = append(inputs, BotLogInput{UserAgent: line.UserAgent, Path: line.Path, HTTPStatus: line.HTTPStatus, OccurredAt: line.OccurredAt})
	}
	if len(inputs) == 0 {
		return IngestResult{Skipped: skipped}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return IngestResult{}, err
	}
	defer tx.Rollback()
	for _, input := range inputs {
		occurred := input.OccurredAt
		if occurred.IsZero() {
			occurred = time.Now().UTC()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_log_events (id,tenant_id,project_id,bot_name,user_agent,path,http_status,ingest_source,occurred_at,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,'access_log',?,UTC_TIMESTAMP(6))`,
			uuid.NewString(), tenantID, projectID, ClassifyBot(input.UserAgent), truncate(input.UserAgent, 500), truncate(input.Path, 1000), input.HTTPStatus, occurred.UTC()); err != nil {
			return IngestResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return IngestResult{}, err
	}
	return IngestResult{Inserted: len(inputs), Skipped: skipped}, nil
}

// CoverageReport compares crawled pages with what AI crawlers actually requested.
type CoverageReport struct {
	Pages         int             `json:"pages"`
	AIVisited     int             `json:"aiVisited"`
	NeverVisited  []string        `json:"neverVisited"`
	RobotsBlocked int             `json:"robotsBlocked"`
	Status        StatusBreakdown `json:"status"`
}

// Coverage reports which known pages AI crawlers never requested.
func (s *Store) Coverage(ctx context.Context, tenantID, projectID string, days int) (CoverageReport, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	report := CoverageReport{NeverVisited: []string{}}
	pageRows, err := s.db.QueryContext(ctx, `SELECT pc.final_url FROM page_contents pc
JOIN crawl_runs cr ON cr.id=pc.crawl_run_id
WHERE pc.tenant_id=UUID_TO_BIN(?) AND cr.project_id=UUID_TO_BIN(?)
ORDER BY pc.created_at DESC LIMIT 500`, tenantID, projectID)
	if err != nil {
		return report, err
	}
	pagePaths := map[string]bool{}
	for pageRows.Next() {
		var rawURL string
		if err := pageRows.Scan(&rawURL); err != nil {
			pageRows.Close()
			return report, err
		}
		if path := URLPath(rawURL); path != "" {
			pagePaths[path] = true
		}
	}
	pageRows.Close()
	report.Pages = len(pagePaths)

	botRows, err := s.db.QueryContext(ctx, `SELECT bot_name,path,http_status FROM bot_log_events
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND occurred_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? DAY)
LIMIT 20000`, tenantID, projectID, days)
	if err != nil {
		return report, err
	}
	defer botRows.Close()
	visited := map[string]bool{}
	statuses := []int{}
	for botRows.Next() {
		var bot, path string
		var status int
		if err := botRows.Scan(&bot, &path, &status); err != nil {
			return report, err
		}
		if !IsAIBot(bot) {
			continue
		}
		visited[path] = true
		statuses = append(statuses, status)
	}
	report.Status = SummarizeStatuses(statuses)
	report.RobotsBlocked = report.Status.Forbidden
	for path := range pagePaths {
		if !visited[path] && len(report.NeverVisited) < 100 {
			report.NeverVisited = append(report.NeverVisited, path)
		}
	}
	if len(visited) > 0 {
		hits := 0
		for path := range pagePaths {
			if visited[path] {
				hits++
			}
		}
		report.AIVisited = hits
	}
	return report, nil
}

// DiscoverMentions scans a watchlist of URLs for brand mentions, deduplicating
// by normalized URL + title and scoring each source by domain authority.
func (s *Store) DiscoverMentions(ctx context.Context, tenantID, projectID string, urls []string) (DiscoverResult, error) {
	if len(urls) == 0 || len(urls) > 20 {
		return DiscoverResult{}, ErrInvalid
	}
	brand, aliases, brandHost := s.brandContext(ctx, tenantID, projectID)
	result := DiscoverResult{Errors: []string{}}
	seen := map[string]bool{}
	for _, raw := range urls {
		target := strings.TrimSpace(raw)
		if target == "" || !isFetchableURL(target) || seen[NormalizeForDedupe(target)] {
			result.Skipped++
			continue
		}
		seen[NormalizeForDedupe(target)] = true
		html, err := fetchPage(target)
		if err != nil || html == "" {
			result.Skipped++
			if err != nil {
				result.Errors = append(result.Errors, target+": "+err.Error())
			}
			continue
		}
		result.Fetched++
		title, snippet, found := ExtractMention(html, brand, aliases)
		if !found {
			result.Skipped++
			continue
		}
		if title == "" {
			title = target
		}
		authority := AuthorityScore(hostOf(target), brandHost)
		hash := DedupeKey(target, title)
		insert, err := s.db.ExecContext(ctx, `INSERT INTO source_mentions (id,tenant_id,project_id,source_url,title,snippet,source_type,sentiment,authority,discovery,dedupe_hash,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?, 'watchlist',?,UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE id=id`,
			uuid.NewString(), tenantID, projectID, truncate(target, 1000), truncate(title, 500), truncate(snippet, 2000),
			sourceTypeFromAuthority(authority), "neutral", authority, hash)
		if err != nil {
			result.Errors = append(result.Errors, target+": "+err.Error())
			continue
		}
		if affected, _ := insert.RowsAffected(); affected > 0 {
			result.Discovered++
		} else {
			result.Duplicates++
		}
	}
	return result, nil
}

func sourceTypeFromAuthority(authority float64) string {
	switch {
	case authority >= 0.85:
		return "wiki"
	case authority >= 0.7:
		return "media"
	case authority >= 0.5:
		return "review"
	default:
		return "other"
	}
}

func (s *Store) brandContext(ctx context.Context, tenantID, projectID string) (string, []string, string) {
	var name, domain string
	_ = s.db.QueryRowContext(ctx, `SELECT canonical_name,COALESCE(official_domain,'') FROM brands
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at LIMIT 1`, tenantID, projectID).Scan(&name, &domain)
	aliases := []string{}
	rows, err := s.db.QueryContext(ctx, `SELECT ea.alias FROM brand_aliases ea JOIN brands b ON b.id=ea.brand_id
WHERE ea.tenant_id=UUID_TO_BIN(?) AND b.project_id=UUID_TO_BIN(?) AND ea.enabled=TRUE LIMIT 20`, tenantID, projectID)
	if err == nil {
		for rows.Next() {
			var alias string
			if err := rows.Scan(&alias); err == nil {
				aliases = append(aliases, alias)
			}
		}
		rows.Close()
	}
	return name, aliases, strings.ToLower(domain)
}

// brandHost returns the brand's own domain to score self-published pages lower.
func (s *Store) brandHost(ctx context.Context, tenantID, projectID string) string {
	var domain string
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(official_domain,'') FROM brands
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at LIMIT 1`, tenantID, projectID).Scan(&domain)
	return strings.ToLower(domain)
}

// hostOf extracts the lowercase hostname from a URL.
func hostOf(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// URLPath extracts the path of a URL for coverage comparison.
func URLPath(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	path := parsed.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}
