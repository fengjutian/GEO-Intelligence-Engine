package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/google/uuid"
)

type questionTrendStore interface {
	QuestionTrends(ctx context.Context, tenantID, projectID string, limit int) ([]audits.QuestionTrend, error)
}

type questionTrendHandler struct {
	store       questionTrendStore
	devTenantID string
}

func newQuestionTrendHandler(store questionTrendStore, devTenantID string) *questionTrendHandler {
	return &questionTrendHandler{store: store, devTenantID: devTenantID}
}

func (h *questionTrendHandler) list(w http.ResponseWriter, r *http.Request) {
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
	items, err := h.store.QuestionTrends(r.Context(), tenantID, projectID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
