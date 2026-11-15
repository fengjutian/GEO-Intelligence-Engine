package monitoring

import (
	"context"
	"log/slog"
	"time"
)

type dueSchedule struct {
	tenantID string
	Schedule
}

// RunDue executes up to five due schedules.
func (s *Store) RunDue(ctx context.Context, logger *slog.Logger) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(tenant_id),BIN_TO_UUID(id),BIN_TO_UUID(project_id),BIN_TO_UUID(protocol_id),
interval_minutes,next_run_at,enabled,COALESCE(BIN_TO_UUID(last_audit_id),''),consecutive_failures,COALESCE(last_error,''),created_at
FROM monitoring_schedules WHERE enabled=TRUE AND next_run_at<=UTC_TIMESTAMP(6) ORDER BY next_run_at LIMIT 5`)
	if err != nil {
		logger.Error("monitoring due query failed", "error", err)
		return
	}
	items := []dueSchedule{}
	for rows.Next() {
		var item dueSchedule
		var lastAudit, lastError string
		if err := rows.Scan(&item.tenantID, &item.ID, &item.ProjectID, &item.ProtocolID, &item.IntervalMinutes, &item.NextRunAt, &item.Enabled, &lastAudit, &item.ConsecutiveFailures, &lastError, &item.CreatedAt); err != nil {
			rows.Close()
			logger.Error("monitoring due scan failed", "error", err)
			return
		}
		if lastAudit != "" {
			item.LastAuditID = &lastAudit
		}
		if lastError != "" {
			item.LastError = &lastError
		}
		items = append(items, item)
	}
	rows.Close()
	for _, item := range items {
		if _, err := s.Run(ctx, item.tenantID, item.Schedule); err != nil {
			logger.Error("monitoring schedule run failed", "schedule_id", item.ID, "error", err)
			continue
		}
		logger.Info("monitoring schedule created audit", "schedule_id", item.ID, "project_id", item.ProjectID)
	}
}

// Start runs the scheduler loop until the context ends.
func (s *Store) Start(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	s.RunDue(ctx, logger)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RunDue(ctx, logger)
		}
	}
}
