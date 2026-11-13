package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"
)

type Dependencies struct {
	DB     *sql.DB
	Logger *slog.Logger
}

func New(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
			var cancel func()
			ctx, cancel = withTimeout(ctx, 2*time.Second)
			defer cancel()
		}
		if err := deps.DB.PingContext(ctx); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Database is unavailable", traceID(r))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"apiVersion": "1.0.0", "baseline": "P0-V1.0"})
	})

	return requestContext(deps.Logger, recoverer(deps.Logger, securityHeaders(mux)))
}
