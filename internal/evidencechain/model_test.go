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
	if input.QuestionID != "q" || input.MetricCode != "mention_rate" || input.TargetDelta != 0.01 {
		t.Fatalf("unexpected normalized input: %+v", input)
	}
}

func TestNormalizeInputRejectsUnsupportedMetric(t *testing.T) {
	_, err := NormalizeInput(UpsertInput{
		QuestionID: "q", ResponseID: "r", ChangeSummary: "change",
		ExpectedOutcome: "improve", VerificationPlan: "rerun", MetricCode: "not_a_metric",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCompareSnapshotsRequiresCompatibleProtocolAndConfidence(t *testing.T) {
	value1, value2, low1, high1, low2, high2, denominator := 0.2, 0.5, 0.1, 0.3, 0.35, 0.65, 40.0
	protocol := ProtocolSnapshot{QuestionVersionID: "qv", MetricVersion: "m", JudgeVersion: "j", PromptTemplateVersion: "p", Channel: "model_api", ModelSelector: "model", Region: "CN", Language: "zh-CN"}
	result := compareSnapshots(protocol, protocol,
		MetricPoint{Value: &value1, CILow: &low1, CIHigh: &high1, Denominator: &denominator},
		MetricPoint{Value: &value2, CILow: &low2, CIHigh: &high2, Denominator: &denominator}, 0.05)
	if !result.Comparable || result.Decision != OutcomeImproved {
		t.Fatalf("unexpected comparison: %+v", result)
	}
	protocol.JudgeVersion = "changed"
	result = compareSnapshots(ProtocolSnapshot{JudgeVersion: "old"}, protocol, MetricPoint{}, MetricPoint{}, 0.05)
	if result.Comparable || result.Decision != OutcomeInconclusive {
		t.Fatalf("protocol drift must be inconclusive: %+v", result)
	}
}
