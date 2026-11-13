package crawls

import "testing"

func TestNormalizeCrawlURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "lowercases host and scheme", raw: "HTTPS://Example.COM/Path", want: "https://example.com/Path"},
		{name: "drops fragment", raw: "https://example.com/a#section", want: "https://example.com/a"},
		{name: "drops default https port", raw: "https://example.com:443/a", want: "https://example.com/a"},
		{name: "drops default http port", raw: "http://example.com:80/", want: "http://example.com/"},
		{name: "keeps non default port", raw: "https://example.com:8443/a", want: "https://example.com:8443/a"},
		{name: "promotes empty path", raw: "https://example.com", want: "https://example.com/"},
		{name: "drops userinfo", raw: "https://user:pass@example.com/a", want: "https://example.com/a"},
		{name: "preserves query", raw: "https://example.com/search?q=geo&page=2", want: "https://example.com/search?q=geo&page=2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeCrawlURL(test.raw)
			if err != nil {
				t.Fatalf("NormalizeCrawlURL(%q) returned error: %v", test.raw, err)
			}
			if got != test.want {
				t.Fatalf("NormalizeCrawlURL(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestNormalizeCrawlURLRejectsUnsupportedInput(t *testing.T) {
	for _, raw := range []string{"", "   ", "mailto:hello@example.com", "javascript:alert(1)", "/relative/path", "https:///missing-host"} {
		if got, err := NormalizeCrawlURL(raw); err == nil {
			t.Fatalf("NormalizeCrawlURL(%q) = %q, want error", raw, got)
		}
	}
}

func TestSameSite(t *testing.T) {
	tests := []struct {
		candidate string
		base      string
		want      bool
	}{
		{"https://example.com/a", "https://example.com", true},
		{"https://www.example.com/a", "https://example.com", true},
		{"https://example.com/a", "https://www.example.com", true},
		{"http://example.com/a", "https://example.com", true},
		{"https://blog.example.com/a", "https://example.com", false},
		{"https://example.org/a", "https://example.com", false},
		{"https://example.com.evil.test/a", "https://example.com", false},
	}
	for _, test := range tests {
		if got := SameSite(test.candidate, test.base); got != test.want {
			t.Fatalf("SameSite(%q, %q) = %v, want %v", test.candidate, test.base, got, test.want)
		}
	}
}

func TestPlanDiscovery(t *testing.T) {
	base := "https://example.com"
	links := []string{
		"https://example.com/about",
		"https://example.com/about#team",
		"https://example.com/about",
		"https://example.com/pricing?plan=pro",
		"https://blog.example.com/post",
		"https://other.example.org/page",
		"mailto:sales@example.com",
		"https://www.example.com/contact",
	}

	got := PlanDiscovery(base, "https://example.com/", links, 0, 2, 10)
	want := []string{"https://example.com/about", "https://example.com/pricing?plan=pro", "https://www.example.com/contact"}
	if len(got) != len(want) {
		t.Fatalf("PlanDiscovery returned %d URLs (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("PlanDiscovery[%d] = %q, want %q (all: %v)", index, got[index], want[index], got)
		}
	}
}

func TestPlanDiscoveryRespectsDepthBudgetAndPage(t *testing.T) {
	links := []string{"https://example.com/a", "https://example.com/b", "https://example.com/c"}
	if got := PlanDiscovery("https://example.com", "https://example.com/", links, 2, 2, 10); len(got) != 0 {
		t.Fatalf("links at max depth must not be planned, got %v", got)
	}
	if got := PlanDiscovery("https://example.com", "https://example.com/", links, 0, 2, 2); len(got) != 2 {
		t.Fatalf("page budget must cap discovery at 2, got %v", got)
	}
	if got := PlanDiscovery("https://example.com", "https://example.com", []string{"https://example.com/"}, 0, 2, 10); len(got) != 0 {
		t.Fatalf("the current page must not be rediscovered, got %v", got)
	}
}

func TestNormalizeLimits(t *testing.T) {
	got := NormalizeLimits(CreateInput{})
	if got.MaxPages != DefaultMaxPages || got.MaxDepth != 0 || got.Concurrency != DefaultConcurrency {
		t.Fatalf("defaults not applied: %+v", got)
	}
	got = NormalizeLimits(CreateInput{MaxPages: -1, MaxDepth: -3, Concurrency: 99})
	if got.MaxPages != DefaultMaxPages || got.MaxDepth != 0 || got.Concurrency != MaxConcurrency {
		t.Fatalf("clamps not applied: %+v", got)
	}
}
