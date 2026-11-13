package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
)

type modelHandler struct {
	gateway *modelgateway.Gateway
	logger  *slog.Logger
}

func newModelHandler(gateway *modelgateway.Gateway, logger *slog.Logger) *modelHandler {
	return &modelHandler{gateway: gateway, logger: logger}
}

func (h *modelHandler) listProviders(w http.ResponseWriter, _ *http.Request) {
	if h.gateway == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": h.gateway.Statuses()})
}

func (h *modelHandler) health(w http.ResponseWriter, r *http.Request) {
	if h.gateway == nil {
		writeProblem(w, http.StatusServiceUnavailable, "MODEL_GATEWAY_UNAVAILABLE", "Model gateway is unavailable", traceID(r))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := h.gateway.Health(ctx, r.PathValue("provider"))
	if h.writeGatewayError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"provider": r.PathValue("provider"), "status": "healthy"})
}

func (h *modelHandler) chat(w http.ResponseWriter, r *http.Request) {
	if h.gateway == nil {
		writeProblem(w, http.StatusServiceUnavailable, "MODEL_GATEWAY_UNAVAILABLE", "Model gateway is unavailable", traceID(r))
		return
	}
	var input modelgateway.ChatRequest
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if len(input.Messages) == 0 || len(input.Messages) > 100 || input.MaxTokens < 0 || input.MaxTokens > 65536 {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_MODEL_REQUEST", "Messages and token limit are invalid", traceID(r))
		return
	}
	for _, message := range input.Messages {
		if (message.Role != "system" && message.Role != "user" && message.Role != "assistant") || message.Content == "" {
			writeProblem(w, http.StatusUnprocessableEntity, "INVALID_MODEL_REQUEST", "Message role or content is invalid", traceID(r))
			return
		}
	}
	response, err := h.gateway.Chat(r.Context(), input)
	if h.writeGatewayError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *modelHandler) writeGatewayError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, modelgateway.ErrProviderUnsupported):
		writeProblem(w, http.StatusBadRequest, "PROVIDER_UNSUPPORTED", "Model provider is unsupported", traceID(r))
	case errors.Is(err, modelgateway.ErrProviderUnconfigured):
		writeProblem(w, http.StatusConflict, "PROVIDER_UNCONFIGURED", "Model provider API key is not configured", traceID(r))
	default:
		h.logger.Error("model gateway request failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusBadGateway, "PROVIDER_REQUEST_FAILED", "Model provider request failed", traceID(r))
	}
	return true
}
