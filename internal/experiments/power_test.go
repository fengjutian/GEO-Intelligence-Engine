package experiments

import (
	"math"
	"testing"
)

func TestRequiredSampleSizeMatchesTwoProportionFormula(t *testing.T) {
	perGroup, err := RequiredSampleSize(0.10, 0.05, 0.05, 0.80)
	if err != nil {
		t.Fatal(err)
	}
	if perGroup < 600 || perGroup > 760 {
		t.Fatalf("perGroup = %d, want roughly 683", perGroup)
	}
}

func TestRequiredSampleSizeGrowsWithSmallerEffect(t *testing.T) {
	small, _ := RequiredSampleSize(0.10, 0.02, 0.05, 0.80)
	large, _ := RequiredSampleSize(0.10, 0.10, 0.05, 0.80)
	if small <= large {
		t.Fatalf("smaller effect must need more samples: %d vs %d", small, large)
	}
}

func TestRequiredSampleSizeRejectsInvalidInput(t *testing.T) {
	cases := []PlanInput{
		{BaselineRate: 0, MDE: 0.1, Alpha: 0.05, Power: 0.8},
		{BaselineRate: 0.5, MDE: 0.6, Alpha: 0.05, Power: 0.8},
		{BaselineRate: 0.5, MDE: 0.1, Alpha: 0, Power: 0.8},
		{BaselineRate: 0.5, MDE: 0.1, Alpha: 0.05, Power: 0.4},
	}
	for _, input := range cases {
		if _, err := RequiredSampleSize(input.BaselineRate, input.MDE, input.Alpha, input.Power); err == nil {
			t.Fatalf("expected error for %+v", input)
		}
	}
}

func TestMinimumDetectableEffectInvertsSampleSize(t *testing.T) {
	perGroup, err := RequiredSampleSize(0.10, 0.05, 0.05, 0.80)
	if err != nil {
		t.Fatal(err)
	}
	mde := MinimumDetectableEffect(perGroup, 0.10, 0.05, 0.80)
	if math.Abs(mde-0.05) > 0.01 {
		t.Fatalf("mde = %.4f, want near 0.05", mde)
	}
}

func TestAdjustedAlphaSpendsAcrossLooks(t *testing.T) {
	final := AdjustedAlpha(3, 3, 0.05)
	first := AdjustedAlpha(1, 3, 0.05)
	if math.Abs(final-0.05) > 1e-9 {
		t.Fatalf("final look alpha = %.6f, want 0.05", final)
	}
	// O'Brien-Fleming is conservative early: the first look spends far less than 0.05.
	if !(first > 0 && first < 0.01) {
		t.Fatalf("first look alpha = %.6f, want a small early boundary", first)
	}
	if AdjustedAlpha(1, 1, 0.05) <= 0 {
		t.Fatal("single look alpha must be positive")
	}
}

func TestEvaluateAtLookStopsOnlyOnSignal(t *testing.T) {
	weak := EvaluateAtLook(Counts{Numerator: 40, Denominator: 100}, Counts{Numerator: 45, Denominator: 100}, 1, 3)
	if weak.Stop {
		t.Fatalf("weak signal must continue: %+v", weak)
	}
	strong := EvaluateAtLook(Counts{Numerator: 20, Denominator: 500}, Counts{Numerator: 60, Denominator: 500}, 3, 3)
	if !strong.Stop || strong.Reason != "boundary_crossed" {
		t.Fatalf("strong signal must stop: %+v", strong)
	}
	tiny := EvaluateAtLook(Counts{Numerator: 2, Denominator: 4}, Counts{Numerator: 4, Denominator: 4}, 1, 1)
	if tiny.Reason != "insufficient_sample" {
		t.Fatalf("tiny sample = %+v", tiny)
	}
}

func TestBuildPlanFillsDerivedFields(t *testing.T) {
	plan, err := BuildPlan(PlanInput{BaselineRate: 0.2, MDE: 0.05})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Alpha != DefaultAlpha || plan.Power != DefaultPower {
		t.Fatalf("defaults not applied: %+v", plan)
	}
	if plan.PerGroup <= 0 || plan.Total != plan.PerGroup*2 {
		t.Fatalf("counts wrong: %+v", plan)
	}
	if plan.Interpretation == "" {
		t.Fatal("interpretation missing")
	}
}
