package httpapi

import "testing"

func TestMatchShareEntityUsesAliasFallback(t *testing.T) {
	byName := map[string]shareMatch{
		"深度求索": {mention: 0.75, share: 0.5},
	}
	got := matchShareEntity(byName, "DeepSeek", []string{"深度求索"})
	if got.mention != 0.75 || got.share != 0.5 {
		t.Fatalf("expected alias match, got %+v", got)
	}
}

func TestMatchShareEntityPrefersCanonicalName(t *testing.T) {
	byName := map[string]shareMatch{
		"deepseek": {mention: 0.9},
		"深度求索":     {mention: 0.1},
	}
	got := matchShareEntity(byName, "DeepSeek", []string{"深度求索"})
	if got.mention != 0.9 {
		t.Fatalf("expected canonical match, got %+v", got)
	}
}
