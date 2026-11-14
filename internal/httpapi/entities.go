package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/entities"
	"github.com/google/uuid"
)

type entityStore interface {
	List(ctx context.Context, tenantID, projectID, entityType, status string) ([]entities.Entity, error)
	Get(ctx context.Context, tenantID, entityID string) (entities.Detail, error)
	UpdateStatus(ctx context.Context, tenantID, entityID, status string) (entities.Entity, error)
	Merge(ctx context.Context, tenantID, actorID string, input entities.MergeInput) (entities.MergeEvent, error)
	UndoMerge(ctx context.Context, tenantID, mergeID string) (entities.MergeEvent, error)
}

type entityHandler struct {
	store       entityStore
	devTenantID string
	logger      *slog.Logger
}

func newEntityHandler(store entityStore, devTenantID string, logger *slog.Logger) *entityHandler {
	return &entityHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *entityHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *entityHandler) list(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	items, err := h.store.List(r.Context(), h.tenant(r), projectID, strings.TrimSpace(r.URL.Query().Get("type")), strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *entityHandler) get(w http.ResponseWriter, r *http.Request) {
	entityID := r.PathValue("entityId")
	if _, err := uuid.Parse(entityID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ENTITY_ID", "Entity ID is invalid", traceID(r))
		return
	}
	detail, err := h.store.Get(r.Context(), h.tenant(r), entityID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *entityHandler) update(w http.ResponseWriter, r *http.Request) {
	entityID := r.PathValue("entityId")
	if _, err := uuid.Parse(entityID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ENTITY_ID", "Entity ID is invalid", traceID(r))
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	entity, err := h.store.UpdateStatus(r.Context(), h.tenant(r), entityID, strings.TrimSpace(input.Status))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entity)
}

func (h *entityHandler) merge(w http.ResponseWriter, r *http.Request) {
	var input entities.MergeInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.SourceEntityID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ENTITY_ID", "Source entity ID is invalid", traceID(r))
		return
	}
	if _, err := uuid.Parse(input.TargetEntityID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ENTITY_ID", "Target entity ID is invalid", traceID(r))
		return
	}
	merge, err := h.store.Merge(r.Context(), h.tenant(r), r.Header.Get(headerActor), input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, merge)
}

func (h *entityHandler) undoMerge(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("mergeAction")
	separator := strings.LastIndexByte(raw, ':')
	if separator < 1 || raw[separator+1:] != "undo" {
		writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route was not found", traceID(r))
		return
	}
	mergeID := raw[:separator]
	if _, err := uuid.Parse(mergeID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_MERGE_ID", "Merge ID is invalid", traceID(r))
		return
	}
	merge, err := h.store.UndoMerge(r.Context(), h.tenant(r), mergeID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, merge)
}

func (h *entityHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, entities.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "ENTITY_NOT_FOUND", "Entity was not found", traceID(r))
	case errors.Is(err, entities.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "ENTITY_INPUT_INVALID", "Entity status is invalid", traceID(r))
	default:
		h.logger.Error("entity operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
