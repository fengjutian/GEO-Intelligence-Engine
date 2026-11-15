package projectmembers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("project member was not found")
var ErrInvalid = errors.New("project member input is invalid")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context, tenantID, projectID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(pm.id),BIN_TO_UUID(pm.user_id),u.email_normalized,COALESCE(pm.role_override,''),pm.created_at
FROM project_memberships pm JOIN users u ON u.id=pm.user_id
WHERE pm.tenant_id=UUID_TO_BIN(?) AND pm.project_id=UUID_TO_BIN(?) ORDER BY u.email_normalized`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Member{}
	for rows.Next() {
		var item Member
		var roleOverride string
		if err := rows.Scan(&item.ID, &item.UserID, &item.Email, &roleOverride, &item.CreatedAt); err != nil {
			return nil, err
		}
		if roleOverride != "" {
			item.RoleOverride = &roleOverride
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Add(ctx context.Context, tenantID, projectID, userID, roleOverride, actorID string) error {
	roleOverride = strings.TrimSpace(roleOverride)
	if roleOverride != "" {
		switch strings.ToLower(roleOverride) {
		case "owner", "project_admin", "analyst", "editor", "approver", "viewer":
		default:
			return ErrInvalid
		}
	}
	var projectExists, userExists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID).Scan(&projectExists); err != nil {
		return err
	}
	if projectExists == 0 {
		return ErrNotFound
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE tenant_id=UUID_TO_BIN(?) AND user_id=UUID_TO_BIN(?) AND status='active'`, tenantID, userID).Scan(&userExists); err != nil {
		return err
	}
	if userExists == 0 {
		return ErrInvalid
	}
	id := uuid.NewString()
	var role any
	if roleOverride != "" {
		role = roleOverride
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO project_memberships (id,tenant_id,project_id,user_id,role_override,permissions_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,NULL,UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE role_override=VALUES(role_override)`, id, tenantID, projectID, userID, role); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"projectId": projectID, "userId": userID, "roleOverride": roleOverride})
	auditID := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs (id,tenant_id,actor_type,actor_id,action,resource_type,resource_id,result,payload_summary,occurred_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'user',UUID_TO_BIN(?),'project_member.added','project',UUID_TO_BIN(?),'succeeded',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		auditID, tenantID, actorID, projectID, payload)
	return err
}

func (s *Store) Remove(ctx context.Context, tenantID, projectID, userID, actorID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM project_memberships WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND user_id=UUID_TO_BIN(?)`, tenantID, projectID, userID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	payload, _ := json.Marshal(map[string]any{"projectId": projectID, "userId": userID})
	auditID := uuid.NewString()
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_logs (id,tenant_id,actor_type,actor_id,action,resource_type,resource_id,result,payload_summary,occurred_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'user',UUID_TO_BIN(?),'project_member.removed','project',UUID_TO_BIN(?),'succeeded',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		auditID, tenantID, actorID, projectID, payload)
	return err
}
