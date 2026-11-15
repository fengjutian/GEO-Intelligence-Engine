package semantic

import (
	"context"
	"testing"

	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
)

type fakeChat struct {
	content string
}

func (f fakeChat) Chat(context.Context, modelgateway.ChatRequest) (modelgateway.ChatResponse, error) {
	return modelgateway.ChatResponse{Model: "fake", Content: f.content}, nil
}

func TestJudgeAcceptsSupportedVerdict(t *testing.T) {
	store := NewStore(nil, fakeChat{content: `{"verdict":"supported","confidence":0.9,"evidence":"飞书支持多维表格"}`}, nil)
	verdict, confidence, quote, err := store.judge(context.Background(), "飞书支持多维表格", []Evidence{
		{Source: "brand_page", Text: "飞书支持多维表格与知识库"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if verdict != VerdictSupported || confidence != 0.9 || quote != "飞书支持多维表格" {
		t.Fatalf("verdict=%q confidence=%v quote=%q", verdict, confidence, quote)
	}
}

func TestJudgeParsesFencedJSONAndRejectsContradiction(t *testing.T) {
	store := NewStore(nil, fakeChat{content: "```json\n{\"verdict\":\"unsupported\",\"confidence\":0.7,\"evidence\":\"页面写的是个人版免费\"}\n```"}, nil)
	verdict, confidence, _, err := store.judge(context.Background(), "飞书完全免费", nil)
	if err != nil {
		t.Fatal(err)
	}
	if verdict != VerdictUnsupported || confidence != 0.7 {
		t.Fatalf("verdict=%q confidence=%v", verdict, confidence)
	}
}

func TestJudgeFallsBackToUnknownOnGarbage(t *testing.T) {
	store := NewStore(nil, fakeChat{content: "I cannot answer that."}, nil)
	verdict, confidence, _, err := store.judge(context.Background(), "claim", nil)
	if err != nil {
		t.Fatal(err)
	}
	if verdict != VerdictUnknown || confidence != 0 {
		t.Fatalf("verdict=%q confidence=%v", verdict, confidence)
	}
}

func TestJudgeClampsConfidence(t *testing.T) {
	store := NewStore(nil, fakeChat{content: `{"verdict":"supported","confidence":4}`}, nil)
	_, confidence, _, err := store.judge(context.Background(), "claim", nil)
	if err != nil {
		t.Fatal(err)
	}
	if confidence != 1 {
		t.Fatalf("confidence = %v", confidence)
	}
}
