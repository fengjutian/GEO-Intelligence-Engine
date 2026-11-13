package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/fengjutian/geo-intelligence-engine/internal/config"
	"github.com/fengjutian/geo-intelligence-engine/internal/database"
	"github.com/fengjutian/geo-intelligence-engine/internal/judge"
	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
	"github.com/google/uuid"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	gateway := modelgateway.New(
		modelgateway.ProviderConfig{Name: "deepseek", BaseURL: cfg.DeepSeekBaseURL, APIKey: cfg.DeepSeekAPIKey, Model: cfg.DeepSeekModel, MaxField: "max_tokens", RPMLimit: cfg.ModelRPM, MaxConcurrency: cfg.ModelConcurrency},
		modelgateway.ProviderConfig{Name: "minimax", BaseURL: cfg.MiniMaxBaseURL, APIKey: cfg.MiniMaxAPIKey, Model: cfg.MiniMaxModel, MaxField: "max_completion_tokens", RPMLimit: cfg.ModelRPM, MaxConcurrency: cfg.ModelConcurrency},
	)
	store := audits.NewStore(db)
	judgeClient := judge.New(cfg.JudgeURL)
	workerID := os.Getenv("GEO_WORKER_ID")
	if workerID == "" {
		workerID = "model-worker-" + uuid.NewString()
	}

	address := os.Getenv("GEO_MODEL_WORKER_ADDRESS")
	if address == "" {
		address = ":8083"
	}
	server := &http.Server{Addr: address, ReadHeaderTimeout: 5 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"model-worker"}`))
	})
	server.Handler = mux

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		logger.Info("model worker listening", "address", address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("health server failed", "error", err)
			stop()
		}
	}()

	configured := map[string]bool{}
	for _, status := range gateway.Statuses() {
		configured[status.Provider] = status.Configured
	}
	anyConfigured := false
	for _, value := range configured {
		anyConfigured = anyConfigured || value
	}
	if !anyConfigured {
		logger.Warn("no model provider is configured; audit samples will not be claimed until keys are set")
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
			return
		case <-ticker.C:
			if !anyConfigured {
				continue
			}
			processOne(ctx, store, gateway, judgeClient, logger, workerID, configured)
		}
	}
}

func processOne(ctx context.Context, store *audits.Store, gateway *modelgateway.Gateway, judgeClient *judge.Client, logger *slog.Logger, workerID string, configured map[string]bool) {
	claim, err := store.ClaimSample(ctx, workerID)
	if errors.Is(err, audits.ErrNoWork) {
		return
	}
	if err != nil {
		logger.Error("claim audit sample failed", "error", err)
		return
	}
	if !configured[claim.Provider] {
		logger.Warn("provider is not configured for sample", "sample", claim.SampleID, "provider", claim.Provider)
		if err := store.FailSample(ctx, audits.SampleFailure{SampleID: claim.SampleID, AuditID: claim.AuditID, ErrorCode: "PROVIDER_UNCONFIGURED"}); err != nil {
			logger.Error("release sample failed", "error", err)
		}
		return
	}
	messages := buildMessages(claim)
	temperature := 0.2
	response, err := callWithRetry(ctx, gateway, modelgateway.ChatRequest{Provider: claim.Provider, Model: claim.ModelSelector, Messages: messages, MaxTokens: 1200, Temperature: &temperature}, logger)
	if err != nil {
		code := "MODEL_CALL_FAILED"
		if errors.Is(err, modelgateway.ErrProviderUnconfigured) {
			code = "PROVIDER_UNCONFIGURED"
		}
		logger.Error("model call failed", "sample", claim.SampleID, "provider", claim.Provider, "error", err)
		if failErr := store.FailSample(ctx, audits.SampleFailure{SampleID: claim.SampleID, AuditID: claim.AuditID, ErrorCode: code}); failErr != nil {
			logger.Error("record sample failure failed", "error", failErr)
		}
		return
	}
	requestPayload, _ := json.Marshal(messages)
	requestHash := sha256.Sum256(requestPayload)
	rawHash := sha256.Sum256([]byte(response.Content))
	var evaluation *audits.Evaluation
	judgeResult, judgeErr := judgeClient.Evaluate(ctx, judge.Input{
		Question:        claim.QuestionText,
		Answer:          response.Content,
		BrandName:       claim.BrandName,
		BrandDomain:     claim.BrandDomain,
		BrandAliases:    claim.BrandAliases,
		Language:        claim.Language,
		TransportStatus: "succeeded",
		FinishReason:    response.FinishReason,
	})
	if judgeErr != nil {
		logger.Warn("judge evaluation failed; answer stays ineligible", "sample", claim.SampleID, "error", judgeErr)
	} else {
		mentions := make([]audits.EvaluationMention, 0, len(judgeResult.Mentions))
		for _, mention := range judgeResult.Mentions {
			mentions = append(mentions, audits.EvaluationMention{Text: mention.Text, Start: mention.Start, End: mention.End})
		}
		citations := make([]audits.EvaluationCitation, 0, len(judgeResult.Citations))
		for _, citation := range judgeResult.Citations {
			citations = append(citations, audits.EvaluationCitation{RawURL: citation.RawURL, NormalizedURL: citation.NormalizedURL, Domain: citation.Domain, SourceType: citation.SourceType, Confidence: citation.Confidence})
		}
		evaluation = &audits.Evaluation{
			ContentValidity:        judgeResult.ContentValidity,
			AnswerOutcome:          judgeResult.AnswerOutcome,
			RecommendationLabel:    judgeResult.RecommendationLabel,
			Sentiment:              judgeResult.Sentiment,
			Confidence:             judgeResult.Confidence,
			Eligible:               judgeResult.EligibleForCoreMetrics,
			JudgeVersion:           judgeResult.JudgeVersion,
			EligibilityRuleVersion: judgeResult.EligibilityRuleVersion,
			Mentions:               mentions,
			Citations:              citations,
		}
	}
	err = store.CompleteSample(ctx, audits.SampleResult{
		SampleID:          claim.SampleID,
		AuditID:           claim.AuditID,
		Provider:          response.Provider,
		ResolvedModel:     response.Model,
		ProviderRequestID: response.ProviderRequestID,
		Content:           response.Content,
		PromptTokens:      response.Usage.PromptTokens,
		CompletionTokens:  response.Usage.CompletionTokens,
		TotalTokens:       response.Usage.TotalTokens,
		FinishReason:      response.FinishReason,
		LatencyMS:         response.LatencyMS,
		CostMicrounits:    claim.CostMicrounits,
		Currency:          claim.Currency,
		RequestHash:       hex.EncodeToString(requestHash[:]),
		RawHash:           hex.EncodeToString(rawHash[:]),
		Evaluation:        evaluation,
	})
	if err != nil {
		logger.Error("complete audit sample failed", "sample", claim.SampleID, "error", err)
		return
	}
	logger.Info("audit sample completed", "sample", claim.SampleID, "audit", claim.AuditID, "provider", claim.Provider, "tokens", response.Usage.TotalTokens, "latency_ms", response.LatencyMS)
}

func callWithRetry(ctx context.Context, gateway *modelgateway.Gateway, request modelgateway.ChatRequest, logger *slog.Logger) (modelgateway.ChatResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		response, err := gateway.Chat(ctx, request)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !modelgateway.Retryable(err) || attempt == 3 {
			break
		}
		delay := time.Duration(250*(1<<(attempt-1))) * time.Millisecond
		var providerErr *modelgateway.ProviderError
		if errors.As(err, &providerErr) && providerErr.RetryAfter > 0 {
			delay = providerErr.RetryAfter
		}
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
		logger.Warn("retrying model call", "provider", request.Provider, "attempt", attempt, "delay_ms", delay.Milliseconds(), "error", err)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return modelgateway.ChatResponse{}, ctx.Err()
		}
	}
	return modelgateway.ChatResponse{}, lastErr
}

func buildMessages(claim audits.SampleClaim) []modelgateway.Message {
	brand := claim.BrandName
	if brand == "" {
		brand = "the target brand"
	}
	system := "You are a helpful assistant answering buyer questions. "
	if claim.BrandDomain != "" {
		system += "The brand is " + brand + " (" + claim.BrandDomain + "). "
	} else {
		system += "The brand is " + brand + ". "
	}
	system += "Answer in " + claim.Language + " for the " + claim.Region + " market. Be specific and concise about tools, capabilities and evidence; do not invent citations."
	return []modelgateway.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: claim.QuestionText},
	}
}
