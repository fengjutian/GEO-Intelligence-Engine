package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/google/uuid"
)

type auditTrendStore interface {
	AuditTrends(ctx context.Context, tenantID, projectID string, limit int) (audits.AuditTrend, error)
	AuditEngineTrends(ctx context.Context, tenantID, projectID string, limit int) (audits.EngineTrend, error)
}

type auditTrendHandler struct {
	store       auditTrendStore
	devTenantID string
}

func newAuditTrendHandler(store auditTrendStore, devTenantID string) *auditTrendHandler {
	return &auditTrendHandler{store: store, devTenantID: devTenantID}
}

func (h *auditTrendHandler) get(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	limit := 12
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	tenantID := r.Header.Get(headerTenant)
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	if r.URL.Query().Get("dimension") == "engine" {
		trend, err := h.store.AuditEngineTrends(r.Context(), tenantID, projectID, limit)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
			return
		}
		writeJSON(w, http.StatusOK, trend)
		return
	}
	trend, err := h.store.AuditTrends(r.Context(), tenantID, projectID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		return
	}
	writeJSON(w, http.StatusOK, trend)
}
