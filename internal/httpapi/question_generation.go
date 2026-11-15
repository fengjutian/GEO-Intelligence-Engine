package httpapi

import (
	"errors"
	"net/http"

	"github.com/fengjutian/geo-intelligence-engine/internal/questions"
	"github.com/google/uuid"
)

func (h *questionHandler) generate(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	var input questions.GenerateInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	run, items, err := h.store.Generate(r.Context(), h.tenantID, projectID, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"run": run, "items": items})
}

func (h *questionHandler) importQuestions(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	var input struct {
		CSV    string `json:"csv"`
		Commit bool   `json:"commit"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	result, err := h.store.Import(r.Context(), h.tenantID, projectID, input.CSV, input.Commit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *questionHandler) rebuildClusters(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	items, err := h.store.RebuildClusters(r.Context(), h.tenantID, projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "algorithmVersion": questions.ClusterAlgorithm})
}

func (h *questionHandler) listClusters(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	items, err := h.store.ListClusters(r.Context(), h.tenantID, projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *questionHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, questions.ErrProjectNotFound):
		writeProblem(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project was not found", traceID(r))
	case errors.Is(err, questions.ErrProjectArchived):
		writeProblem(w, http.StatusConflict, "PROJECT_ARCHIVED", "Project is archived", traceID(r))
	case errors.Is(err, questions.ErrDuplicate):
		writeProblem(w, http.StatusConflict, "QUESTION_DUPLICATE", "Question already exists", traceID(r))
	case errors.Is(err, questions.ErrNoKnowledgeSnapshot):
		writeProblem(w, http.StatusUnprocessableEntity, "KNOWLEDGE_SNAPSHOT_REQUIRED", "Publish a knowledge snapshot before generating questions", traceID(r))
	case errors.Is(err, questions.ErrInvalidImport):
		writeProblem(w, http.StatusUnprocessableEntity, "QUESTION_IMPORT_INVALID", "CSV header or content is invalid", traceID(r))
	default:
		h.logger.Error("question operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
