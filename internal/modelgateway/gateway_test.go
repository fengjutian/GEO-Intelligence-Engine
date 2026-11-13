package modelgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayUsesProviderSpecificTokenField(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization header missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests <- body
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "request-1", "model": body["model"],
			"choices":   []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": "ok"}}},
			"usage":     map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
			"base_resp": map[string]any{"status_code": 0, "status_msg": ""},
		})
	}))
	defer server.Close()

	gateway := New(
		ProviderConfig{Name: "deepseek", BaseURL: server.URL, APIKey: "secret", Model: "deepseek-test", MaxField: "max_tokens"},
		ProviderConfig{Name: "minimax", BaseURL: server.URL, APIKey: "secret", Model: "minimax-test", MaxField: "max_completion_tokens"},
	)
	for _, provider := range []string{"deepseek", "minimax"} {
		response, err := gateway.Chat(context.Background(), ChatRequest{Provider: provider, Messages: []Message{{Role: "user", Content: "hello"}}, MaxTokens: 128})
		if err != nil {
			t.Fatalf("Chat(%s): %v", provider, err)
		}
		if response.Content != "ok" || response.Usage.TotalTokens != 3 {
			t.Fatalf("unexpected response: %#v", response)
		}
		body := <-requests
		expected := "max_tokens"
		unexpected := "max_completion_tokens"
		if provider == "minimax" {
			expected, unexpected = unexpected, expected
		}
		if body[expected] != float64(128) {
			t.Fatalf("%s missing %s: %#v", provider, expected, body)
		}
		if _, exists := body[unexpected]; exists {
			t.Fatalf("%s unexpectedly sent %s", provider, unexpected)
		}
	}
}

func TestGatewayRejectsMiniMaxApplicationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"base_resp": map[string]any{"status_code": 1004, "status_msg": "invalid key"}})
	}))
	defer server.Close()
	gateway := New(ProviderConfig{Name: "minimax", BaseURL: server.URL, APIKey: "secret", Model: "test", MaxField: "max_completion_tokens"})
	if _, err := gateway.Chat(context.Background(), ChatRequest{Provider: "minimax", Messages: []Message{{Role: "user", Content: "hello"}}}); err == nil {
		t.Fatal("expected MiniMax application error")
	}
}

func TestGatewayDoesNotExposeAPIKeysInStatus(t *testing.T) {
	gateway := New(ProviderConfig{Name: "minimax", BaseURL: "https://api.minimaxi.com/v1", APIKey: "top-secret", Model: "test", MaxField: "max_completion_tokens"})
	encoded, err := json.Marshal(gateway.Statuses())
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || contains(string(encoded), "top-secret") {
		t.Fatalf("status exposed credential: %s", encoded)
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
