package modelgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrProviderUnsupported = errors.New("model provider is unsupported")
var ErrProviderUnconfigured = errors.New("model provider is not configured")

type ProviderConfig struct {
	Name           string
	BaseURL        string
	APIKey         string
	Model          string
	MaxField       string
	RPMLimit       int
	MaxConcurrency int
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model,omitempty"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"maxTokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

type ChatResponse struct {
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	ProviderRequestID string `json:"providerRequestId"`
	Content           string `json:"content"`
	ReasoningContent  string `json:"reasoningContent,omitempty"`
	FinishReason      string `json:"finishReason"`
	Usage             Usage  `json:"usage"`
	LatencyMS         int64  `json:"latencyMs"`
}

type ProviderStatus struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
	BaseURL    string `json:"baseUrl"`
	Model      string `json:"defaultModel"`
}

type Gateway struct {
	providers map[string]ProviderConfig
	limiters  map[string]*providerLimiter
	client    *http.Client
}

func New(configs ...ProviderConfig) *Gateway {
	providers := make(map[string]ProviderConfig, len(configs))
	limiters := make(map[string]*providerLimiter, len(configs))
	for _, config := range configs {
		config.Name = strings.ToLower(strings.TrimSpace(config.Name))
		config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
		providers[config.Name] = config
		if limiter := newProviderLimiter(config.RPMLimit, config.MaxConcurrency); limiter != nil {
			limiters[config.Name] = limiter
		}
	}
	return &Gateway{providers: providers, limiters: limiters, client: &http.Client{Timeout: 90 * time.Second}}
}

func (g *Gateway) Statuses() []ProviderStatus {
	order := []string{"deepseek", "minimax"}
	statuses := make([]ProviderStatus, 0, len(order))
	for _, name := range order {
		config, ok := g.providers[name]
		if !ok {
			continue
		}
		statuses = append(statuses, ProviderStatus{Provider: name, Configured: strings.TrimSpace(config.APIKey) != "", BaseURL: config.BaseURL, Model: config.Model})
	}
	return statuses
}

func (g *Gateway) Chat(ctx context.Context, input ChatRequest) (ChatResponse, error) {
	config, ok := g.providers[strings.ToLower(input.Provider)]
	if !ok {
		return ChatResponse{}, ErrProviderUnsupported
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return ChatResponse{}, ErrProviderUnconfigured
	}
	limiter := g.limiters[config.Name]
	if err := limiter.acquire(ctx); err != nil {
		return ChatResponse{}, fmt.Errorf("wait for %s rate limit: %w", config.Name, err)
	}
	defer limiter.release()
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = config.Model
	}
	body := map[string]any{"model": model, "messages": input.Messages, "stream": false}
	if input.MaxTokens > 0 {
		body[config.MaxField] = input.MaxTokens
	}
	if input.Temperature != nil {
		body["temperature"] = *input.Temperature
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("encode provider request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create provider request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+config.APIKey)
	request.Header.Set("Content-Type", "application/json")
	start := time.Now()
	response, err := g.client.Do(request)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("call %s: %w", config.Name, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read provider response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := safeProviderError(data)
		if response.StatusCode == http.StatusTooManyRequests && strings.Contains(message, "2056") {
			return ChatResponse{}, fmt.Errorf("%w: %s", ErrQuotaExceeded, message)
		}
		return ChatResponse{}, &ProviderError{Provider: config.Name, StatusCode: response.StatusCode, RetryAfter: parseRetryAfter(response.Header.Get("Retry-After")), Message: message}
	}
	var result struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		BaseResp *struct {
			StatusCode    int    `json:"status_code"`
			StatusMessage string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return ChatResponse{}, fmt.Errorf("decode provider response: %w", err)
	}
	if result.BaseResp != nil && result.BaseResp.StatusCode != 0 {
		if result.BaseResp.StatusCode == 2056 {
			return ChatResponse{}, fmt.Errorf("%w: %s", ErrQuotaExceeded, result.BaseResp.StatusMessage)
		}
		return ChatResponse{}, fmt.Errorf("provider minimax error %d: %s", result.BaseResp.StatusCode, result.BaseResp.StatusMessage)
	}
	if len(result.Choices) == 0 {
		return ChatResponse{}, errors.New("provider response contains no choices")
	}
	choice := result.Choices[0]
	return ChatResponse{Provider: config.Name, Model: result.Model, ProviderRequestID: result.ID, Content: choice.Message.Content, ReasoningContent: choice.Message.ReasoningContent, FinishReason: choice.FinishReason, Usage: Usage{PromptTokens: result.Usage.PromptTokens, CompletionTokens: result.Usage.CompletionTokens, TotalTokens: result.Usage.TotalTokens}, LatencyMS: time.Since(start).Milliseconds()}, nil
}

func (g *Gateway) Health(ctx context.Context, provider string) error {
	config, ok := g.providers[strings.ToLower(provider)]
	if !ok {
		return ErrProviderUnsupported
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return ErrProviderUnconfigured
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, config.BaseURL+"/models", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+config.APIKey)
	response, err := g.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	return nil
}

func safeProviderError(data []byte) string {
	var value struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
		BaseResp struct {
			StatusCode    int    `json:"status_code"`
			StatusMessage string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if json.Unmarshal(data, &value) == nil {
		if value.Error.Message != "" {
			return value.Error.Message
		}
		if value.BaseResp.StatusMessage != "" {
			return value.BaseResp.StatusMessage
		}
	}
	return "provider request failed"
}
