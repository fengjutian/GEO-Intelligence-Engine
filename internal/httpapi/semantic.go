package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/semantic"
)

type semanticStore interface {
	VerifyAudit(ctx context.Context, tenantID, auditID string) (semantic.Run, error)
	ListByAudit(ctx context.Context, tenantID, auditID string) ([]semantic.Check, error)
	ListByProject(ctx context.Context, tenantID, projectID, status string) ([]semantic.Check, error)
}

type semanticHandler struct {
	store       semanticStore
	devTenantID string
	logger      *slog.Logger
}

func newSemanticHandler(store semanticStore, devTenantID string, logger *slog.Logger) *semanticHandler {
	return &semanticHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *semanticHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *semanticHandler) verify(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.VerifyAudit(r.Context(), h.tenant(r), r.PathValue("auditId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (h *semanticHandler) listByAudit(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListByAudit(r.Context(), h.tenant(r), r.PathValue("auditId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *semanticHandler) listByProject(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListByProject(r.Context(), h.tenant(r), r.PathValue("projectId"), strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *semanticHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, semantic.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "AUDIT_NOT_FOUND", "Audit was not found", traceID(r))
	case errors.Is(err, semantic.ErrNoClaims):
		writeProblem(w, http.StatusUnprocessableEntity, "NO_CLAIMS_TO_VERIFY", "Audit answers contain no verifiable claims", traceID(r))
	case errors.Is(err, semantic.ErrNoEvidence):
		writeProblem(w, http.StatusUnprocessableEntity, "NO_EVIDENCE_TO_VERIFY", "No brand pages or third-party mentions are available", traceID(r))
	default:
		h.logger.Error("semantic verification failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
