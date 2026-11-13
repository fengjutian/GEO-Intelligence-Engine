package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	HTTPAddress string
	DatabaseDSN string
	LogLevel    slog.Level
}

func FromEnv() (Config, error) {
	cfg := Config{
		HTTPAddress: envOr("GEO_HTTP_ADDRESS", ":8080"),
		DatabaseDSN: strings.TrimSpace(os.Getenv("GEO_DATABASE_DSN")),
		LogLevel:    slog.LevelInfo,
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
