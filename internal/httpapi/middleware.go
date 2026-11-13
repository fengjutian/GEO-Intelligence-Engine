package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const traceKey contextKey = "trace_id"

func requestContext(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := uuid.NewString()
		ctx := context.WithValue(r.Context(), traceKey, trace)
		w.Header().Set("X-Trace-Id", trace)
		start := time.Now()
		next.ServeHTTP(w, r.WithContext(ctx))
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "trace_id", trace, "duration_ms", time.Since(start).Milliseconds())
	})
}

func recoverer(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("panic recovered", "error", fmt.Sprint(recovered), "trace_id", traceID(r), "stack", string(debug.Stack()))
				writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func traceID(r *http.Request) string {
	value, _ := r.Context().Value(traceKey).(string)
	return value
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
