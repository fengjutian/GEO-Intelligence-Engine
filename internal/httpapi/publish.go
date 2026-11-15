package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/publish"
)

type publishStore interface {
	SetTarget(ctx context.Context, tenantID, projectID, kind, endpoint string, enabled bool) (publish.Target, error)
	GetTarget(ctx context.Context, tenantID, projectID string) (publish.Target, error)
	Publish(ctx context.Context, tenantID, projectID, draftID string) (publish.Event, error)
	Rollback(ctx context.Context, tenantID, projectID, eventID, reason string) (publish.Event, error)
	ListEvents(ctx context.Context, tenantID, projectID string) ([]publish.Event, error)
}

type publishHandler struct {
	store       publishStore
	devTenantID string
	logger      *slog.Logger
}

func newPublishHandler(store publishStore, devTenantID string, logger *slog.Logger) *publishHandler {
	return &publishHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *publishHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *publishHandler) setTarget(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Kind     string `json:"kind"`
		Endpoint string `json:"endpoint"`
		Enabled  *bool  `json:"enabled"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = publish.KindWebhook
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if kind == publish.KindWebhook && !isPublicHTTPURL(input.Endpoint) {
		writeProblem(w, http.StatusUnprocessableEntity, "PUBLISH_TARGET_INVALID", "endpoint must be a public http(s) URL", traceID(r))
		return
	}
	target, err := h.store.SetTarget(r.Context(), h.tenant(r), r.PathValue("projectId"), kind, input.Endpoint, enabled)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, target)
}

func (h *publishHandler) getTarget(w http.ResponseWriter, r *http.Request) {
	target, err := h.store.GetTarget(r.Context(), h.tenant(r), r.PathValue("projectId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, target)
}

func (h *publishHandler) publishDraft(w http.ResponseWriter, r *http.Request) {
	event, err := h.store.Publish(r.Context(), h.tenant(r), r.PathValue("projectId"), r.PathValue("draftId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	status := http.StatusOK
	if event.Status != "delivered" {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, event)
}

func (h *publishHandler) rollback(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reason string `json:"reason"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	event, err := h.store.Rollback(r.Context(), h.tenant(r), r.PathValue("projectId"), r.PathValue("eventId"), input.Reason)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	status := http.StatusOK
	if event.Status != "delivered" {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, event)
}

func (h *publishHandler) listEvents(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListEvents(r.Context(), h.tenant(r), r.PathValue("projectId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *publishHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var targetErr *publish.TargetError
	switch {
	case errors.As(err, &targetErr):
		writeProblem(w, http.StatusUnprocessableEntity, "PUBLISH_TARGET_INVALID", targetErr.Message, traceID(r))
	case errors.Is(err, publish.ErrTargetMissing):
		writeProblem(w, http.StatusNotFound, "PUBLISH_TARGET_MISSING", "Project has no enabled publish target", traceID(r))
	case errors.Is(err, publish.ErrNotApproved):
		writeProblem(w, http.StatusConflict, "DRAFT_NOT_APPROVED", "Draft must be approved before publishing", traceID(r))
	case errors.Is(err, publish.ErrEventNotFound):
		writeProblem(w, http.StatusNotFound, "PUBLISH_EVENT_NOT_FOUND", "Publish event was not found", traceID(r))
	case errors.Is(err, publish.ErrNotDelivered):
		writeProblem(w, http.StatusConflict, "PUBLISH_NOT_DELIVERED", "Only delivered publishes can be rolled back", traceID(r))
	case strings.Contains(err.Error(), "GEO_PUBLISH_SECRET"):
		writeProblem(w, http.StatusUnprocessableEntity, "PUBLISH_NOT_CONFIGURED", "Publish secret is not configured", traceID(r))
	default:
		h.logger.Error("publish operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
