package judge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvaluateDecodesJudgeResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/judge/v1/evaluate" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content_validity":"valid","answer_outcome":"recommendation","recommendation_label":"mentioned","sentiment":"positive","confidence":1.0,"eligible_for_core_metrics":true,"judge_version":"judge-rules-v1.0","eligibility_rule_version":"eligibility-v1.0","mentions":[{"text":"Acme GEO","start":0,"end":8}]}`))
	}))
	defer server.Close()

	result, err := New(server.URL).Evaluate(context.Background(), Input{Question: "q", Answer: "Acme GEO is recommended", BrandName: "Acme GEO"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.EligibleForCoreMetrics || result.AnswerOutcome != "recommendation" || len(result.Mentions) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Mentions[0].Text != "Acme GEO" {
		t.Fatalf("unexpected mention: %+v", result.Mentions[0])
	}
}

func TestEvaluateRejectsServerErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	if _, err := New(server.URL).Evaluate(context.Background(), Input{Question: "q", Answer: "a"}); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}
