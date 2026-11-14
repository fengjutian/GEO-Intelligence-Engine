package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/auth"
	"github.com/google/uuid"
)

const (
	headerTenant = "X-Tenant-ID"
	headerActor  = "X-Actor-ID"
	headerRole   = "X-Role"
)

type authOptions struct {
	Mode      string
	Secret    string
	DevRole   string
	DevTenant string
	DevActor  string
	OIDC      *auth.OIDCVerifier
	DB        *sql.DB
	Logger    *slog.Logger
}

var errNotMember = errors.New("principal is not an active tenant member")

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

// newAuthMiddleware validates bearer tokens and injects trusted tenant/actor/role
// headers. In jwt/oidc modes it also verifies active tenant membership and uses the
// database role as the authorization source of truth.
func newAuthMiddleware(options authOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/healthz" || path == "/readyz" || path == "/api/v1/version" || strings.HasPrefix(path, "/internal/") {
				next.ServeHTTP(w, r)
				return
			}
			recorder := &statusRecorder{ResponseWriter: w}
			auditRequest := r
			defer func() {
				if recorder.status == http.StatusUnauthorized || recorder.status == http.StatusForbidden {
					logDenied(options, auditRequest, recorder.status)
				}
			}()
			mode := strings.ToLower(options.Mode)
			if mode == "jwt" || mode == "oidc" {
				token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
				var claims auth.Claims
				var err error
				if mode == "oidc" {
					if options.OIDC == nil {
						writeProblem(recorder, http.StatusUnauthorized, "AUTHENTICATION_UNAVAILABLE", "OIDC verifier is not configured", traceID(r))
						return
					}
					claims, err = options.OIDC.Verify(r.Context(), token)
				} else {
					claims, err = auth.ParseHS256(token, options.Secret)
				}
				if err != nil {
					writeProblem(recorder, http.StatusUnauthorized, "UNAUTHENTICATED", "Valid bearer token is required", traceID(r))
					return
				}
				authenticatedRequest := r.Clone(r.Context())
				authenticatedRequest.Header.Set(headerTenant, claims.TenantID)
				authenticatedRequest.Header.Set(headerActor, claims.Subject)
				auditRequest = authenticatedRequest
				principal, err := resolvePrincipal(r.Context(), options.DB, claims)
				if err != nil {
					writeProblem(recorder, http.StatusForbidden, "NOT_A_TENANT_MEMBER", "Principal is not an active tenant member", traceID(r))
					return
				}
				clone := r.Clone(r.Context())
				clone.Header.Set(headerTenant, principal.TenantID)
				clone.Header.Set(headerActor, principal.UserID)
				clone.Header.Set(headerRole, principal.Role)
				auditRequest = clone
				next.ServeHTTP(recorder, clone)
				return
			}
			clone := r.Clone(r.Context())
			if clone.Header.Get(headerTenant) == "" && options.DevTenant != "" {
				clone.Header.Set(headerTenant, options.DevTenant)
			}
			if clone.Header.Get(headerActor) == "" && options.DevActor != "" {
				clone.Header.Set(headerActor, options.DevActor)
			}
			if clone.Header.Get(headerRole) == "" {
				clone.Header.Set(headerRole, options.DevRole)
			}
			auditRequest = clone
			next.ServeHTTP(recorder, clone)
		})
	}
}

func resolvePrincipal(ctx context.Context, db *sql.DB, claims auth.Claims) (auth.Principal, error) {
	if db == nil {
		return auth.Principal{}, errNotMember
	}
	userID := strings.TrimSpace(claims.Subject)
	if _, err := uuid.Parse(userID); err != nil {
		if err := db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM users WHERE oidc_subject=? AND status='active'`, userID).Scan(&userID); err != nil {
			return auth.Principal{}, errNotMember
		}
	}
	var role, status string
	err := db.QueryRowContext(ctx, `SELECT role,status FROM memberships WHERE tenant_id=UUID_TO_BIN(?) AND user_id=UUID_TO_BIN(?)`, claims.TenantID, userID).Scan(&role, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Principal{}, errNotMember
	}
	if err != nil {
		return auth.Principal{}, err
	}
	if status != "active" {
		return auth.Principal{}, errNotMember
	}
	return auth.Principal{TenantID: claims.TenantID, UserID: userID, Role: auth.NormalizeRole(role), Subject: claims.Subject}, nil
}

func logDenied(options authOptions, r *http.Request, status int) {
	if options.DB == nil {
		return
	}
	tenantID := r.Header.Get(headerTenant)
	actorID := r.Header.Get(headerActor)
	if tenantID == "" {
		return
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{"method": r.Method, "path": r.URL.Path, "status": status})
	auditID, _ := uuid.NewV7()
	trace := strings.ReplaceAll(traceID(r), "-", "")
	_, err := options.DB.ExecContext(r.Context(), `INSERT INTO audit_logs (id,tenant_id,actor_type,actor_id,action,resource_type,result,ip_hash,trace_id,payload_summary,occurred_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'user',UUID_TO_BIN(?),'auth.denied','http','denied',NULL,?,CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		auditID.String(), tenantID, actorID, trace, payload)
	if err != nil && options.Logger != nil {
		options.Logger.Warn("write security audit log failed", "error", err)
	}
}

func meHandler(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get(headerRole)
	if role == "" {
		role = "owner"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenantId":      r.Header.Get(headerTenant),
		"userId":        r.Header.Get(headerActor),
		"role":          role,
		"authenticated": true,
	})
}

func hasPermission(r *http.Request, action string) bool {
	role := r.Header.Get(headerRole)
	if role == "" {
		role = "owner"
	}
	return auth.Can(role, action)
}

func deny(w http.ResponseWriter, r *http.Request, action string) {
	writeProblem(w, http.StatusForbidden, "FORBIDDEN", "Role is not allowed to perform "+action, traceID(r))
}

// requirePermission wraps a handler with the role check for one action.
func requirePermission(action string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !hasPermission(r, action) {
			deny(w, r, action)
			return
		}
		next(w, r)
	}
}
