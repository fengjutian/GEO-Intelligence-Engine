package config

import "testing"

func TestFromEnvRequiresDatabaseDSN(t *testing.T) {
	t.Setenv("GEO_DATABASE_DSN", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected missing DSN error")
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("GEO_DATABASE_DSN", "user:pass@tcp(localhost:3306)/geo")
	t.Setenv("GEO_HTTP_ADDRESS", "")
	t.Setenv("GEO_LOG_LEVEL", "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.HTTPAddress != ":8080" {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
}
