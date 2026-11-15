package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var input struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		if input.Model != "embed-small" || len(input.Input) != 2 {
			t.Fatalf("unexpected body: %+v", input)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"embedding": []float64{1, 0}},
			{"embedding": []float64{0, 1}},
		}})
	}))
	defer server.Close()
	client := New(server.URL, "secret", "embed-small")
	vectors, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 {
		t.Fatalf("unexpected vectors: %+v", vectors)
	}
}

func TestEmbedNotConfigured(t *testing.T) {
	if _, err := New("", "", "").Embed(context.Background(), []string{"x"}); err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}
