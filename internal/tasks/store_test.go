package tasks

import "testing"

func TestPriorityForSeverity(t *testing.T) {
	cases := map[string]string{"critical": "P0", "high": "P1", "medium": "P2", "low": "P3", "unknown": "P3"}
	for severity, want := range cases {
		if got := priorityForSeverity(severity); got != want {
			t.Fatalf("priorityForSeverity(%s) = %s, want %s", severity, got, want)
		}
	}
}

func TestAllowedTransitionEdges(t *testing.T) {
	valid := [][2]string{
		{"todo", "in_progress"},
		{"todo", "cancelled"},
		{"in_progress", "in_review"},
		{"in_review", "done"},
		{"in_review", "changes_requested"},
		{"in_review", "in_progress"},
		{"changes_requested", "in_progress"},
	}
	for _, edge := range valid {
		if !allowedTransitions[edge[0]][edge[1]] {
			t.Fatalf("expected %s -> %s to be allowed", edge[0], edge[1])
		}
	}
	invalid := [][2]string{
		{"todo", "done"},
		{"todo", "in_review"},
		{"in_progress", "done"},
		{"done", "in_progress"},
		{"cancelled", "todo"},
		{"changes_requested", "done"},
	}
	for _, edge := range invalid {
		if allowedTransitions[edge[0]][edge[1]] {
			t.Fatalf("expected %s -> %s to be rejected", edge[0], edge[1])
		}
	}
}
