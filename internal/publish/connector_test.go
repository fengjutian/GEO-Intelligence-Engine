package publish

import (
	"strings"
	"testing"

	"github.com/fengjutian/geo-intelligence-engine/internal/drafts"
)

func TestConnectorExtension(t *testing.T) {
	cases := map[string]string{"jsonld": "json", "llms_txt": "txt", "meta": "md", "unknown": "md"}
	for kind, want := range cases {
		if got := connectorExtension(kind); got != want {
			t.Fatalf("%s -> %s, want %s", kind, got, want)
		}
	}
}

func TestValidateFilesystemPath(t *testing.T) {
	if err := ValidateFilesystemPath(""); err == nil {
		t.Fatal("empty path must be rejected")
	}
	if err := ValidateFilesystemPath("../etc"); err == nil {
		t.Fatal("traversal must be rejected")
	}
	if err := ValidateFilesystemPath("site-content"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderDraftLlmstxtWritesPlainText(t *testing.T) {
	body, err := RenderDraft(drafts.Draft{Kind: "llms_txt", Payload: map[string]any{"text": "# llms.txt\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# llms.txt\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestRenderDraftJsonldIsIndentedJSON(t *testing.T) {
	body, err := RenderDraft(drafts.Draft{Kind: "jsonld", Payload: map[string]any{"@context": "https://schema.org", "name": "Acme"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "\n  \"@context\"") {
		t.Fatalf("expected indented JSON, got %s", body)
	}
}
