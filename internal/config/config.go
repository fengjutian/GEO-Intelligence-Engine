package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddress      string
	DatabaseDSN      string
	DevTenantID      string
	DevActorID       string
	WorkerToken      string
	DeepSeekAPIKey   string
	DeepSeekBaseURL  string
	DeepSeekModel    string
	MiniMaxAPIKey    string
	MiniMaxBaseURL   string
	MiniMaxModel     string
	ModelRPM         int
	ModelConcurrency int
	JudgeURL         string
	ReportDir        string
	AuthMode         string
	AuthSecret       string
	DevRole          string
	LogLevel         slog.Level
}

func FromEnv() (Config, error) {
	cfg := Config{
		HTTPAddress:      envOr("GEO_HTTP_ADDRESS", ":8080"),
		DatabaseDSN:      strings.TrimSpace(os.Getenv("GEO_DATABASE_DSN")),
		DevTenantID:      envOr("GEO_DEV_TENANT_ID", "019a7f64-7c2c-7000-8000-000000000001"),
		DevActorID:       envOr("GEO_DEV_ACTOR_ID", "019a7f64-7c2c-7000-8000-000000000002"),
		WorkerToken:      envOr("GEO_INTERNAL_WORKER_TOKEN", "local-worker-token-change-me"),
		DeepSeekAPIKey:   strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
		DeepSeekBaseURL:  envOr("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekModel:    envOr("DEEPSEEK_MODEL", "deepseek-flash"),
		MiniMaxAPIKey:    strings.TrimSpace(os.Getenv("MINIMAX_API_KEY")),
		MiniMaxBaseURL:   envOr("MINIMAX_BASE_URL", "https://api.minimaxi.com/v1"),
		MiniMaxModel:     envOr("MINIMAX_MODEL", "MiniMax-M2.5"),
		ModelRPM:         envInt("GEO_MODEL_RPM", 60),
		ModelConcurrency: envInt("GEO_MODEL_MAX_CONCURRENCY", 4),
		JudgeURL:         envOr("GEO_JUDGE_URL", "http://127.0.0.1:8081"),
		ReportDir:        envOr("GEO_REPORT_DIR", ".run/reports"),
		AuthMode:         envOr("GEO_AUTH_MODE", "dev"),
		AuthSecret:       strings.TrimSpace(os.Getenv("GEO_AUTH_SECRET")),
		DevRole:          envOr("GEO_DEV_ROLE", "owner"),
		LogLevel:         slog.LevelInfo,
	}
	if cfg.DatabaseDSN == "" {
		return Config{}, fmt.Errorf("GEO_DATABASE_DSN is required")
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(envOr("GEO_LOG_LEVEL", "INFO"))); err != nil {
		return Config{}, fmt.Errorf("GEO_LOG_LEVEL: %w", err)
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
