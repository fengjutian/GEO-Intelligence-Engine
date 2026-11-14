package httpapi

import (
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/auth"
)

const (
	headerTenant = "X-Tenant-ID"
	headerActor  = "X-Actor-ID"
	headerRole   = "X-Role"
)

// newAuthMiddleware validates bearer tokens in jwt mode and injects the trusted
// tenant/actor/role headers consumed by handlers. In dev mode it derives a developer
// principal so local flows keep working without an identity provider.
func newAuthMiddleware(mode, secret, devRole, devTenant, devActor string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/healthz" || path == "/readyz" || path == "/api/v1/version" || strings.HasPrefix(path, "/internal/") {
				next.ServeHTTP(w, r)
				return
			}
			if strings.EqualFold(mode, "jwt") {
				token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
				claims, err := auth.ParseHS256(token, secret)
				if err != nil {
					writeProblem(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Valid bearer token is required", traceID(r))
					return
				}
				clone := r.Clone(r.Context())
				clone.Header.Set(headerTenant, claims.TenantID)
				clone.Header.Set(headerActor, claims.Subject)
				role := claims.Role
				if role == "" {
					role = "viewer"
				}
				clone.Header.Set(headerRole, role)
				next.ServeHTTP(w, clone)
				return
			}
			clone := r.Clone(r.Context())
			if clone.Header.Get(headerTenant) == "" && devTenant != "" {
				clone.Header.Set(headerTenant, devTenant)
			}
			if clone.Header.Get(headerActor) == "" && devActor != "" {
				clone.Header.Set(headerActor, devActor)
			}
			if clone.Header.Get(headerRole) == "" {
				clone.Header.Set(headerRole, devRole)
			}
			next.ServeHTTP(w, clone)
		})
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
