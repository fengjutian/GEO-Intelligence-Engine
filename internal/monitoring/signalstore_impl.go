package monitoring

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/signals"
	"github.com/google/uuid"
)

// ErrAlertNotFound marks a missing signal alert.
var ErrAlertNotFound = errors.New("signal alert was not found")

// EvaluateSignals checks share drop, competitor overtake and crawler silence.
func (s *Store) EvaluateSignals(ctx context.Context, tenantID, projectID string) ([]SignalAlert, error) {
	created := []SignalAlert{}
	audits, err := s.auditsWithPrompt(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if len(audits) > 0 {
		latest := audits[0]
		brand, competitors, err := s.auditShares(ctx, tenantID, latest.id)
		if err != nil {
			return nil, err
		}
		if overtaker, ok := CompetitorOvertake(brand, competitors); ok {
			alert, err := s.raiseSignal(ctx, tenantID, projectID, AlertCompetitorOvertake, "high",
				"竞品 "+overtaker+" 的品牌份额已超过本品牌", map[string]any{"competitor": overtaker, "brandShare": brand})
			if err != nil {
				return nil, err
			}
			if alert != nil {
				created = append(created, *alert)
			}
		}
		if len(audits) > 1 && audits[1].prompt == latest.prompt {
			previousBrand, _, err := s.auditShares(ctx, tenantID, audits[1].id)
			if err != nil {
				return nil, err
			}
			if ShouldAlertShareDrop(previousBrand, brand) {
				alert, err := s.raiseSignal(ctx, tenantID, projectID, AlertShareDrop, "high",
					ShareDropMessage(previousBrand, brand), map[string]any{"previousShare": previousBrand, "currentShare": brand})
				if err != nil {
					return nil, err
				}
				if alert != nil {
					created = append(created, *alert)
				}
			}
		}
	}

	silent, err := s.crawlerSilent(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if silent {
		alert, err := s.raiseSignal(ctx, tenantID, projectID, AlertCrawlerSilent, "medium",
			"近 7 天没有任何 AI 爬虫访问，此前有抓取记录", map[string]any{"days": CrawlerSilenceDays})
		if err != nil {
			return nil, err
		}
		if alert != nil {
			created = append(created, *alert)
		}
	}
	return created, nil
}

type auditPrompt struct {
	id     string
	prompt string
}

func (s *Store) auditsWithPrompt(ctx context.Context, tenantID, projectID string) ([]auditPrompt, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(a.id),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(p.config_json,'$.promptTemplateVersion')),'')
FROM audits a JOIN measurement_protocols p ON p.id=a.protocol_id
WHERE a.tenant_id=UUID_TO_BIN(?) AND a.project_id=UUID_TO_BIN(?) AND a.status IN ('succeeded','partial_success')
ORDER BY a.created_at DESC LIMIT 10`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []auditPrompt{}
	for rows.Next() {
		var item auditPrompt
		if err := rows.Scan(&item.id, &item.prompt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) auditShares(ctx context.Context, tenantID, auditID string) (float64, []EntityShare, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT JSON_UNQUOTE(JSON_EXTRACT(mv.dimensions_json,'$.entityType')),
JSON_UNQUOTE(JSON_EXTRACT(mv.dimensions_json,'$.entity')),COALESCE(mv.value_decimal,0)
FROM metric_values mv
WHERE mv.snapshot_id=(SELECT id FROM metric_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?) ORDER BY generated_at DESC,id DESC LIMIT 1)
  AND mv.metric_code='share_of_voice' AND mv.aggregation_method='answer_equal'`, tenantID, auditID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	brand := 0.0
	competitors := []EntityShare{}
	for rows.Next() {
		var entityType, name string
		var share float64
		if err := rows.Scan(&entityType, &name, &share); err != nil {
			return 0, nil, err
		}
		if entityType == "brand" {
			brand = share
			continue
		}
		competitors = append(competitors, EntityShare{Name: name, Share: share})
	}
	return brand, competitors, rows.Err()
}

func (s *Store) crawlerSilent(ctx context.Context, tenantID, projectID string) (bool, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_log_events WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`, tenantID, projectID).Scan(&total); err != nil {
		return false, err
	}
	if total == 0 {
		return false, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT bot_name FROM bot_log_events
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)
  AND occurred_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? DAY)`, tenantID, projectID, CrawlerSilenceDays)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	aiHits := 0
	for rows.Next() {
		var bot string
		if err := rows.Scan(&bot); err != nil {
			return false, err
		}
		if signals.IsAIBot(bot) {
			aiHits++
		}
	}
	return aiHits == 0, rows.Err()
}

// raiseSignal inserts an alert unless the same kind is already open.
func (s *Store) raiseSignal(ctx context.Context, tenantID, projectID, kind, severity, message string, details map[string]any) (*SignalAlert, error) {
	var open int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM signal_alerts
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND kind=? AND status='open'`,
		tenantID, projectID, kind).Scan(&open); err != nil {
		return nil, err
	}
	if open > 0 {
		return nil, nil
	}
	payload, _ := json.Marshal(details)
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO signal_alerts (id,tenant_id,project_id,kind,severity,message,status,details_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'open',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		id, tenantID, projectID, kind, severity, message, payload); err != nil {
		return nil, err
	}
	return &SignalAlert{ID: id, Kind: kind, Severity: severity, Message: message, Status: "open", Details: details, CreatedAt: time.Now().UTC()}, nil
}

// ListSignalAlerts returns alerts newest first.
func (s *Store) ListSignalAlerts(ctx context.Context, tenantID, projectID, status string) ([]SignalAlert, error) {
	query := `SELECT BIN_TO_UUID(id),kind,severity,message,status,COALESCE(details_json,'{}'),created_at,resolved_at
FROM signal_alerts WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`
	args := []any{tenantID, projectID}
	if status != "" {
		query += " AND status=?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT 100"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SignalAlert{}
	for rows.Next() {
		var item SignalAlert
		var details []byte
		var resolved sql.NullTime
		if err := rows.Scan(&item.ID, &item.Kind, &item.Severity, &item.Message, &item.Status, &details, &item.CreatedAt, &resolved); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(details, &item.Details)
		if resolved.Valid {
			item.ResolvedAt = &resolved.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ResolveSignalAlert closes one alert.
func (s *Store) ResolveSignalAlert(ctx context.Context, tenantID, alertID string) (SignalAlert, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE signal_alerts SET status='resolved',resolved_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND status='open'`, tenantID, alertID)
	if err != nil {
		return SignalAlert{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return SignalAlert{}, ErrAlertNotFound
	}
	var item SignalAlert
	var details []byte
	var resolved sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),kind,severity,message,status,COALESCE(details_json,'{}'),created_at,resolved_at
FROM signal_alerts WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, alertID).Scan(
		&item.ID, &item.Kind, &item.Severity, &item.Message, &item.Status, &details, &item.CreatedAt, &resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return SignalAlert{}, ErrAlertNotFound
	}
	if err != nil {
		return SignalAlert{}, err
	}
	_ = json.Unmarshal(details, &item.Details)
	if resolved.Valid {
		item.ResolvedAt = &resolved.Time
	}
	return item, nil
}
