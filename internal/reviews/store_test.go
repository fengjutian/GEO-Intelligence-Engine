package reviews

import (
	"errors"
	"testing"
)

func TestParseOverrideAcceptsKnownFields(t *testing.T) {
	override, err := parseOverride(map[string]any{
		"decision":            "agree",
		"answerOutcome":       " Recommendation ",
		"recommendationLabel": "mentioned",
		"contentValidity":     "valid",
		"sentiment":           "positive",
		"eligible":            true,
	})
	if err != nil {
		t.Fatalf("parseOverride returned error: %v", err)
	}
	if override.AnswerOutcome != "recommendation" {
		t.Fatalf("answerOutcome = %q", override.AnswerOutcome)
	}
	if override.RecommendationLabel != "mentioned" || override.ContentValidity != "valid" || override.Sentiment != "positive" {
		t.Fatalf("unexpected override: %+v", override)
	}
	if override.Eligible == nil || !*override.Eligible {
		t.Fatalf("eligible was not applied: %+v", override.Eligible)
	}
}

func TestParseOverrideIgnoresDecisionOnlyResults(t *testing.T) {
	override, err := parseOverride(map[string]any{"decision": "disagree"})
	if err != nil {
		t.Fatalf("parseOverride returned error: %v", err)
	}
	if override.AnswerOutcome != "" || override.Eligible != nil {
		t.Fatalf("expected empty override, got %+v", override)
	}
}

func TestParseOverrideRejectsInvalidValues(t *testing.T) {
	cases := []map[string]any{
		{"answerOutcome": "maybe"},
		{"contentValidity": "partially_valid"},
		{"sentiment": "angry"},
		{"answerOutcome": 42},
		{"eligible": "yes"},
	}
	for _, input := range cases {
		if _, err := parseOverride(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("input %v: expected ErrInvalid, got %v", input, err)
		}
	}
}

func TestParseOverrideRejectsOverlongRecommendationLabel(t *testing.T) {
	long := make([]byte, 65)
	for index := range long {
		long[index] = 'a'
	}
	if _, err := parseOverride(map[string]any{"recommendationLabel": string(long)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for overlong label, got %v", err)
	}
}

func TestErrorCodeMapping(t *testing.T) {
	cases := map[string]error{
		"NOT_FOUND":     ErrNotFound,
		"NOT_PENDING":   ErrConflict,
		"INVALID_INPUT": ErrInvalid,
		"INTERNAL":      errors.New("boom"),
	}
	for expected, err := range cases {
		if got := ErrorCode(err); got != expected {
			t.Fatalf("ErrorCode(%v) = %q, want %q", err, got, expected)
		}
	}
}

func TestBatchRecordCountsOutcomes(t *testing.T) {
	result := newBatchResult(2)
	result.record("item-a", nil)
	result.record("item-b", ErrConflict)
	if result.Applied != 1 || result.Failed != 1 {
		t.Fatalf("applied=%d failed=%d", result.Applied, result.Failed)
	}
	if result.Results[1].ErrorCode != "NOT_PENDING" {
		t.Fatalf("unexpected outcome: %+v", result.Results[1])
	}
}
