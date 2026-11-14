package serviceaccounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("service account resource was not found")
var ErrInvalid = errors.New("service account input is invalid")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateAccount(ctx context.Context, tenantID, name string) (Account, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return Account{}, ErrInvalid
	}
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO service_accounts (id,tenant_id,name,status,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),?,'active',UTC_TIMESTAMP(6))`, id, tenantID, name); err != nil {
		if strings.Contains(err.Error(), "1062") {
			return Account{}, ErrInvalid
		}
		return Account{}, err
	}
	return s.getAccount(ctx, tenantID, id)
}

func (s *Store) ListAccounts(ctx context.Context, tenantID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),name,status,created_at FROM service_accounts WHERE tenant_id=UUID_TO_BIN(?) ORDER BY name,id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Account{}
	for rows.Next() {
		var item Account
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) getAccount(ctx context.Context, tenantID, id string) (Account, error) {
	var item Account
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),name,status,created_at FROM service_accounts WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, id).Scan(&item.ID, &item.Name, &item.Status, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return item, err
}

func (s *Store) CreateToken(ctx context.Context, tenantID, accountID string, scopes []string, expiresInDays int) (Token, error) {
	if len(scopes) == 0 {
		return Token{}, ErrInvalid
	}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "" {
			return Token{}, ErrInvalid
		}
	}
	if _, err := s.getAccount(ctx, tenantID, accountID); err != nil {
		return Token{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Token{}, err
	}
	plaintext := "geo_" + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(plaintext))
	scopesJSON, _ := json.Marshal(scopes)
	id := uuid.NewString()
	var expires any
	if expiresInDays > 0 {
		expires = time.Now().UTC().Add(time.Duration(expiresInDays) * 24 * time.Hour)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (id,tenant_id,principal_type,principal_id,token_hash,scopes,expires_at,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'service_account',UUID_TO_BIN(?),?,?,?,UTC_TIMESTAMP(6))`, id, tenantID, accountID, hash[:], scopesJSON, expires); err != nil {
		return Token{}, err
	}
	token, err := s.getToken(ctx, tenantID, id)
	if err != nil {
		return Token{}, err
	}
	token.Token = plaintext
	return token, nil
}

func (s *Store) ListTokens(ctx context.Context, tenantID string, accountID string) ([]Token, error) {
	query := `SELECT BIN_TO_UUID(id),principal_type,BIN_TO_UUID(principal_id),scopes,expires_at,last_used_at,revoked_at,created_at FROM api_tokens WHERE tenant_id=UUID_TO_BIN(?)`
	args := []any{tenantID}
	if accountID != "" {
		query += " AND principal_id=UUID_TO_BIN(?)"
		args = append(args, accountID)
	}
	query += " ORDER BY created_at DESC,id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Token{}
	for rows.Next() {
		var item Token
		var scopes []byte
		if err := rows.Scan(&item.ID, &item.PrincipalType, &item.PrincipalID, &scopes, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(scopes, &item.Scopes)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RevokeToken(ctx context.Context, tenantID, tokenID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET revoked_at=UTC_TIMESTAMP(6) WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND revoked_at IS NULL`, tenantID, tokenID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) getToken(ctx context.Context, tenantID, id string) (Token, error) {
	var item Token
	var scopes []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),principal_type,BIN_TO_UUID(principal_id),scopes,expires_at,last_used_at,revoked_at,created_at FROM api_tokens WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, id).Scan(&item.ID, &item.PrincipalType, &item.PrincipalID, &scopes, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	_ = json.Unmarshal(scopes, &item.Scopes)
	return item, err
}

// Authenticate resolves a plaintext API token to its tenant, principal and scopes.
func (s *Store) Authenticate(ctx context.Context, plaintext string) (tenantID, principalID string, scopes []string, err error) {
	hash := sha256.Sum256([]byte(strings.TrimSpace(plaintext)))
	var scopesJSON []byte
	var expiresAt, revokedAt sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(tenant_id),BIN_TO_UUID(principal_id),scopes,expires_at,revoked_at
FROM api_tokens WHERE token_hash=?`, hash[:]).Scan(&tenantID, &principalID, &scopesJSON, &expiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil, ErrNotFound
	}
	if err != nil {
		return "", "", nil, err
	}
	_ = json.Unmarshal(scopesJSON, &scopes)
	if revokedAt.Valid || (expiresAt.Valid && time.Now().After(expiresAt.Time)) {
		return "", "", nil, ErrNotFound
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at=UTC_TIMESTAMP(6) WHERE token_hash=?`, hash[:])
	return tenantID, principalID, scopes, nil
}

// RoleForScopes maps token scopes to the closest P0 role.
func RoleForScopes(scopes []string) string {
	role := "viewer"
	for _, scope := range scopes {
		switch strings.ToLower(strings.TrimSpace(scope)) {
		case "admin", "manage":
			return "project_admin"
		case "write", "run_audit", "manage_crawl":
			role = "analyst"
		}
	}
	return role
}
