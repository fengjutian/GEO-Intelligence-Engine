package monitoring

import (
	"strings"
	"testing"
)

func TestShouldAlertShareDrop(t *testing.T) {
	if ShouldAlertShareDrop(0.80, 0.70) {
		t.Fatal("10pp drop is below the 15pp threshold")
	}
	if !ShouldAlertShareDrop(0.80, 0.65) {
		t.Fatal("15pp drop must alert")
	}
	if !ShouldAlertShareDrop(0.80, 0.60) {
		t.Fatal("bigger drops must alert")
	}
	if ShouldAlertShareDrop(0.50, 0.60) {
		t.Fatal("share increase must not alert")
	}
}

func TestCompetitorOvertake(t *testing.T) {
	name, ok := CompetitorOvertake(0.30, []EntityShare{{"钉钉", 0.55}, {"企业微信", 0.20}})
	if !ok || name != "钉钉" {
		t.Fatalf("overtaker = %q ok=%v", name, ok)
	}
	if name, ok := CompetitorOvertake(0.60, []EntityShare{{"钉钉", 0.55}}); ok {
		t.Fatalf("brand leading should not alert, got %q", name)
	}
	if _, ok := CompetitorOvertake(0.40, nil); ok {
		t.Fatal("no competitors cannot overtake")
	}
}

func TestCompetitorOvertakePicksStrongest(t *testing.T) {
	name, ok := CompetitorOvertake(0.1, []EntityShare{{"A", 0.3}, {"B", 0.7}, {"C", 0.2}})
	if !ok || name != "B" {
		t.Fatalf("overtaker = %q", name)
	}
}

func TestShareDropMessage(t *testing.T) {
	message := ShareDropMessage(0.80, 0.60)
	for _, want := range []string{"80.0%", "60.0%", "20.0"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message missing %q: %s", want, message)
		}
	}
}
