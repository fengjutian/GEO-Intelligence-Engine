package monitoring

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// SignalInterval returns how often project signals are re-evaluated.
func SignalInterval() time.Duration {
	minutes := 360
	if raw := os.Getenv("GEO_SIGNAL_INTERVAL_MINUTES"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 5 {
			minutes = parsed
		}
	}
	return time.Duration(minutes) * time.Minute
}

type projectRef struct {
	tenantID  string
	projectID string
}

func (s *Store) projectsWithAudits(ctx context.Context) ([]projectRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(tenant_id),BIN_TO_UUID(id) FROM projects ORDER BY 1 LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []projectRef{}
	for rows.Next() {
		var item projectRef
		if err := rows.Scan(&item.tenantID, &item.projectID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// EvaluateAllSignals re-checks every project that has at least one finished audit.
func (s *Store) EvaluateAllSignals(ctx context.Context, logger *slog.Logger) {
	projects, err := s.projectsWithAudits(ctx)
	if err != nil {
		logger.Error("list projects for signal evaluation failed", "error", err)
		return
	}
	for _, project := range projects {
		created, err := s.EvaluateSignals(ctx, project.tenantID, project.projectID)
		if err != nil {
			logger.Error("signal evaluation failed", "project_id", project.projectID, "error", err)
			continue
		}
		if len(created) > 0 {
			logger.Info("signal alerts raised", "project_id", project.projectID, "count", len(created))
		}
	}
}

// StartSignalLoop evaluates signals periodically until the context is cancelled.
func (s *Store) StartSignalLoop(ctx context.Context, logger *slog.Logger) {
	interval := SignalInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger.Info("signal alert loop started", "interval", interval.String())
	s.EvaluateAllSignals(ctx, logger)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.EvaluateAllSignals(ctx, logger)
		}
	}
}
