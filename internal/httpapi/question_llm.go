package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
	"github.com/fengjutian/geo-intelligence-engine/internal/questions"
	"github.com/google/uuid"
)

func (h *questionHandler) generateQuestions(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	var input questions.GenerateInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if strings.EqualFold(strings.TrimSpace(input.Mode), "llm") && h.gateway != nil {
		run, items, err := h.generateWithLLM(r.Context(), projectID, input)
		if err == nil {
			writeJSON(w, http.StatusCreated, map[string]any{"run": run, "items": items, "mode": "llm"})
			return
		}
		h.logger.Warn("llm question generation failed; falling back to templates", "error", err, "trace_id", traceID(r))
		run, items, fallbackErr := h.store.Generate(r.Context(), h.tenantID, projectID, input)
		if fallbackErr != nil {
			h.writeError(w, r, fallbackErr)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"run": run, "items": items, "mode": "template", "warning": err.Error()})
		return
	}
	run, items, err := h.store.Generate(r.Context(), h.tenantID, projectID, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"run": run, "items": items, "mode": "template"})
}

func (h *questionHandler) generateWithLLM(ctx context.Context, projectID string, input questions.GenerateInput) (questions.GenerationRun, []questions.Question, error) {
	generationContext, err := h.store.LoadGenerationContext(ctx, h.tenantID, projectID, input.KnowledgeSnapshotID)
	if err != nil {
		return questions.GenerationRun{}, nil, err
	}
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = "deepseek"
	}
	temperature := 0.2
	response, err := h.gateway.Chat(ctx, modelgateway.ChatRequest{
		Provider: provider,
		Model:    input.Model,
		Messages: []modelgateway.Message{
			{Role: "system", Content: "You are a GEO buyer-question generator. Return only valid JSON."},
			{Role: "user", Content: questions.BuildLLMPrompt(generationContext, input.Language)},
		},
		MaxTokens:   4096,
		Temperature: &temperature,
	})
	if err != nil {
		return questions.GenerationRun{}, nil, err
	}
	h.logger.Info("llm question response", "finish_reason", response.FinishReason, "length", len(response.Content))
	candidates, err := questions.ParseLLMCandidates(response.Content, generationContext.Brand)
	if err != nil {
		return questions.GenerationRun{}, nil, err
	}
	language := strings.TrimSpace(input.Language)
	if language == "" {
		language = "zh-CN"
	}
	return h.store.GenerateFromCandidates(ctx, h.tenantID, projectID, generationContext.SnapshotID, "llm-v1", response.Model, language, candidates)
}
