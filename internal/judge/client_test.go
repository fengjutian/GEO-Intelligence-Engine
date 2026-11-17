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
		_, _ = w.Write([]byte(`{"content_validity":"valid","answer_outcome":"recommendation","recommendation_label":"mentioned","sentiment":"positive","confidence":1.0,"eligible_for_core_metrics":true,"judge_version":"judge-rules-v1.1","eligibility_rule_version":"eligibility-v1.0","mentions":[{"text":"Acme GEO","start":0,"end":8}],"citations":[{"raw_url":"https://example.com/guide","normalized_url":"https://example.com/guide","domain":"example.com","source_type":"official","confidence":1.0,"first_citation_sentence_index":1,"sentence_count":3,"citation_word_count":12.5,"answer_word_count":50,"citation_word_share":0.25,"position_adjusted_word_share":0.18}]}`))
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
	if len(result.Citations) != 1 || result.Citations[0].SourceType != "official" {
		t.Fatalf("unexpected citations: %+v", result.Citations)
	}
	if result.Citations[0].FirstCitationSentenceIndex != 1 || result.Citations[0].CitationWordShare != 0.25 || result.Citations[0].PositionAdjustedWordShare != 0.18 {
		t.Fatalf("unexpected citation visibility: %+v", result.Citations[0])
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
