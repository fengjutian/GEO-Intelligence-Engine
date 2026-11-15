package publish

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSignIsDeterministicAndSecretScoped(t *testing.T) {
	body := []byte(`{"event":"publish"}`)
	first := Sign("secret-a", body)
	if first != Sign("secret-a", body) {
		t.Fatal("signature must be deterministic")
	}
	if first == Sign("secret-b", body) {
		t.Fatal("different secrets must produce different signatures")
	}
	if !strings.HasPrefix(SignedValue("secret-a", body), "sha256=") {
		t.Fatal("header value must carry the algorithm prefix")
	}
}

func TestBuildPayloadAndEncode(t *testing.T) {
	payload := BuildPayload("publish", "proj-1", "draft-1", "jsonld", "Schema", 3, map[string]any{"@context": "https://schema.org"}, time.Unix(0, 0))
	body, err := Encode(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"event", "projectId", "draftId", "kind", "title", "version", "content", "publishedAt"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("payload missing %q: %s", key, body)
		}
	}
	if decoded["event"] != "publish" || decoded["version"].(float64) != 3 {
		t.Fatalf("unexpected payload: %s", body)
	}
	if _, ok := decoded["reason"]; ok {
		t.Fatal("reason must be omitted when empty")
	}
	rollback := BuildPayload("rollback", "proj-1", "draft-1", "jsonld", "Schema", 3, map[string]any{}, time.Unix(0, 0))
	rollback.Reason = "content regression"
	body, _ = Encode(rollback)
	if !strings.Contains(string(body), "content regression") {
		t.Fatalf("rollback reason missing: %s", body)
	}
}

func TestValidateEndpoint(t *testing.T) {
	if err := ValidateEndpoint("https://hooks.example.com/geo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, endpoint := range []string{"", "ftp://example.com/hook", "hooks.example.com"} {
		if err := ValidateEndpoint(endpoint); err == nil {
			t.Fatalf("%q should be rejected", endpoint)
		}
	}
}
