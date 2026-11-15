package signals

import (
	"context"
	"database/sql"
	"errors"
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
		id := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO source_mentions (id,tenant_id,project_id,source_url,title,snippet,source_type,sentiment,published_at,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,UTC_TIMESTAMP(6))`,
			id, tenantID, projectID, truncate(input.SourceURL, 1000), truncate(input.Title, 500), input.Snippet, sourceType, sentiment, input.PublishedAt); err != nil {
			return 0, err
		}
		inserted++
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
	query := `SELECT BIN_TO_UUID(id),source_url,title,COALESCE(snippet,''),source_type,sentiment,published_at,created_at
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
		if err := rows.Scan(&item.ID, &item.SourceURL, &item.Title, &item.Snippet, &item.SourceType, &item.Sentiment, &published, &item.CreatedAt); err != nil {
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
