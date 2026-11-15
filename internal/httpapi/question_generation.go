package httpapi

import (
	"errors"
	"net/http"
	"strconv"

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
	algorithm := r.URL.Query().Get("algorithm")
	warning := ""
	var vectors map[string][]float64
	if algorithm == questions.ClusterAlgorithmEmbedding {
		if h.embeddings == nil || !h.embeddings.Enabled() {
			algorithm = questions.ClusterAlgorithmSemantic
			warning = "embedding service is not configured; fell back to semantic-v2"
		} else {
			texts, err := h.store.DraftQuestionTexts(r.Context(), h.tenantID, projectID)
			if err != nil {
				h.writeError(w, r, err)
				return
			}
			ids := make([]string, 0, len(texts))
			inputs := make([]string, 0, len(texts))
			for id, text := range texts {
				ids = append(ids, id)
				inputs = append(inputs, text)
			}
			embedded, err := h.embeddings.Embed(r.Context(), inputs)
			if err != nil {
				algorithm = questions.ClusterAlgorithmSemantic
				warning = "embedding request failed; fell back to semantic-v2"
			} else {
				vectors = make(map[string][]float64, len(ids))
				for index, id := range ids {
					vectors[id] = embedded[index]
				}
			}
		}
	}
	threshold := 0.0
	if raw := r.URL.Query().Get("threshold"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			threshold = parsed
		}
	}
	items, err := h.store.RebuildClusters(r.Context(), h.tenantID, projectID, algorithm, vectors, threshold)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	effectiveThreshold := threshold
	if effectiveThreshold == 0 {
		effectiveThreshold = 0.3
		if algorithm == questions.ClusterAlgorithmEmbedding {
			effectiveThreshold = 0.75
		}
	}
	version := algorithm
	if version == "" {
		version = questions.ClusterAlgorithm
	}
	response := map[string]any{"items": items, "algorithmVersion": version, "threshold": effectiveThreshold}
	if warning != "" {
		response["warning"] = warning
	}
	writeJSON(w, http.StatusOK, response)
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

func (h *questionHandler) updateCluster(w http.ResponseWriter, r *http.Request) {
	projectID, clusterID, ok := h.clusterPath(w, r)
	if !ok {
		return
	}
	var input struct {
		Locked                   bool   `json:"locked"`
		RepresentativeQuestionID string `json:"representativeQuestionId"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	cluster, err := h.store.SetClusterLocked(r.Context(), h.tenantID, projectID, clusterID, input.Locked, input.RepresentativeQuestionID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (h *questionHandler) moveClusterMember(w http.ResponseWriter, r *http.Request) {
	projectID, clusterID, ok := h.clusterPath(w, r)
	if !ok {
		return
	}
	var input struct {
		QuestionID string `json:"questionId"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.QuestionID); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_QUESTION_ID", "Question ID is invalid", traceID(r))
		return
	}
	if err := h.store.MoveMember(r.Context(), h.tenantID, projectID, clusterID, input.QuestionID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *questionHandler) splitCluster(w http.ResponseWriter, r *http.Request) {
	projectID, clusterID, ok := h.clusterPath(w, r)
	if !ok {
		return
	}
	var input struct {
		QuestionIDs []string `json:"questionIds"`
		Name        string   `json:"name"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	cluster, err := h.store.SplitCluster(r.Context(), h.tenantID, projectID, clusterID, input.QuestionIDs, input.Name)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, cluster)
}

func (h *questionHandler) mergeClusters(w http.ResponseWriter, r *http.Request) {
	projectID, clusterID, ok := h.clusterPath(w, r)
	if !ok {
		return
	}
	var input struct {
		TargetClusterID string `json:"targetClusterId"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.TargetClusterID); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_CLUSTER_ID", "Target cluster ID is invalid", traceID(r))
		return
	}
	cluster, err := h.store.MergeClusters(r.Context(), h.tenantID, projectID, clusterID, input.TargetClusterID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (h *questionHandler) clusterPath(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	projectID := r.PathValue("projectId")
	clusterID := r.PathValue("clusterId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return "", "", false
	}
	if _, err := uuid.Parse(clusterID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_CLUSTER_ID", "Cluster ID is invalid", traceID(r))
		return "", "", false
	}
	return projectID, clusterID, true
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
	case errors.Is(err, questions.ErrClusterNotFound):
		writeProblem(w, http.StatusNotFound, "QUESTION_CLUSTER_NOT_FOUND", "Question cluster was not found", traceID(r))
	case errors.Is(err, questions.ErrClusterConflict):
		writeProblem(w, http.StatusConflict, "QUESTION_CLUSTER_CONFLICT", "Question cluster operation conflicts with current membership", traceID(r))
	default:
		h.logger.Error("question operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
