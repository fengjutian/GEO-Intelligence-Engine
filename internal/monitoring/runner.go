package monitoring

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

type dueSchedule struct {
	tenantID string
	Schedule
}

// claimDue atomically leases due schedules so multiple API replicas cannot run the same one.
func (s *Store) claimDue(ctx context.Context) ([]dueSchedule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(tenant_id),BIN_TO_UUID(id),BIN_TO_UUID(project_id),BIN_TO_UUID(protocol_id),
interval_minutes,next_run_at,enabled,COALESCE(BIN_TO_UUID(last_audit_id),''),consecutive_failures,COALESCE(last_error,''),created_at
FROM monitoring_schedules WHERE enabled=TRUE AND next_run_at<=UTC_TIMESTAMP(6)
ORDER BY next_run_at LIMIT 5 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return nil, err
	}
	items := []dueSchedule{}
	for rows.Next() {
		var item dueSchedule
		var lastAudit, lastError string
		if err := rows.Scan(&item.tenantID, &item.ID, &item.ProjectID, &item.ProtocolID, &item.IntervalMinutes, &item.NextRunAt, &item.Enabled, &lastAudit, &item.ConsecutiveFailures, &lastError, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, err
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
		if _, err := tx.ExecContext(ctx, `UPDATE monitoring_schedules
SET next_run_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL interval_minutes MINUTE),updated_at=UTC_TIMESTAMP(6)
WHERE id=UUID_TO_BIN(?)`, item.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

// RunDue executes up to five due schedules.
func (s *Store) RunDue(ctx context.Context, logger *slog.Logger) {
	items, err := s.claimDue(ctx)
	if err != nil {
		logger.Error("monitoring claim failed", "error", err)
		return
	}
	for _, item := range items {
		if _, err := s.Run(ctx, item.tenantID, item.Schedule); err != nil {
			logger.Error("monitoring schedule run failed", "schedule_id", item.ID, "error", err)
			continue
		}
		logger.Info("monitoring schedule created audit", "schedule_id", item.ID, "project_id", item.ProjectID)
	}
	s.CheckExecutionFailures(ctx, logger)
}

// CheckExecutionFailures raises one alert per failed/cancelled audit.
func (s *Store) CheckExecutionFailures(ctx context.Context, logger *slog.Logger) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(m.tenant_id),BIN_TO_UUID(m.id),BIN_TO_UUID(m.project_id),BIN_TO_UUID(a.id),a.status
FROM monitoring_schedules m JOIN audits a ON a.id=m.last_audit_id
WHERE a.status IN ('failed','cancelled')`)
	if err != nil {
		logger.Error("monitoring execution check failed", "error", err)
		return
	}
	type failure struct {
		tenantID   string
		scheduleID string
		projectID  string
		auditID    string
		status     string
	}
	items := []failure{}
	for rows.Next() {
		var item failure
		if err := rows.Scan(&item.tenantID, &item.scheduleID, &item.projectID, &item.auditID, &item.status); err != nil {
			rows.Close()
			logger.Error("monitoring execution scan failed", "error", err)
			return
		}
		items = append(items, item)
	}
	rows.Close()
	for _, item := range items {
		message := "scheduled audit finished with status " + item.status
		if err := s.insertAlert(ctx, item.tenantID, item.projectID, item.scheduleID, item.auditID, "audit_execution_failed", message); err != nil {
			if !strings.Contains(err.Error(), "1062") {
				logger.Error("monitoring execution alert failed", "error", err)
			}
		}
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
