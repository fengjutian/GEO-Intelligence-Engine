package monitoring

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/fengjutian/geo-intelligence-engine/internal/notify"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("monitoring schedule was not found")
var ErrInvalid = errors.New("monitoring schedule input is invalid")

type Schedule struct {
	ID                  string    `json:"id"`
	ProjectID           string    `json:"projectId"`
	ProtocolID          string    `json:"protocolId"`
	IntervalMinutes     int       `json:"intervalMinutes"`
	NextRunAt           time.Time `json:"nextRunAt"`
	Enabled             bool      `json:"enabled"`
	LastAuditID         *string   `json:"lastAuditId"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	LastError           *string   `json:"lastError"`
	CreatedAt           time.Time `json:"createdAt"`
}

type Alert struct {
	ID         string     `json:"id"`
	ScheduleID string     `json:"scheduleId"`
	AuditID    *string    `json:"auditId"`
	Kind       string     `json:"kind"`
	Message    string     `json:"message"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

type Store struct {
	db       *sql.DB
	notifier *notify.Notifier
}

func NewStore(db *sql.DB, notifier *notify.Notifier) *Store {
	return &Store{db: db, notifier: notifier}
}

func (s *Store) Create(ctx context.Context, tenantID, projectID, protocolID string, intervalMinutes int) (Schedule, error) {
	if intervalMinutes < 60 || intervalMinutes > 24*60*30 {
		return Schedule{}, ErrInvalid
	}
	var published int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM measurement_protocols WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND status='published'`, tenantID, projectID, protocolID).Scan(&published); err != nil {
		return Schedule{}, err
	}
	if published == 0 {
		return Schedule{}, ErrInvalid
	}
	id := uuid.NewString()
	nextRun := time.Now().UTC().Add(time.Duration(intervalMinutes) * time.Minute)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO monitoring_schedules
(id,tenant_id,project_id,protocol_id,interval_minutes,next_run_at,enabled,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,TRUE,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		id, tenantID, projectID, protocolID, intervalMinutes, nextRun); err != nil {
		return Schedule{}, err
	}
	return s.get(ctx, tenantID, id)
}

func (s *Store) List(ctx context.Context, tenantID, projectID string) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),BIN_TO_UUID(protocol_id),interval_minutes,next_run_at,enabled,
COALESCE(BIN_TO_UUID(last_audit_id),''),consecutive_failures,COALESCE(last_error,''),created_at
FROM monitoring_schedules WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Schedule{}
	for rows.Next() {
		var item Schedule
		var lastAudit, lastError string
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProtocolID, &item.IntervalMinutes, &item.NextRunAt, &item.Enabled, &lastAudit, &item.ConsecutiveFailures, &lastError, &item.CreatedAt); err != nil {
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
	return items, rows.Err()
}

func (s *Store) Update(ctx context.Context, tenantID, scheduleID string, enabled *bool, intervalMinutes *int) (Schedule, error) {
	if intervalMinutes != nil && (*intervalMinutes < 60 || *intervalMinutes > 24*60*30) {
		return Schedule{}, ErrInvalid
	}
	if enabled == nil && intervalMinutes == nil {
		return Schedule{}, ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE monitoring_schedules SET
enabled=COALESCE(?,enabled), interval_minutes=COALESCE(?,interval_minutes), updated_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, enabled, intervalMinutes, tenantID, scheduleID)
	if err != nil {
		return Schedule{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return Schedule{}, ErrNotFound
	}
	return s.get(ctx, tenantID, scheduleID)
}

func (s *Store) Delete(ctx context.Context, tenantID, scheduleID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM monitoring_schedules WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, scheduleID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Trigger(ctx context.Context, tenantID, scheduleID string) (Schedule, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE monitoring_schedules SET next_run_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, scheduleID)
	if err != nil {
		return Schedule{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return Schedule{}, ErrNotFound
	}
	return s.get(ctx, tenantID, scheduleID)
}

func (s *Store) Alerts(ctx context.Context, tenantID, projectID string) ([]Alert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(a.id),BIN_TO_UUID(a.schedule_id),COALESCE(BIN_TO_UUID(a.audit_id),''),a.kind,a.message,a.status,a.created_at,a.resolved_at
FROM monitoring_alerts a JOIN monitoring_schedules s ON s.id=a.schedule_id
WHERE a.tenant_id=UUID_TO_BIN(?) AND s.project_id=UUID_TO_BIN(?) ORDER BY a.created_at DESC LIMIT 100`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Alert{}
	for rows.Next() {
		var item Alert
		var auditID string
		if err := rows.Scan(&item.ID, &item.ScheduleID, &auditID, &item.Kind, &item.Message, &item.Status, &item.CreatedAt, &item.ResolvedAt); err != nil {
			return nil, err
		}
		if auditID != "" {
			item.AuditID = &auditID
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ResolveAlert(ctx context.Context, tenantID, alertID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE monitoring_alerts SET status='resolved',resolved_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, alertID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) get(ctx context.Context, tenantID, scheduleID string) (Schedule, error) {
	var item Schedule
	var lastAudit, lastError string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),BIN_TO_UUID(protocol_id),interval_minutes,next_run_at,enabled,
COALESCE(BIN_TO_UUID(last_audit_id),''),consecutive_failures,COALESCE(last_error,''),created_at
FROM monitoring_schedules WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, scheduleID).Scan(&item.ID, &item.ProjectID, &item.ProtocolID, &item.IntervalMinutes, &item.NextRunAt, &item.Enabled, &lastAudit, &item.ConsecutiveFailures, &lastError, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	if lastAudit != "" {
		item.LastAuditID = &lastAudit
	}
	if lastError != "" {
		item.LastError = &lastError
	}
	return item, err
}

// Run executes one due schedule and creates an audit.
func (s *Store) Run(ctx context.Context, tenantID string, schedule Schedule) (string, error) {
	key := "monitoring:" + schedule.ID + ":" + time.Now().UTC().Format("200601021504")
	audit, err := audits.NewStore(s.db).CreateAudit(ctx, tenantID, schedule.ProjectID, schedule.ProtocolID, key)
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE monitoring_schedules SET consecutive_failures=consecutive_failures+1,
next_run_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL LEAST(interval_minutes,5*(consecutive_failures+1)) MINUTE),
last_error=?,updated_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, err.Error(), schedule.ID)
		_ = s.insertAlert(ctx, tenantID, schedule.ProjectID, schedule.ID, "", "audit_create_failed", err.Error())
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE monitoring_schedules SET last_audit_id=UUID_TO_BIN(?),consecutive_failures=0,last_error=NULL,
next_run_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL interval_minutes MINUTE),updated_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, audit.ID, schedule.ID); err != nil {
		return "", err
	}
	return audit.ID, nil
}

func (s *Store) insertAlert(ctx context.Context, tenantID, projectID, scheduleID, auditID, kind, message string) error {
	alertID := uuid.NewString()
	var auditValue any
	if auditID != "" {
		auditValue = auditID
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO monitoring_alerts (id,tenant_id,schedule_id,audit_id,kind,message,status,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,'open',UTC_TIMESTAMP(6))`, alertID, tenantID, scheduleID, auditValue, kind, message); err != nil {
		return err
	}
	if s.notifier != nil && s.notifier.Enabled() {
		payload := notify.Alert{ID: alertID, ProjectID: projectID, ScheduleID: scheduleID, AuditID: auditID, Kind: kind, Message: message}
		delivery := s.notifier.Send(ctx, payload)
		_, _ = s.db.ExecContext(ctx, `UPDATE monitoring_alerts SET delivery_json=CAST(? AS JSON) WHERE id=UUID_TO_BIN(?)`, delivery, alertID)
	}
	return nil
}
