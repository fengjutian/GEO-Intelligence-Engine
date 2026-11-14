package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/tasks"
	"github.com/google/uuid"
)

type taskStore interface {
	CreateFromFinding(ctx context.Context, tenantID, actorID, findingID string, input tasks.CreateInput) (tasks.Task, error)
	Transition(ctx context.Context, tenantID, actorID, taskID string, expectedVersion int64, input tasks.TransitionInput) (tasks.Task, error)
	Get(ctx context.Context, tenantID, taskID string) (tasks.Task, error)
	List(ctx context.Context, tenantID, projectID, status string) ([]tasks.Task, error)
	UpdateAcceptance(ctx context.Context, tenantID, actorID, taskID, itemID, status string, expectedVersion int64) (tasks.Task, error)
	Update(ctx context.Context, tenantID, actorID, taskID string, expectedVersion int64, input tasks.UpdateInput) (tasks.Task, error)
	AddComment(ctx context.Context, tenantID, actorID, taskID, body string) (tasks.Comment, error)
}

type taskHandler struct {
	store       taskStore
	devTenantID string
	devActorID  string
	logger      *slog.Logger
}

func newTaskHandler(store taskStore, devTenantID, devActorID string, logger *slog.Logger) *taskHandler {
	return &taskHandler{store: store, devTenantID: devTenantID, devActorID: devActorID, logger: logger}
}

func (h *taskHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *taskHandler) actor(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Actor-ID")); value != "" {
		return value
	}
	return h.devActorID
}

func (h *taskHandler) createFromFinding(w http.ResponseWriter, r *http.Request) {
	findingID := r.PathValue("findingId")
	if _, err := uuid.Parse(findingID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_FINDING_ID", "Finding ID is invalid", traceID(r))
		return
	}
	var input tasks.CreateInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	task, err := h.store.CreateFromFinding(r.Context(), h.tenant(r), h.actor(r), findingID, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (h *taskHandler) transition(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("taskAction")
	separator := strings.LastIndexByte(raw, ':')
	if separator < 1 || raw[separator+1:] != "transition" {
		writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route was not found", traceID(r))
		return
	}
	taskID := raw[:separator]
	if _, err := uuid.Parse(taskID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_TASK_ID", "Task ID is invalid", traceID(r))
		return
	}
	var version int64
	if value := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "INVALID_VERSION", "If-Match version is invalid", traceID(r))
			return
		}
		version = parsed
	}
	var input tasks.TransitionInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if (input.TargetState == "done" || input.TargetState == "changes_requested") && !hasPermission(r, "approve_task") {
		deny(w, r, "approve_task")
		return
	}
	task, err := h.store.Transition(r.Context(), h.tenant(r), h.actor(r), taskID, version, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *taskHandler) update(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if _, err := uuid.Parse(taskID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_TASK_ID", "Task ID is invalid", traceID(r))
		return
	}
	var version int64
	if value := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "INVALID_VERSION", "If-Match version is invalid", traceID(r))
			return
		}
		version = parsed
	}
	var input tasks.UpdateInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	task, err := h.store.Update(r.Context(), h.tenant(r), h.actor(r), taskID, version, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *taskHandler) updateAcceptance(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	itemID := r.PathValue("itemId")
	if _, err := uuid.Parse(taskID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_TASK_ID", "Task ID is invalid", traceID(r))
		return
	}
	if _, err := uuid.Parse(itemID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ACCEPTANCE_ID", "Acceptance item ID is invalid", traceID(r))
		return
	}
	var version int64
	if value := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "INVALID_VERSION", "If-Match version is invalid", traceID(r))
			return
		}
		version = parsed
	}
	var input tasks.AcceptanceUpdate
	if !decodeJSONBody(w, r, &input) {
		return
	}
	task, err := h.store.UpdateAcceptance(r.Context(), h.tenant(r), h.actor(r), taskID, itemID, input.Status, version)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *taskHandler) addComment(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if _, err := uuid.Parse(taskID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_TASK_ID", "Task ID is invalid", traceID(r))
		return
	}
	var input struct {
		Body string `json:"body"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	comment, err := h.store.AddComment(r.Context(), h.tenant(r), h.actor(r), taskID, input.Body)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (h *taskHandler) listEvents(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if _, err := uuid.Parse(taskID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_TASK_ID", "Task ID is invalid", traceID(r))
		return
	}
	task, err := h.store.Get(r.Context(), h.tenant(r), taskID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": task.Events})
}

func (h *taskHandler) get(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if _, err := uuid.Parse(taskID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_TASK_ID", "Task ID is invalid", traceID(r))
		return
	}
	task, err := h.store.Get(r.Context(), h.tenant(r), taskID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *taskHandler) list(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
	if projectID != "" {
		if _, err := uuid.Parse(projectID); err != nil {
			writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
			return
		}
	}
	items, err := h.store.List(r.Context(), h.tenant(r), projectID, strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *taskHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "TASK_NOT_FOUND", "Task resource was not found", traceID(r))
	case errors.Is(err, tasks.ErrConflict):
		writeProblem(w, http.StatusConflict, "TASK_VERSION_CONFLICT", "Task was modified concurrently", traceID(r))
	case errors.Is(err, tasks.ErrInvalidTransition):
		writeProblem(w, http.StatusConflict, "TASK_INVALID_TRANSITION", "Transition is not allowed from the current state", traceID(r))
	case errors.Is(err, tasks.ErrInvalidInput):
		writeProblem(w, http.StatusUnprocessableEntity, "TASK_INPUT_INVALID", "Task input is invalid", traceID(r))
	default:
		h.logger.Error("task operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
