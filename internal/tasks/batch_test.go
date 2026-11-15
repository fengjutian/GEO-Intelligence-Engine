package tasks

import "testing"

func TestBatchTitleIsDeterministic(t *testing.T) {
	if got := BatchTitle("CONTENT_THIN", 15); got != "[CONTENT_THIN] 15 条问题待修复" {
		t.Fatalf("title = %q", got)
	}
	if BatchTitle("A", 1) == BatchTitle("A", 2) {
		t.Fatal("different counts must produce different titles")
	}
}

func TestBatchAcceptanceDefaultsAndOverride(t *testing.T) {
	defaults := BatchAcceptance("CANONICAL_MISSING", 28, nil)
	if len(defaults) != 3 {
		t.Fatalf("defaults = %#v", defaults)
	}
	custom := []string{"只保留这一条"}
	if got := BatchAcceptance("CANONICAL_MISSING", 28, custom); len(got) != 1 || got[0] != custom[0] {
		t.Fatalf("custom = %#v", got)
	}
}

func TestBatchAcceptanceMentionsRuleAndCount(t *testing.T) {
	items := BatchAcceptance("CONTENT_JS_ONLY", 13, nil)
	if items[0] != "复测后 CONTENT_JS_ONLY 的 open finding 归零" {
		t.Fatalf("first item = %q", items[0])
	}
}
