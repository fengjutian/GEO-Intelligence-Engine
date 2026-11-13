package projects

import "testing"

func TestNormalizeEntityName(t *testing.T) {
	tests := map[string]string{
		"  Acme，Cloud  ": "acme cloud",
		"ＡＣＭＥ":           "acme",
		"Acme---AI":      "acme ai",
		"产品 X":           "产品 x",
	}
	for input, expected := range tests {
		if actual := NormalizeEntityName(input); actual != expected {
			t.Errorf("NormalizeEntityName(%q) = %q, want %q", input, actual, expected)
		}
	}
}
