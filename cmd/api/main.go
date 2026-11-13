package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/config"
	"github.com/fengjutian/geo-intelligence-engine/internal/database"
	"github.com/fengjutian/geo-intelligence-engine/internal/httpapi"
	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
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

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           httpapi.New(httpapi.Dependencies{DB: db, Logger: logger, DevTenantID: cfg.DevTenantID, DevActorID: cfg.DevActorID, WorkerToken: cfg.WorkerToken, ModelGateway: gateway}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("api listening", "address", cfg.HTTPAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}
