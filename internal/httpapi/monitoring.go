package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/monitoring"
	"github.com/google/uuid"
)

type monitoringStore interface {
	Create(ctx context.Context, tenantID, projectID, protocolID string, intervalMinutes int) (monitoring.Schedule, error)
	List(ctx context.Context, tenantID, projectID string) ([]monitoring.Schedule, error)
	Update(ctx context.Context, tenantID, scheduleID string, enabled *bool, intervalMinutes *int) (monitoring.Schedule, error)
	Delete(ctx context.Context, tenantID, scheduleID string) error
	Trigger(ctx context.Context, tenantID, scheduleID string) (monitoring.Schedule, error)
	Run(ctx context.Context, tenantID string, schedule monitoring.Schedule) (string, error)
	Alerts(ctx context.Context, tenantID, projectID string) ([]monitoring.Alert, error)
	ResolveAlert(ctx context.Context, tenantID, alertID string) error
}

type monitoringHandler struct {
	store       monitoringStore
	devTenantID string
}

func newMonitoringHandler(store monitoringStore, devTenantID string) *monitoringHandler {
	return &monitoringHandler{store: store, devTenantID: devTenantID}
}

func (h *monitoringHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *monitoringHandler) list(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	items, err := h.store.List(r.Context(), h.tenant(r), projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *monitoringHandler) create(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	var input struct {
		ProtocolID      string `json:"protocolId"`
		IntervalMinutes int    `json:"intervalMinutes"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.ProtocolID); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_PROTOCOL_ID", "Protocol ID is invalid", traceID(r))
		return
	}
	schedule, err := h.store.Create(r.Context(), h.tenant(r), projectID, input.ProtocolID, input.IntervalMinutes)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, schedule)
}

func (h *monitoringHandler) update(w http.ResponseWriter, r *http.Request) {
	scheduleID := r.PathValue("scheduleId")
	var input struct {
		Enabled         *bool `json:"enabled"`
		IntervalMinutes *int  `json:"intervalMinutes"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	schedule, err := h.store.Update(r.Context(), h.tenant(r), scheduleID, input.Enabled, input.IntervalMinutes)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, schedule)
}

func (h *monitoringHandler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(r.Context(), h.tenant(r), r.PathValue("scheduleId")); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *monitoringHandler) runNow(w http.ResponseWriter, r *http.Request) {
	schedule, err := h.store.Trigger(r.Context(), h.tenant(r), r.PathValue("scheduleId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	auditID, err := h.store.Run(r.Context(), h.tenant(r), schedule)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"auditId": auditID})
}

func (h *monitoringHandler) alerts(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.Alerts(r.Context(), h.tenant(r), r.PathValue("projectId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *monitoringHandler) resolveAlert(w http.ResponseWriter, r *http.Request) {
	if err := h.store.ResolveAlert(r.Context(), h.tenant(r), r.PathValue("alertId")); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *monitoringHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, monitoring.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "MONITORING_NOT_FOUND", "Monitoring resource was not found", traceID(r))
	case errors.Is(err, monitoring.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "MONITORING_INVALID", "Protocol must be published and interval at least 60 minutes", traceID(r))
	default:
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
