package httpapi

import (
	"context"
	"net/http"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/google/uuid"
)

type citationStore interface {
	Citations(ctx context.Context, tenantID, auditID string) (audits.AuditCitations, error)
}

type citationHandler struct {
	store       citationStore
	devTenantID string
}

func newCitationHandler(store citationStore, devTenantID string) *citationHandler {
	return &citationHandler{store: store, devTenantID: devTenantID}
}

func (h *citationHandler) get(w http.ResponseWriter, r *http.Request) {
	auditID := r.PathValue("auditId")
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	tenantID := r.Header.Get(headerTenant)
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	result, err := h.store.Citations(r.Context(), tenantID, auditID)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		return
	}
	writeJSON(w, http.StatusOK, result)
}
