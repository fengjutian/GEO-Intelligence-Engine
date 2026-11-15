package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/google/uuid"
)

type shareOfVoiceStore interface {
	ShareOfVoice(ctx context.Context, tenantID, auditID string) (audits.ShareOfVoice, error)
}

type shareOfVoiceHandler struct {
	store       shareOfVoiceStore
	devTenantID string
}

func newShareOfVoiceHandler(store shareOfVoiceStore, devTenantID string) *shareOfVoiceHandler {
	return &shareOfVoiceHandler{store: store, devTenantID: devTenantID}
}

func (h *shareOfVoiceHandler) get(w http.ResponseWriter, r *http.Request) {
	auditID := r.PathValue("auditId")
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	tenantID := r.Header.Get(headerTenant)
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	result, err := h.store.ShareOfVoice(r.Context(), tenantID, auditID)
	if err != nil {
		if errors.Is(err, audits.ErrAuditNotFound) {
			writeProblem(w, http.StatusNotFound, "AUDIT_NOT_FOUND", "Audit was not found", traceID(r))
			return
		}
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		return
	}
	writeJSON(w, http.StatusOK, result)
}
