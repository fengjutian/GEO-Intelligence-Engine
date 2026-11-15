package experiments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound marks a missing experiment.
var ErrNotFound = errors.New("experiment was not found")

// ErrInvalid marks unusable input.
var ErrInvalid = errors.New("experiment input is invalid")

// ErrConflict marks an experiment in the wrong state.
var ErrConflict = errors.New("experiment is not in a runnable state")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Create declares an experiment with a frozen hypothesis and metric.
func (s *Store) Create(ctx context.Context, tenantID, projectID string, input CreateInput) (Experiment, error) {
	name := strings.TrimSpace(input.Name)
	hypothesis := strings.TrimSpace(input.Hypothesis)
	metric := strings.TrimSpace(input.MetricCode)
	if name == "" || hypothesis == "" || metric == "" {
		return Experiment{}, ErrInvalid
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID).Scan(&exists); err != nil {
		return Experiment{}, err
	}
	if exists == 0 {
		return Experiment{}, ErrNotFound
	}
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO experiments (id,tenant_id,project_id,name,hypothesis,metric_code,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'draft',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		id, tenantID, projectID, name, hypothesis, metric); err != nil {
		return Experiment{}, err
	}
	return s.Get(ctx, tenantID, projectID, id)
}

// List returns experiments newest first.
func (s *Store) List(ctx context.Context, tenantID, projectID string) ([]Experiment, error) {
	items := []Experiment{}
	direct, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),name,hypothesis,metric_code,baseline_audit_id,treatment_audit_id,status,conclusion,stats_json,created_at,updated_at
FROM experiments WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer direct.Close()
	for direct.Next() {
		item, err := scanExperiment(direct)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, direct.Err()
}

// Get returns one experiment.
func (s *Store) Get(ctx context.Context, tenantID, projectID, id string) (Experiment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(project_id),name,hypothesis,metric_code,baseline_audit_id,treatment_audit_id,status,conclusion,stats_json,created_at,updated_at
FROM experiments WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID, id)
	item, err := scanExperiment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Experiment{}, ErrNotFound
	}
	return item, err
}

// Attach binds the baseline and treatment audits.
func (s *Store) Attach(ctx context.Context, tenantID, projectID, id, baselineAuditID, treatmentAuditID string) (Experiment, error) {
	if _, err := uuid.Parse(baselineAuditID); err != nil {
		return Experiment{}, ErrInvalid
	}
	if _, err := uuid.Parse(treatmentAuditID); err != nil {
		return Experiment{}, ErrInvalid
	}
	var valid int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)
AND id IN (UUID_TO_BIN(?),UUID_TO_BIN(?)) AND status IN ('succeeded','partial_success')`, tenantID, projectID, baselineAuditID, treatmentAuditID).Scan(&valid); err != nil {
		return Experiment{}, err
	}
	if valid != 2 {
		return Experiment{}, ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE experiments SET baseline_audit_id=UUID_TO_BIN(?),treatment_audit_id=UUID_TO_BIN(?),status='running',updated_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, baselineAuditID, treatmentAuditID, tenantID, projectID, id)
	if err != nil {
		return Experiment{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Experiment{}, ErrNotFound
	}
	return s.Get(ctx, tenantID, projectID, id)
}

// Evaluate runs the comparison and freezes the conclusion.
func (s *Store) Evaluate(ctx context.Context, tenantID, projectID, id string) (Experiment, error) {
	item, err := s.Get(ctx, tenantID, projectID, id)
	if err != nil {
		return Experiment{}, err
	}
	if item.Status != "running" || item.BaselineAuditID == nil || item.TreatmentAuditID == nil {
		return Experiment{}, ErrConflict
	}
	baseline, err := s.metricCounts(ctx, tenantID, *item.BaselineAuditID, item.MetricCode)
	if err != nil {
		return Experiment{}, err
	}
	treatment, err := s.metricCounts(ctx, tenantID, *item.TreatmentAuditID, item.MetricCode)
	if err != nil {
		return Experiment{}, err
	}
	result := Compare(baseline, treatment)
	stats, _ := json.Marshal(result)
	conclusion := Conclusion(result)
	if _, err := s.db.ExecContext(ctx, `UPDATE experiments SET status='concluded',conclusion=?,stats_json=CAST(? AS JSON),updated_at=UTC_TIMESTAMP(6)
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, conclusion, stats, tenantID, projectID, id); err != nil {
		return Experiment{}, err
	}
	return s.Get(ctx, tenantID, projectID, id)
}

func (s *Store) metricCounts(ctx context.Context, tenantID, auditID, code string) (Counts, error) {
	var numerator, denominator sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(mv.numerator),0),COALESCE(SUM(mv.denominator),0)
FROM metric_values mv
WHERE mv.snapshot_id=(SELECT id FROM metric_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?) ORDER BY generated_at DESC,id DESC LIMIT 1)
  AND mv.metric_code=? AND mv.aggregation_method='answer_equal' AND JSON_LENGTH(mv.dimensions_json)=1`,
		tenantID, auditID, code).Scan(&numerator, &denominator)
	if err != nil {
		return Counts{}, err
	}
	return Counts{Numerator: int(numerator.Float64), Denominator: int(denominator.Float64)}, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanExperiment(row rowScanner) (Experiment, error) {
	var item Experiment
	var baseline, treatment, conclusion []byte
	var stats []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(&item.ID, &item.ProjectID, &item.Name, &item.Hypothesis, &item.MetricCode, &baseline, &treatment, &item.Status, &conclusion, &stats, &createdAt, &updatedAt); err != nil {
		return Experiment{}, err
	}
	if len(baseline) > 0 {
		value := string(baseline)
		item.BaselineAuditID = &value
	}
	if len(treatment) > 0 {
		value := string(treatment)
		item.TreatmentAuditID = &value
	}
	if len(conclusion) > 0 {
		value := string(conclusion)
		item.Conclusion = &value
	}
	if len(stats) > 0 {
		_ = json.Unmarshal(stats, &item.Stats)
	}
	item.CreatedAt = createdAt
	item.UpdatedAt = updatedAt
	return item, nil
}
