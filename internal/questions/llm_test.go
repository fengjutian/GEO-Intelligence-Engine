package questions

import (
	"errors"
	"strings"
	"testing"
)

func TestParseLLMCandidates(t *testing.T) {
	content := "```json\n[{\"text\":\"Acme GEO 是什么？\",\"questionType\":\"brand\",\"intent\":\"definition\",\"topic\":\"Acme GEO\",\"funnelStage\":\"awareness\",\"basis\":\"brand\"},{\"text\":\"Acme GEO 是什么？\",\"questionType\":\"brand\"},{\"text\":\"Bad\",\"questionType\":\"unknown\"}]\n```"
	items, err := ParseLLMCandidates(content, "Acme GEO")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one deduplicated candidate, got %+v", items)
	}
	if !items[0].Branded {
		t.Fatal("branded candidate must be detected")
	}
}

func TestParseLLMCandidatesRejectsGarbage(t *testing.T) {
	if _, err := ParseLLMCandidates("no json here", ""); !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("expected ErrNoCandidates, got %v", err)
	}
}

func TestBuildLLMPromptContainsSnapshot(t *testing.T) {
	prompt := BuildLLMPrompt(GenerationContext{Brand: "Acme GEO", Products: []string{"Insight"}, Competitors: []string{"Globex"}}, "zh-CN")
	for _, expected := range []string{"Acme GEO", "Insight", "Globex", "zh-CN"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q: %s", expected, prompt)
		}
	}
}
