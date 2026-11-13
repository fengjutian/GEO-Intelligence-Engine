package httpapi

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	handler := New(Dependencies{DB: &sql.DB{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	if res.Header().Get("X-Trace-Id") == "" {
		t.Fatal("missing trace id")
	}
}

func TestVersion(t *testing.T) {
	handler := New(Dependencies{DB: &sql.DB{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
}
