package audits

import (
	"context"
	"database/sql"
	"errors"
)

// LatestShareOfVoice returns share data for the newest finished audit in a project.
func (s *Store) LatestShareOfVoice(ctx context.Context, tenantID, projectID string) (ShareOfVoice, error) {
	var auditID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(a.id) FROM audits a
JOIN metric_snapshots ms ON ms.audit_id=a.id
JOIN metric_values mv ON mv.snapshot_id=ms.id AND mv.metric_code='share_of_voice'
WHERE a.tenant_id=UUID_TO_BIN(?) AND a.project_id=UUID_TO_BIN(?) AND a.finished_at IS NOT NULL
GROUP BY a.id,a.finished_at ORDER BY a.finished_at DESC,a.id DESC LIMIT 1`, tenantID, projectID).Scan(&auditID)
	if errors.Is(err, sql.ErrNoRows) {
		return ShareOfVoice{}, ErrNotFound
	}
	if err != nil {
		return ShareOfVoice{}, err
	}
	return s.ShareOfVoice(ctx, tenantID, auditID)
}
