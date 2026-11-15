package questions

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrClusterNotFound = errors.New("question cluster was not found")
var ErrClusterConflict = errors.New("question cluster operation conflicts with current membership")

// SetClusterLocked updates the manual lock and optional representative.
func (s *Store) SetClusterLocked(ctx context.Context, tenantID, projectID, clusterID string, locked bool, representativeID string) (Cluster, error) {
	versionID, err := clusterVersion(ctx, s.db, tenantID, projectID, clusterID)
	if err != nil {
		return Cluster{}, err
	}
	if representativeID != "" {
		var member int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_members WHERE cluster_id=UUID_TO_BIN(?) AND question_id=UUID_TO_BIN(?)`, clusterID, representativeID).Scan(&member); err != nil {
			return Cluster{}, err
		}
		if member == 0 {
			return Cluster{}, ErrClusterConflict
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE query_clusters SET locked=?, representative_question_id=COALESCE(NULLIF(?,''),representative_question_id)
WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND question_version_id=UUID_TO_BIN(?)`, locked, representativeID, tenantID, clusterID, versionID); err != nil {
		return Cluster{}, err
	}
	return s.getCluster(ctx, tenantID, projectID, clusterID)
}

// MoveMember moves a question to another cluster and locks the manual assignment.
func (s *Store) MoveMember(ctx context.Context, tenantID, projectID, targetClusterID, questionID string) error {
	versionID, err := clusterVersion(ctx, s.db, tenantID, projectID, targetClusterID)
	if err != nil {
		return err
	}
	var sourceClusterID string
	err = s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(cm.cluster_id) FROM cluster_members cm
JOIN query_clusters qc ON qc.id=cm.cluster_id
WHERE cm.question_id=UUID_TO_BIN(?) AND qc.tenant_id=UUID_TO_BIN(?) AND qc.question_version_id=UUID_TO_BIN(?)`, questionID, tenantID, versionID).Scan(&sourceClusterID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrClusterNotFound
	}
	if err != nil {
		return err
	}
	if sourceClusterID == targetClusterID {
		return ErrClusterConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE cluster_members SET cluster_id=UUID_TO_BIN(?),manually_locked=TRUE WHERE cluster_id=UUID_TO_BIN(?) AND question_id=UUID_TO_BIN(?)`, targetClusterID, sourceClusterID, questionID); err != nil {
		if strings.Contains(err.Error(), "1062") {
			return ErrClusterConflict
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE query_clusters SET locked=TRUE WHERE id=UUID_TO_BIN(?)`, targetClusterID); err != nil {
		return err
	}
	return tx.Commit()
}

// SplitCluster creates a locked cluster from selected members of a source cluster.
func (s *Store) SplitCluster(ctx context.Context, tenantID, projectID, sourceClusterID string, questionIDs []string, name string) (Cluster, error) {
	if len(questionIDs) == 0 {
		return Cluster{}, ErrClusterConflict
	}
	versionID, err := clusterVersion(ctx, s.db, tenantID, projectID, sourceClusterID)
	if err != nil {
		return Cluster{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = "人工拆分"
	}
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:120])
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Cluster{}, err
	}
	defer tx.Rollback()
	clusterID := uuid.NewString()
	var sourceName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM query_clusters WHERE id=UUID_TO_BIN(?)`, sourceClusterID).Scan(&sourceName); err != nil {
		return Cluster{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO query_clusters (id,tenant_id,question_version_id,name,representative_question_id,algorithm_version,locked)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,NULL,?,TRUE)`, clusterID, tenantID, versionID, name, ClusterAlgorithm); err != nil {
		return Cluster{}, err
	}
	moved := 0
	for _, questionID := range questionIDs {
		result, err := tx.ExecContext(ctx, `UPDATE cluster_members SET cluster_id=UUID_TO_BIN(?),manually_locked=TRUE,similarity=1
WHERE cluster_id=UUID_TO_BIN(?) AND question_id=UUID_TO_BIN(?)`, clusterID, sourceClusterID, questionID)
		if err != nil {
			return Cluster{}, err
		}
		affected, _ := result.RowsAffected()
		if affected > 0 {
			moved++
			if moved == 1 {
				_, _ = tx.ExecContext(ctx, `UPDATE query_clusters SET representative_question_id=UUID_TO_BIN(?) WHERE id=UUID_TO_BIN(?)`, questionID, clusterID)
			}
		}
	}
	if moved == 0 {
		return Cluster{}, ErrClusterConflict
	}
	if err := tx.Commit(); err != nil {
		return Cluster{}, err
	}
	return s.getCluster(ctx, tenantID, projectID, clusterID)
}

// MergeClusters moves all members from source to target and removes source.
func (s *Store) MergeClusters(ctx context.Context, tenantID, projectID, sourceClusterID, targetClusterID string) (Cluster, error) {
	versionID, err := clusterVersion(ctx, s.db, tenantID, projectID, sourceClusterID)
	if err != nil {
		return Cluster{}, err
	}
	if _, err := clusterVersion(ctx, s.db, tenantID, projectID, targetClusterID); err != nil {
		return Cluster{}, err
	}
	if sourceClusterID == targetClusterID {
		return Cluster{}, ErrClusterConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Cluster{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE IGNORE cluster_members SET cluster_id=UUID_TO_BIN(?),manually_locked=TRUE WHERE cluster_id=UUID_TO_BIN(?)`, targetClusterID, sourceClusterID); err != nil {
		return Cluster{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cluster_members WHERE cluster_id=UUID_TO_BIN(?)`, sourceClusterID); err != nil {
		return Cluster{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM query_clusters WHERE id=UUID_TO_BIN(?) AND question_version_id=UUID_TO_BIN(?)`, sourceClusterID, versionID); err != nil {
		return Cluster{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE query_clusters SET locked=TRUE WHERE id=UUID_TO_BIN(?)`, targetClusterID); err != nil {
		return Cluster{}, err
	}
	if err := tx.Commit(); err != nil {
		return Cluster{}, err
	}
	return s.getCluster(ctx, tenantID, projectID, targetClusterID)
}

func clusterVersion(ctx context.Context, db *sql.DB, tenantID, projectID, clusterID string) (string, error) {
	var versionID string
	err := db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(qc.question_version_id) FROM query_clusters qc
JOIN question_set_versions qsv ON qsv.id=qc.question_version_id JOIN question_sets qs ON qs.id=qsv.question_set_id
WHERE qc.tenant_id=UUID_TO_BIN(?) AND qs.project_id=UUID_TO_BIN(?) AND qc.id=UUID_TO_BIN(?) AND qsv.status='draft'`, tenantID, projectID, clusterID).Scan(&versionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrClusterNotFound
	}
	return versionID, err
}

func (s *Store) getCluster(ctx context.Context, tenantID, projectID, clusterID string) (Cluster, error) {
	clusters, err := s.ListClusters(ctx, tenantID, projectID)
	if err != nil {
		return Cluster{}, err
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			return cluster, nil
		}
	}
	return Cluster{}, ErrClusterNotFound
}
