package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/monitoring"
	"github.com/google/uuid"
)

type signalAlertStore interface {
	EvaluateSignals(ctx context.Context, tenantID, projectID string) ([]monitoring.SignalAlert, error)
	ListSignalAlerts(ctx context.Context, tenantID, projectID, status string) ([]monitoring.SignalAlert, error)
	ResolveSignalAlert(ctx context.Context, tenantID, alertID string) (monitoring.SignalAlert, error)
}

type signalAlertHandler struct {
	store       signalAlertStore
	devTenantID string
	logger      *slog.Logger
}

func newSignalAlertHandler(store signalAlertStore, devTenantID string, logger *slog.Logger) *signalAlertHandler {
	return &signalAlertHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *signalAlertHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *signalAlertHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListSignalAlerts(r.Context(), h.tenant(r), r.PathValue("projectId"), strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *signalAlertHandler) evaluate(w http.ResponseWriter, r *http.Request) {
	created, err := h.store.EvaluateSignals(r.Context(), h.tenant(r), r.PathValue("projectId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": len(created), "items": created})
}

func (h *signalAlertHandler) resolve(w http.ResponseWriter, r *http.Request) {
	alertID := r.PathValue("alertId")
	if _, err := uuid.Parse(alertID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ALERT_ID", "Alert ID is invalid", traceID(r))
		return
	}
	item, err := h.store.ResolveSignalAlert(r.Context(), h.tenant(r), alertID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *signalAlertHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, monitoring.ErrAlertNotFound) {
		writeProblem(w, http.StatusNotFound, "SIGNAL_ALERT_NOT_FOUND", "Signal alert was not found", traceID(r))
		return
	}
	h.logger.Error("signal alert operation failed", "error", err, "trace_id", traceID(r))
	writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
}
