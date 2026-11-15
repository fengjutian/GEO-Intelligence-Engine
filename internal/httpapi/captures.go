package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/captures"
	"github.com/google/uuid"
)

type captureStore interface {
	Submit(ctx context.Context, tenantID string, batch captures.BatchRequest, raw []byte) (captures.BatchResult, error)
	ListByAudit(ctx context.Context, tenantID, auditID string) ([]captures.Capture, error)
	ListByBatch(ctx context.Context, tenantID, batchID string) ([]captures.Capture, error)
	Revoke(ctx context.Context, tenantID, captureID, reason string) error
}

type captureHandler struct {
	store       captureStore
	devTenantID string
	logger      *slog.Logger
}

func newCaptureHandler(store captureStore, devTenantID string, logger *slog.Logger) *captureHandler {
	return &captureHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *captureHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *captureHandler) submit(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("X-Capture-Protocol")) != captures.ProtocolVersion {
		writeProblem(w, http.StatusBadRequest, "CAPTURE_PROTOCOL_UNSUPPORTED", "X-Capture-Protocol must be 1", traceID(r))
		return
	}
	var input captures.BatchRequest
	if !decodeJSONBody(w, r, &input) {
		return
	}
	result, err := h.store.Submit(r.Context(), h.tenant(r), input, input.Captures)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *captureHandler) listBatch(w http.ResponseWriter, r *http.Request) {
	batchID := r.PathValue("batchId")
	if _, err := uuid.Parse(batchID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_BATCH_ID", "Batch ID is invalid", traceID(r))
		return
	}
	items, err := h.store.ListByBatch(r.Context(), h.tenant(r), batchID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *captureHandler) listByAudit(w http.ResponseWriter, r *http.Request) {
	auditID := r.PathValue("auditId")
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	items, err := h.store.ListByAudit(r.Context(), h.tenant(r), auditID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *captureHandler) revoke(w http.ResponseWriter, r *http.Request) {
	captureID := r.PathValue("captureId")
	if _, err := uuid.Parse(captureID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_CAPTURE_ID", "Capture ID is invalid", traceID(r))
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if err := h.store.Revoke(r.Context(), h.tenant(r), captureID, input.Reason); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *captureHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *captures.ValidationError
	switch {
	case errors.As(err, &validation):
		writeProblem(w, http.StatusUnprocessableEntity, validation.Code, validation.Message, traceID(r))
	case errors.Is(err, captures.ErrBatchInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "BATCH_INVALID", "Capture batch is invalid", traceID(r))
	default:
		h.logger.Error("capture operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
