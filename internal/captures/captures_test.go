package captures

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func validCapture() CaptureInput {
	return CaptureInput{
		CaptureID:  "018f3f9e-8a10-7c22-b001-6f1c2d3e4a55",
		AuditID:    "01a1de7d-b707-7921-b185-ef29bce5ed70",
		QuestionID: "01a1de7d-b69b-7b44-8629-d2fa3830398a",
		Channel:    ChannelConsumer,
		Surface:    Surface{Provider: "openai", Product: "chatgpt", Model: "gpt-5", Locale: "zh-CN", Region: "CN"},
		ObservedAt: time.Now().UTC(),
		Answer:     Answer{Text: "飞书支持多维表格", FinishReason: "stop"},
		Provenance: Provenance{Method: "manual_console"},
		Consent:    Consent{Basis: "customer_authorized", Reference: "PO-1"},
	}
}

func TestValidateCaptureAcceptsManualConsole(t *testing.T) {
	if err := ValidateCapture(validCapture(), time.Now().UTC()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCaptureRejectsModelAPI(t *testing.T) {
	capture := validCapture()
	capture.Channel = "model_api"
	err := ValidateCapture(capture, time.Now().UTC())
	validation, ok := err.(*ValidationError)
	if !ok || validation.Code != "CHANNEL_NOT_CAPTURABLE" {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateCaptureRejectsMissingConsentAndStaleTimestamp(t *testing.T) {
	capture := validCapture()
	capture.Consent = Consent{}
	if err := ValidateCapture(capture, time.Now().UTC()); err == nil {
		t.Fatal("expected consent error")
	}
	fresh := validCapture()
	fresh.ObservedAt = time.Now().UTC().Add(-8 * 24 * time.Hour)
	err := ValidateCapture(fresh, time.Now().UTC())
	validation, ok := err.(*ValidationError)
	if !ok || validation.Code != "TIMESTAMP_OUT_OF_RANGE" {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateCaptureRejectsEmptyAnswerAndBadCitation(t *testing.T) {
	empty := validCapture()
	empty.Answer.Text = "   "
	if err := ValidateCapture(empty, time.Now().UTC()); err == nil {
		t.Fatal("expected empty answer error")
	}
	badCitation := validCapture()
	badCitation.Citations = []Citation{{URL: "ftp://example.com/x"}}
	err := ValidateCapture(badCitation, time.Now().UTC())
	validation, ok := err.(*ValidationError)
	if !ok || validation.Code != "CITATION_INVALID" {
		t.Fatalf("err = %v", err)
	}
}

func TestTierOf(t *testing.T) {
	cases := []struct {
		method     string
		screenshot bool
		signed     bool
		tier       string
	}{
		{"partner_api", false, true, TierPartner},
		{"browser_extension", false, true, TierClient},
		{"manual_console", true, false, TierAttested},
		{"manual_console", false, false, TierManual},
		{"browser_extension", false, false, TierManual},
	}
	for _, item := range cases {
		if got := TierOf(item.method, item.screenshot, item.signed); got != item.tier {
			t.Fatalf("TierOf(%s,%v,%v) = %s, want %s", item.method, item.screenshot, item.signed, got, item.tier)
		}
	}
	if TierCountsForReport(TierManual) || !TierCountsForReport(TierAttested) {
		t.Fatal("tier report policy mismatch")
	}
}

func TestIdempotencyKeyIsStableWithinTheMinute(t *testing.T) {
	capture := validCapture()
	first := IdempotencyKey(capture)
	again := IdempotencyKey(capture)
	if first != again {
		t.Fatal("idempotency key must be stable")
	}
	moved := capture
	moved.ObservedAt = capture.ObservedAt.Add(90 * time.Second)
	if IdempotencyKey(moved) == first {
		t.Fatal("different minutes must produce different keys")
	}
	changed := capture
	changed.Answer.Text = "不同的回答"
	if IdempotencyKey(changed) == first {
		t.Fatal("different answers must produce different keys")
	}
}

func TestDecodeCapturesEnforcesBounds(t *testing.T) {
	if _, err := DecodeCaptures(json.RawMessage(`[]`)); err == nil {
		t.Fatal("empty batch must be rejected")
	}
	if _, err := DecodeCaptures(json.RawMessage(`not-json`)); err == nil {
		t.Fatal("invalid JSON must be rejected")
	}
	raw := json.RawMessage(`[{"captureId":"x","channel":"consumer_app_capture"}]`)
	items, err := DecodeCaptures(raw)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestVerifySignatureRoundTrip(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(publicKey)
	t.Setenv("GEO_CAPTURE_AGENT_KEYS", `{"test-key":"`+encoded+`"}`)

	raw := []byte(`[{"captureId":"abc"}]`)
	sum := sha256.Sum256(raw)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, sum[:]))

	verified, err := VerifySignature(Agent{PublicKeyID: "test-key", Signature: signature}, raw)
	if err != nil || !verified {
		t.Fatalf("verified=%v err=%v", verified, err)
	}
	tampered, err := VerifySignature(Agent{PublicKeyID: "test-key", Signature: signature}, []byte(`[{"captureId":"abd"}]`))
	if err != nil || tampered {
		t.Fatalf("tampered verified=%v err=%v", tampered, err)
	}
	if verified, err := VerifySignature(Agent{}, raw); err != nil || verified {
		t.Fatalf("missing signature must be unverified without error: %v %v", verified, err)
	}
}

func TestRequiresSignature(t *testing.T) {
	if !RequiresSignature("partner_api") || !RequiresSignature("browser_extension") {
		t.Fatal("A/B methods must require signatures")
	}
	if RequiresSignature("manual_console") {
		t.Fatal("manual console must not require a signature")
	}
	_ = os.Getenv("GEO_CAPTURE_AGENT_KEYS")
}

func TestLooksMojibake(t *testing.T) {
	broken := []string{"????????,??????", "Is it???", "????????????????"}
	for _, text := range broken {
		if !looksMojibake(text) {
			t.Fatalf("%q should be flagged", text)
		}
	}
	healthy := []string{"飞书支持多维表格", "What is this?", "正常的中文回答，包含问号？", "Short answer."}
	for _, text := range healthy {
		if looksMojibake(text) {
			t.Fatalf("%q should not be flagged", text)
		}
	}
}

func TestValidateCaptureRejectsMojibakeAnswer(t *testing.T) {
	capture := validCapture()
	capture.Answer.Text = "????????,??????"
	err := ValidateCapture(capture, time.Now().UTC())
	validation, ok := err.(*ValidationError)
	if !ok || validation.Code != "ANSWER_ENCODING_SUSPECT" {
		t.Fatalf("err = %v", err)
	}
}
