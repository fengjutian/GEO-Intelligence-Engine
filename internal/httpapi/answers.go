package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/google/uuid"
)

type answerStore interface {
	ListAnswers(ctx context.Context, tenantID, auditID string, limit int) ([]audits.Answer, error)
}

type answerHandler struct {
	store       answerStore
	devTenantID string
}

func newAnswerHandler(store answerStore, devTenantID string) *answerHandler {
	return &answerHandler{store: store, devTenantID: devTenantID}
}

func (h *answerHandler) list(w http.ResponseWriter, r *http.Request) {
	auditID := r.PathValue("auditId")
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	tenantID := r.Header.Get(headerTenant)
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	items, err := h.store.ListAnswers(r.Context(), tenantID, auditID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
