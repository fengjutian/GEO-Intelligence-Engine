package members

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/auth"
	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("member resource not found")
	ErrConflict  = errors.New("member state conflict")
	ErrInvalid   = errors.New("member input is invalid")
	ErrForbidden = errors.New("member action is forbidden")
)

var validRoles = map[string]bool{"owner": true, "project_admin": true, "analyst": true, "editor": true, "approver": true, "viewer": true}

func validRoleInput(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "owner", "project_admin", "admin", "analyst", "editor", "approver", "viewer":
		return true
	default:
		return false
	}
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context, tenantID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(m.id),BIN_TO_UUID(u.id),u.email_normalized,u.oidc_subject,m.role,m.status,m.created_at
FROM memberships m JOIN users u ON u.id=m.user_id
WHERE m.tenant_id=UUID_TO_BIN(?) ORDER BY m.created_at,m.id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Member{}
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.ID, &member.UserID, &member.Email, &member.Subject, &member.Role, &member.Status, &member.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, member)
	}
	return items, rows.Err()
}

func (s *Store) Invite(ctx context.Context, tenantID, actorID string, input InviteInput) (Invitation, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if !strings.Contains(email, "@") || len(email) > 320 {
		return Invitation{}, ErrInvalid
	}
	if !validRoleInput(input.Role) {
		return Invitation{}, ErrInvalid
	}
	role := auth.NormalizeRole(input.Role)
	hours := input.ExpiresInHours
	if hours <= 0 {
		hours = 168
	}
	if hours > 720 {
		hours = 720
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Invitation{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(token))
	id, _ := uuid.NewV7()
	expires := time.Now().UTC().Add(time.Duration(hours) * time.Hour)
	_, err := s.db.ExecContext(ctx, `INSERT INTO invitations (id,tenant_id,email_normalized,role,token_hash,status,expires_at,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,'pending',?,UTC_TIMESTAMP(6))`,
		id.String(), tenantID, email, role, tokenHash[:], expires)
	if err != nil {
		return Invitation{}, fmt.Errorf("insert invitation: %w", err)
	}
	s.audit(ctx, tenantID, actorID, "member.invited", id.String(), map[string]any{"email": email, "role": role})
	return Invitation{ID: id.String(), Email: email, Role: roleLabel(role), Status: "pending", ExpiresAt: expires, Token: token}, nil
}

func (s *Store) Update(ctx context.Context, tenantID, actorID, memberID string, input UpdateInput) (Member, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Member{}, err
	}
	defer tx.Rollback()
	var userID, currentRole, currentStatus string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(user_id),role,status FROM memberships WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, memberID).Scan(&userID, &currentRole, &currentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, err
	}
	role := currentRole
	status := currentStatus
	if strings.TrimSpace(input.Role) != "" {
		if !validRoleInput(input.Role) {
			return Member{}, ErrInvalid
		}
		role = auth.NormalizeRole(input.Role)
	}
	if strings.TrimSpace(input.Status) != "" {
		status = strings.ToLower(strings.TrimSpace(input.Status))
		if status != "active" && status != "disabled" {
			return Member{}, ErrInvalid
		}
	}
	if auth.NormalizeRole(currentRole) == "owner" && (auth.NormalizeRole(role) != "owner" || status != "active") {
		var owners int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE tenant_id=UUID_TO_BIN(?) AND role='Owner' AND status='active'`, tenantID).Scan(&owners); err != nil {
			return Member{}, err
		}
		if owners <= 1 {
			return Member{}, ErrConflict
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE memberships SET role=?,status=?,updated_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, roleLabel(role), status, memberID); err != nil {
		return Member{}, err
	}
	payload, _ := json.Marshal(map[string]any{"before": map[string]string{"role": currentRole, "status": currentStatus}, "after": map[string]string{"role": role, "status": status}})
	if err := s.auditTx(ctx, tx, tenantID, actorID, "member.updated", memberID, payload); err != nil {
		return Member{}, err
	}
	if err := tx.Commit(); err != nil {
		return Member{}, err
	}
	return s.get(ctx, tenantID, memberID, userID)
}

// Accept consumes an invitation token for the authenticated user whose email matches.
func (s *Store) Accept(ctx context.Context, token, actorID string) (Member, error) {
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Member{}, err
	}
	defer tx.Rollback()
	var invitationID, tenantID, email, role, status string
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(tenant_id),email_normalized,role,status,expires_at
FROM invitations WHERE token_hash=? FOR UPDATE`, tokenHash[:]).Scan(&invitationID, &tenantID, &email, &role, &status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, err
	}
	if status != "pending" || time.Now().UTC().After(expiresAt) {
		return Member{}, ErrConflict
	}
	var actorEmail, actorSubject string
	if err := tx.QueryRowContext(ctx, `SELECT email_normalized,oidc_subject FROM users WHERE id=UUID_TO_BIN(?) AND status='active'`, actorID).Scan(&actorEmail, &actorSubject); err != nil {
		return Member{}, ErrForbidden
	}
	if !strings.EqualFold(actorEmail, email) {
		return Member{}, ErrForbidden
	}
	var membershipID string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM memberships WHERE tenant_id=UUID_TO_BIN(?) AND user_id=UUID_TO_BIN(?)`, tenantID, actorID).Scan(&membershipID)
	if errors.Is(err, sql.ErrNoRows) {
		membershipID = uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO memberships (id,tenant_id,user_id,role,status,created_at,updated_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,'active',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, membershipID, tenantID, actorID, roleLabel(role)); err != nil {
			return Member{}, err
		}
	} else if err != nil {
		return Member{}, err
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE memberships SET role=?,status='active',updated_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)`, roleLabel(role), membershipID); err != nil {
			return Member{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invitations SET status='accepted' WHERE id=UUID_TO_BIN(?)`, invitationID); err != nil {
		return Member{}, err
	}
	payload, _ := json.Marshal(map[string]any{"invitationId": invitationID, "email": email, "role": role})
	if err := s.auditTx(ctx, tx, tenantID, actorID, "member.joined", membershipID, payload); err != nil {
		return Member{}, err
	}
	if err := tx.Commit(); err != nil {
		return Member{}, err
	}
	return s.get(ctx, tenantID, membershipID, actorID)
}

func (s *Store) get(ctx context.Context, tenantID, memberID, userID string) (Member, error) {
	var member Member
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(m.id),BIN_TO_UUID(u.id),u.email_normalized,u.oidc_subject,m.role,m.status,m.created_at
FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=UUID_TO_BIN(?) AND m.id=UUID_TO_BIN(?)`, tenantID, memberID).Scan(&member.ID, &member.UserID, &member.Email, &member.Subject, &member.Role, &member.Status, &member.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	return member, err
}

func (s *Store) audit(ctx context.Context, tenantID, actorID, action, resourceID string, payload map[string]any) {
	data, _ := json.Marshal(payload)
	_ = s.auditTx(ctx, s.db, tenantID, actorID, action, resourceID, data)
}

func (s *Store) auditTx(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, tenantID, actorID, action, resourceID string, payload []byte) error {
	auditID, _ := uuid.NewV7()
	_, err := db.ExecContext(ctx, `INSERT INTO audit_logs (id,tenant_id,actor_type,actor_id,action,resource_type,resource_id,result,trace_id,payload_summary,occurred_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'user',UUID_TO_BIN(?),?,'membership',UUID_TO_BIN(?),'succeeded',NULL,CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		auditID.String(), tenantID, actorID, action, resourceID, payload)
	return err
}

func roleLabel(role string) string {
	switch auth.NormalizeRole(role) {
	case "owner":
		return "Owner"
	case "project_admin":
		return "ProjectAdmin"
	case "analyst":
		return "Analyst"
	case "editor":
		return "Editor"
	case "approver":
		return "Approver"
	default:
		return "Viewer"
	}
}
