package experiments

import "testing"

func TestCompareDeclaresEffectiveWhenPoweredAndPositive(t *testing.T) {
	result := Compare(Counts{Numerator: 40, Denominator: 100}, Counts{Numerator: 60, Denominator: 100})
	if result.Insufficient {
		t.Fatal("expected sufficient sample")
	}
	if !result.Significant || result.Direction != "positive" {
		t.Fatalf("result = %+v", result)
	}
	if got := Conclusion(result); got != "effective" {
		t.Fatalf("conclusion = %q", got)
	}
	if result.Uplift < 0.19 || result.Uplift > 0.21 {
		t.Fatalf("uplift = %v", result.Uplift)
	}
}

func TestCompareFlagsInsufficientSample(t *testing.T) {
	result := Compare(Counts{Numerator: 3, Denominator: 4}, Counts{Numerator: 4, Denominator: 4})
	if !result.Insufficient {
		t.Fatalf("small sample must be insufficient: %+v", result)
	}
	if got := Conclusion(result); got != "insufficient_data" {
		t.Fatalf("conclusion = %q", got)
	}
}

func TestCompareDetectsNegativeEffect(t *testing.T) {
	result := Compare(Counts{Numerator: 60, Denominator: 100}, Counts{Numerator: 35, Denominator: 100})
	if Conclusion(result) != "negative" {
		t.Fatalf("conclusion = %q (%+v)", Conclusion(result), result)
	}
}

func TestCompareWithoutEffect(t *testing.T) {
	result := Compare(Counts{Numerator: 50, Denominator: 100}, Counts{Numerator: 52, Denominator: 100})
	if Conclusion(result) != "no_significant_change" {
		t.Fatalf("conclusion = %q (p=%v)", Conclusion(result), result.PValue)
	}
}

func TestCompareHandlesEmptyDenominator(t *testing.T) {
	result := Compare(Counts{}, Counts{})
	if !result.Insufficient || result.PValue != 0 {
		t.Fatalf("result = %+v", result)
	}
}
