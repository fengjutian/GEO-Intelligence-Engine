package semantic

import "testing"

func TestSplitClaimsHandlesChineseAndEnglish(t *testing.T) {
	claims := SplitClaims("飞书是一个协作平台，支持文档与日历。It also offers AI writing. Short. 另一个很长的断言句子用来测试长度过滤是否生效因为长度必须超过八个字符才会被保留。")
	if len(claims) < 2 {
		t.Fatalf("claims = %#v", claims)
	}
	if claims[0] != "飞书是一个协作平台，支持文档与日历" {
		t.Fatalf("first claim = %q", claims[0])
	}
}

func TestSplitClaimsDeduplicatesAndCaps(t *testing.T) {
	text := "这是第一个足够长的断言句子。这是第一个足够长的断言句子。这是第二个足够长的断言句子。这是第三个足够长的断言句子。"
	claims := SplitClaims(text)
	if len(claims) != 3 {
		t.Fatalf("claims = %#v", claims)
	}
	if len(SplitClaims(text+"这是第四个足够长的断言句子。这是第五个足够长的断言句子。这是第六个足够长的断言句子。这是第七个足够长的断言句子。这是第八个足够长的断言句子。这是第九个足够长的断言句子。")) > MaxClaims {
		t.Fatal("claim cap not enforced")
	}
}

func TestTermsBuildsChineseBigrams(t *testing.T) {
	terms := Terms("飞书文档")
	if !terms["飞书"] || !terms["书文"] || !terms["文档"] {
		t.Fatalf("terms = %#v", terms)
	}
	english := Terms("Acme GEO platform")
	if !english["acme"] || !english["geo"] || !english["platform"] {
		t.Fatalf("english terms = %#v", english)
	}
}

func TestLexicalScoreRanksOverlap(t *testing.T) {
	high := LexicalScore("飞书支持多维表格", "飞书支持多维表格与知识库")
	low := LexicalScore("飞书支持多维表格", "Notion is a note taking tool")
	if high <= low || high < 0.3 {
		t.Fatalf("high=%v low=%v", high, low)
	}
}

func TestRankEvidenceSortsAndTruncates(t *testing.T) {
	pool := []Evidence{
		{Source: "brand_page", Text: "unrelated"},
		{Source: "third_party", Text: "飞书支持多维表格"},
		{Source: "brand_page", Text: "飞书支持多维表格与知识库"},
	}
	ranked := RankEvidence("飞书支持多维表格", pool, []float64{0.1, 0.6, 0.9}, 2)
	if len(ranked) != 2 {
		t.Fatalf("ranked = %#v", ranked)
	}
	if ranked[0].Score != 0.9 || ranked[1].Score != 0.6 {
		t.Fatalf("order = %#v", ranked)
	}
}

func TestNeedsJudgementThreshold(t *testing.T) {
	if NeedsJudgement(Evidence{Score: 0.2}) {
		t.Fatal("weak evidence must not trigger a judge call")
	}
	if !NeedsJudgement(Evidence{Score: EvidenceThreshold}) {
		t.Fatal("threshold evidence should trigger a judge call")
	}
}

func TestNormalizeVerdict(t *testing.T) {
	cases := map[string]string{
		"Supported":    VerdictSupported,
		"true":         VerdictSupported,
		"unsupported":  VerdictUnsupported,
		"contradicted": VerdictUnsupported,
		"maybe":        VerdictUnknown,
		"":             VerdictUnknown,
	}
	for input, expected := range cases {
		if got := NormalizeVerdict(input); got != expected {
			t.Fatalf("NormalizeVerdict(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestCosine(t *testing.T) {
	if got := Cosine([]float64{1, 0}, []float64{1, 0}); got != 1 {
		t.Fatalf("cosine = %v", got)
	}
	if got := Cosine([]float64{1, 0}, []float64{0, 1}); got != 0 {
		t.Fatalf("cosine = %v", got)
	}
	if got := Cosine(nil, nil); got != 0 {
		t.Fatalf("cosine = %v", got)
	}
}
