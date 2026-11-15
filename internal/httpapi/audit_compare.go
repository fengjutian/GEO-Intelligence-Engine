package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/google/uuid"
)

type auditCompareStore interface {
	CompareAudits(ctx context.Context, tenantID, baseAuditID, targetAuditID string) (audits.AuditComparison, error)
}

type auditCompareHandler struct {
	store       auditCompareStore
	devTenantID string
}

func newAuditCompareHandler(store auditCompareStore, devTenantID string) *auditCompareHandler {
	return &auditCompareHandler{store: store, devTenantID: devTenantID}
}

func (h *auditCompareHandler) get(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("auditId")
	baseID := r.URL.Query().Get("baseAuditId")
	if _, err := uuid.Parse(targetID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	if _, err := uuid.Parse(baseID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_BASE_AUDIT_ID", "Base audit ID is invalid", traceID(r))
		return
	}
	tenantID := r.Header.Get(headerTenant)
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	comparison, err := h.store.CompareAudits(r.Context(), tenantID, baseID, targetID)
	if err != nil {
		switch {
		case errors.Is(err, audits.ErrAuditNotFound):
			writeProblem(w, http.StatusNotFound, "AUDIT_NOT_FOUND", "Audit was not found", traceID(r))
		case errors.Is(err, audits.ErrAuditComparisonInvalid):
			writeProblem(w, http.StatusUnprocessableEntity, "AUDIT_COMPARISON_INVALID", "Audits belong to different projects", traceID(r))
		default:
			writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		}
		return
	}
	writeJSON(w, http.StatusOK, comparison)
}
