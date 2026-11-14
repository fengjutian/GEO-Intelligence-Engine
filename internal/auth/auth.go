package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid token")

// Principal is the authenticated caller mapped to a tenant role.
type Principal struct {
	TenantID string
	UserID   string
	Role     string
	Subject  string
}

// Claims are the JWT fields trusted by the API.
type Claims struct {
	Subject  string `json:"sub"`
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	Expires  int64  `json:"exp"`
}

// ParseHS256 validates a compact HS256 JWT and returns its claims.
func ParseHS256(token, secret string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || secret == "" {
		return Claims{}, ErrInvalidToken
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil || !strings.EqualFold(header.Algorithm, "HS256") {
		return Claims{}, ErrInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if claims.Expires > 0 && time.Now().Unix() >= claims.Expires {
		return Claims{}, ErrInvalidToken
	}
	if strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(claims.Subject) == "" {
		return Claims{}, ErrInvalidToken
	}
	claims.Role = NormalizeRole(claims.Role)
	return claims, nil
}

// NormalizeRole maps aliases to the P0 role set.
func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "org_admin":
		return "owner"
	case "project_admin", "admin":
		return "project_admin"
	case "analyst":
		return "analyst"
	case "editor":
		return "editor"
	case "approver", "reviewer":
		return "approver"
	case "viewer", "readonly":
		return "viewer"
	case "service_account":
		return "service_account"
	default:
		return "viewer"
	}
}

// Can reports whether a role may perform an action. The matrix is intentionally small
// for P0 and mirrors docs/security/rbac-matrix.md.
func Can(role, action string) bool {
	role = NormalizeRole(role)
	switch action {
	case "read":
		return true
	case "edit_project", "run_audit", "manage_questions", "manage_crawl":
		return role == "owner" || role == "project_admin" || role == "analyst"
	case "edit_task", "comment_task":
		return role != "viewer"
	case "approve_task":
		return role == "owner" || role == "project_admin" || role == "approver"
	case "manage_members", "manage_keys":
		return role == "owner" || role == "project_admin"
	case "export_report":
		return role != "viewer"
	default:
		return role == "owner" || role == "project_admin"
	}
}
