package httpapi

import (
	"database/sql"
	"encoding/json"
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

func TestAuthConfigIsPublicAndOIDCAware(t *testing.T) {
	handler := New(Dependencies{
		DB: &sql.DB{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		AuthMode: "oidc", OIDCIssuer: "https://id.example.test/realms/geo", OIDCClientID: "geo-web", OIDCAudience: "geo-api",
	})
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/auth-config", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	var body struct {
		Mode     string `json:"mode"`
		Issuer   string `json:"issuer"`
		ClientID string `json:"clientId"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Mode != "oidc" || body.Issuer != "https://id.example.test/realms/geo" || body.ClientID != "geo-web" {
		t.Fatalf("unexpected auth config: %+v", body)
	}
}
