package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/embeddings"
	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
	"github.com/fengjutian/geo-intelligence-engine/internal/questions"
	"github.com/google/uuid"
)

type questionStore interface {
	Create(context.Context, string, string, questions.CreateInput) (questions.Question, error)
	List(context.Context, string, string, int) (questions.Page, error)
	Generate(context.Context, string, string, questions.GenerateInput) (questions.GenerationRun, []questions.Question, error)
	LoadGenerationContext(context.Context, string, string, string) (questions.GenerationContext, error)
	GenerateFromCandidates(context.Context, string, string, string, string, string, string, []questions.Candidate) (questions.GenerationRun, []questions.Question, error)
	Import(context.Context, string, string, string, bool) (questions.ImportResult, error)
	RebuildClusters(context.Context, string, string, string, map[string][]float64, float64) ([]questions.Cluster, error)
	DraftQuestionTexts(context.Context, string, string) (map[string]string, error)
	ListClusters(context.Context, string, string) ([]questions.Cluster, error)
	SetClusterLocked(context.Context, string, string, string, bool, string) (questions.Cluster, error)
	MoveMember(context.Context, string, string, string, string) error
	SplitCluster(context.Context, string, string, string, []string, string) (questions.Cluster, error)
	MergeClusters(context.Context, string, string, string, string) (questions.Cluster, error)
}

type questionHandler struct {
	store      questionStore
	tenantID   string
	logger     *slog.Logger
	gateway    *modelgateway.Gateway
	embeddings *embeddings.Client
}

func newQuestionHandler(store questionStore, tenantID string, logger *slog.Logger, gateway *modelgateway.Gateway, embeddingClient *embeddings.Client) *questionHandler {
	return &questionHandler{store: store, tenantID: tenantID, logger: logger, gateway: gateway, embeddings: embeddingClient}
}

func (h *questionHandler) list(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, 400, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(w, 400, "INVALID_LIMIT", "Limit must be between 1 and 100", traceID(r))
			return
		}
		limit = parsed
	}
	page, err := h.store.List(r.Context(), h.tenantID, projectID, limit)
	if err != nil {
		h.logger.Error("list questions", "error", err, "traceId", traceID(r))
		writeProblem(w, 500, "INTERNAL_ERROR", "Questions could not be loaded", traceID(r))
		return
	}
	writeJSON(w, 200, page)
}

func (h *questionHandler) create(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, 400, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input questions.CreateInput
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, 400, "INVALID_JSON", "Request body is invalid", traceID(r))
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	input.Language = strings.TrimSpace(input.Language)
	input.QuestionType = strings.TrimSpace(input.QuestionType)
	input.Intent = strings.TrimSpace(input.Intent)
	if input.Weight == nil {
		defaultWeight := 1.0
		input.Weight = &defaultWeight
	}
	if err := validateQuestion(input); err != "" {
		writeProblem(w, 422, "VALIDATION_FAILED", err, traceID(r))
		return
	}
	question, err := h.store.Create(r.Context(), h.tenantID, projectID, input)
	switch {
	case errors.Is(err, questions.ErrProjectNotFound):
		writeProblem(w, 404, "PROJECT_NOT_FOUND", "Project was not found", traceID(r))
		return
	case errors.Is(err, questions.ErrProjectArchived):
		writeProblem(w, 409, "PROJECT_ARCHIVED", "Archived projects cannot be changed", traceID(r))
		return
	case errors.Is(err, questions.ErrDuplicate):
		writeProblem(w, 409, "QUESTION_DUPLICATE", "The same question already exists in the draft", traceID(r))
		return
	case err != nil:
		h.logger.Error("create question", "error", err, "traceId", traceID(r))
		writeProblem(w, 500, "INTERNAL_ERROR", "Question could not be created", traceID(r))
		return
	}
	w.Header().Set("Location", "/api/v1/questions/"+question.ID)
	writeJSON(w, 201, question)
}

func validateQuestion(input questions.CreateInput) string {
	if input.Text == "" || len([]rune(input.Text)) > 2000 {
		return "Question text is required and must not exceed 2000 characters"
	}
	if input.Language == "" || len(input.Language) > 35 {
		return "Language is required and must not exceed 35 characters"
	}
	valid := map[string]bool{"brand": true, "category": true, "comparison": true, "scenario": true, "decision": true}
	if !valid[input.QuestionType] {
		return "Question type is invalid"
	}
	if input.Weight == nil || *input.Weight < 0 || *input.Weight > 1000 {
		return "Weight must be between 0 and 1000"
	}
	if len(input.Intent) > 64 {
		return "Intent must not exceed 64 characters"
	}
	return ""
}
