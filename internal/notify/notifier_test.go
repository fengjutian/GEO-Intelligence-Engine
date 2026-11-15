package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhook(t *testing.T) {
	var received Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	notifier := New(server.URL, "", "", "", "", "", "")
	result := notifier.Send(context.Background(), Alert{ID: "a1", Kind: "audit_execution_failed", Message: "failed"})
	if received.ID != "a1" || received.Kind != "audit_execution_failed" {
		t.Fatalf("unexpected webhook payload: %+v", received)
	}
	if result == "" || result == `{"status":"not_configured"}` {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestDisabled(t *testing.T) {
	if New("", "", "", "", "", "", "").Enabled() {
		t.Fatal("empty notifier must be disabled")
	}
}
