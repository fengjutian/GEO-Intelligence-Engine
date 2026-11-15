package main

import (
	"strings"
	"testing"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
)

func TestBuildMessagesDoesNotPrimeTheBrand(t *testing.T) {
	messages := buildMessages(audits.SampleClaim{
		BrandName:    "飞书",
		BrandDomain:  "feishu.cn",
		Language:     "zh-CN",
		Region:       "CN",
		QuestionText: "国内团队协作办公软件有哪些值得推荐？",
	})
	if len(messages) != 2 {
		t.Fatalf("messages = %d", len(messages))
	}
	system := messages[0].Content
	if strings.Contains(system, "飞书") || strings.Contains(strings.ToLower(system), "feishu") {
		t.Fatalf("system prompt must not name the brand: %q", system)
	}
	if !strings.Contains(system, "zh-CN") || !strings.Contains(system, "CN") {
		t.Fatalf("system prompt lost market context: %q", system)
	}
	if messages[1].Content != "国内团队协作办公软件有哪些值得推荐？" {
		t.Fatalf("user message = %q", messages[1].Content)
	}
}
