package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/entities"
	"github.com/google/uuid"
)

type entityStore interface {
	List(ctx context.Context, tenantID, projectID, entityType, status string) ([]entities.Entity, error)
	Get(ctx context.Context, tenantID, entityID string) (entities.Detail, error)
	UpdateStatus(ctx context.Context, tenantID, entityID, status string) (entities.Entity, error)
}

type entityHandler struct {
	store       entityStore
	devTenantID string
}

func newEntityHandler(store entityStore, devTenantID string) *entityHandler {
	return &entityHandler{store: store, devTenantID: devTenantID}
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

func (h *entityHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, entities.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "ENTITY_NOT_FOUND", "Entity was not found", traceID(r))
	case errors.Is(err, entities.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "ENTITY_INPUT_INVALID", "Entity status is invalid", traceID(r))
	default:
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
