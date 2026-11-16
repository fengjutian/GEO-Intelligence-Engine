package evidencechain

import (
	"errors"
	"testing"
)

func TestNormalizeInput(t *testing.T) {
	input, err := NormalizeInput(UpsertInput{
		QuestionID: " q ", ResponseID: " r ", ChangeSummary: " change ",
		ExpectedOutcome: " improve ", VerificationPlan: " rerun ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.QuestionID != "q" || input.RetestOutcome != OutcomePending {
		t.Fatalf("unexpected normalized input: %+v", input)
	}
}

func TestNormalizeInputRejectsOutcomeWithoutRetest(t *testing.T) {
	_, err := NormalizeInput(UpsertInput{
		QuestionID: "q", ResponseID: "r", ChangeSummary: "change",
		ExpectedOutcome: "improve", VerificationPlan: "rerun", RetestOutcome: OutcomeImproved,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}
