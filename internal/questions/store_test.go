package questions

import "testing"

func TestNormalizeText(t *testing.T) {
	if got := NormalizeText("  Which   GEO Tool?  "); got != "which geo tool?" {
		t.Fatalf("unexpected normalized text: %q", got)
	}
}
