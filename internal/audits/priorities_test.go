package audits

import "testing"

func TestCalculatePriorityMapping(t *testing.T) {
	cases := []struct {
		severity string
		want     string
	}{
		{"critical", "P0"},
		{"high", "P1"},
		{"medium", "P2"},
		{"low", "P3"},
	}
	for _, test := range cases {
		priority, score, components := calculatePriority(test.severity, 1)
		if priority != test.want {
			t.Fatalf("calculatePriority(%s) = %s (score %.3f), want %s", test.severity, priority, score, test.want)
		}
		if score <= 0 || score > 1 {
			t.Fatalf("score out of range for %s: %.3f", test.severity, score)
		}
		for _, key := range []string{"impact", "confidence", "businessImportance", "fixCost", "risk", "weights", "priorityVersion"} {
			if _, ok := components[key]; !ok {
				t.Fatalf("components missing %s for %s", key, test.severity)
			}
		}
	}
}

func TestCalculatePriorityIsDeterministic(t *testing.T) {
	firstPriority, firstScore, _ := calculatePriority("high", 0.8)
	secondPriority, secondScore, _ := calculatePriority("high", 0.8)
	if firstPriority != secondPriority || firstScore != secondScore {
		t.Fatalf("priority calculation is not deterministic: %s %.4f vs %s %.4f", firstPriority, firstScore, secondPriority, secondScore)
	}
}
