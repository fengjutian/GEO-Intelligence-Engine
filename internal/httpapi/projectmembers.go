package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/projectmembers"
	"github.com/google/uuid"
)

type projectMemberStore interface {
	List(ctx context.Context, tenantID, projectID string) ([]projectmembers.Member, error)
	Add(ctx context.Context, tenantID, projectID, userID, roleOverride, actorID string) error
	Remove(ctx context.Context, tenantID, projectID, userID, actorID string) error
}

type projectMemberHandler struct {
	store       projectMemberStore
	devTenantID string
}

func newProjectMemberHandler(store projectMemberStore, devTenantID string) *projectMemberHandler {
	return &projectMemberHandler{store: store, devTenantID: devTenantID}
}

func (h *projectMemberHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *projectMemberHandler) list(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	items, err := h.store.List(r.Context(), h.tenant(r), projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *projectMemberHandler) add(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	var input struct {
		UserID       string `json:"userId"`
		RoleOverride string `json:"roleOverride"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.UserID); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_USER_ID", "User ID is invalid", traceID(r))
		return
	}
	if err := h.store.Add(r.Context(), h.tenant(r), projectID, input.UserID, input.RoleOverride, r.Header.Get(headerActor)); err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"projectId": projectID, "userId": input.UserID})
}

func (h *projectMemberHandler) remove(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	userID := r.PathValue("userId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	if _, err := uuid.Parse(userID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_USER_ID", "User ID is invalid", traceID(r))
		return
	}
	if err := h.store.Remove(r.Context(), h.tenant(r), projectID, userID, r.Header.Get(headerActor)); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *projectMemberHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, projectmembers.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "PROJECT_MEMBER_NOT_FOUND", "Project member was not found", traceID(r))
	case errors.Is(err, projectmembers.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "PROJECT_MEMBER_INVALID", "Project member input is invalid", traceID(r))
	default:
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
