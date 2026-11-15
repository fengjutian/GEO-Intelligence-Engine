package captures

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ProtocolVersion is the accepted X-Capture-Protocol value.
const ProtocolVersion = "1"

// MaxCaptures bounds one batch.
const MaxCaptures = 100

// Channel values accepted by the capture protocol.
const (
	ChannelSearchAPI = "search_api"
	ChannelConsumer  = "consumer_app_capture"
)

// Trust tiers from SPEC-11.
const (
	TierPartner  = "A"
	TierClient   = "B"
	TierAttested = "C"
	TierManual   = "D"
)

// ValidationError carries a stable error code for the API response.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(code, message string) error { return &ValidationError{Code: code, Message: message} }

// BatchRequest is the submitted capture envelope. Captures stays raw so the
// agent signature can be verified over the exact bytes that were signed.
type BatchRequest struct {
	BatchID  string          `json:"batchId"`
	Agent    Agent           `json:"agent"`
	Captures json.RawMessage `json:"captures"`
}

// Agent identifies the capture client.
type Agent struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Version     string `json:"version"`
	PublicKeyID string `json:"publicKeyId"`
	Signature   string `json:"signature"`
}

// Surface is where the answer was observed.
type Surface struct {
	Provider    string `json:"provider"`
	Product     string `json:"product"`
	Model       string `json:"model"`
	Locale      string `json:"locale"`
	Region      string `json:"region"`
	AccountTier string `json:"accountTier"`
}

// Answer is the observed response text.
type Answer struct {
	Text         string `json:"text"`
	FinishReason string `json:"finishReason"`
	Truncated    bool   `json:"truncated"`
}

// Citation is one source shown next to the answer.
type Citation struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Position int    `json:"position"`
}

// Provenance describes how the capture was obtained.
type Provenance struct {
	Method             string `json:"method"`
	SessionHash        string `json:"sessionHash"`
	ScreenshotSHA256   string `json:"screenshotSha256"`
	CollectorVersion   string `json:"collectorVersion"`
	DOMSelectorVersion string `json:"domSelectorVersion"`
}

// Consent records the legal basis for capturing.
type Consent struct {
	Basis     string `json:"basis"`
	Reference string `json:"reference"`
}

// CaptureInput is one observed answer.
type CaptureInput struct {
	CaptureID  string     `json:"captureId"`
	AuditID    string     `json:"auditId"`
	QuestionID string     `json:"questionId"`
	Channel    string     `json:"channel"`
	Surface    Surface    `json:"surface"`
	ObservedAt time.Time  `json:"observedAt"`
	Answer     Answer     `json:"answer"`
	Citations  []Citation `json:"citations"`
	Provenance Provenance `json:"provenance"`
	Consent    Consent    `json:"consent"`
}

// BatchResult reports per-item outcomes.
type BatchResult struct {
	BatchID string       `json:"batchId"`
	Results []ItemResult `json:"results"`
}

// ItemResult is the outcome of one capture inside a batch.
type ItemResult struct {
	CaptureID string `json:"captureId"`
	Status    string `json:"status"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

// ValidMethod guards the provenance methods.
func ValidMethod(method string) bool {
	switch method {
	case "browser_extension", "manual_console", "partner_api", "screenshot_ocr":
		return true
	}
	return false
}

// TierOf maps method, screenshot presence and signature status onto SPEC-11 tiers.
func TierOf(method string, hasScreenshot bool, signatureVerified bool) string {
	switch {
	case method == "partner_api" && signatureVerified:
		return TierPartner
	case method == "browser_extension" && signatureVerified:
		return TierClient
	case hasScreenshot:
		return TierAttested
	default:
		return TierManual
	}
}

// TierCountsForReport tells whether a tier feeds metrics by default.
func TierCountsForReport(tier string) bool { return tier != TierManual }

// ValidateCapture enforces the field contract before any write happens.
func ValidateCapture(c CaptureInput, now time.Time) error {
	if strings.TrimSpace(c.CaptureID) == "" {
		return invalid("CAPTURE_ID_REQUIRED", "captureId is required")
	}
	if c.Channel != ChannelSearchAPI && c.Channel != ChannelConsumer {
		if c.Channel == "model_api" {
			return invalid("CHANNEL_NOT_CAPTURABLE", "model_api cannot be captured")
		}
		return invalid("CHANNEL_INVALID", "channel must be search_api or consumer_app_capture")
	}
	if strings.TrimSpace(c.Answer.Text) == "" {
		return invalid("ANSWER_EMPTY", "answer text is required")
	}
	if len([]rune(c.Answer.Text)) > 20000 {
		return invalid("ANSWER_TOO_LONG", "answer text exceeds 20000 characters")
	}
	if c.Surface.Provider == "" || c.Surface.Product == "" {
		return invalid("SURFACE_INVALID", "surface.provider and surface.product are required")
	}
	if !ValidMethod(c.Provenance.Method) {
		return invalid("PROVENANCE_INVALID", "provenance.method is not supported")
	}
	if strings.TrimSpace(c.Consent.Basis) == "" || strings.TrimSpace(c.Consent.Reference) == "" {
		return invalid("CONSENT_MISSING", "consent basis and reference are required")
	}
	if c.ObservedAt.IsZero() {
		return invalid("TIMESTAMP_REQUIRED", "observedAt is required")
	}
	if delta := now.Sub(c.ObservedAt); delta > 7*24*time.Hour || delta < -7*24*time.Hour {
		return invalid("TIMESTAMP_OUT_OF_RANGE", "observedAt must be within 7 days of server time")
	}
	for _, citation := range c.Citations {
		if citation.URL != "" && !strings.HasPrefix(citation.URL, "http://") && !strings.HasPrefix(citation.URL, "https://") {
			return invalid("CITATION_INVALID", "citation url must be http(s)")
		}
	}
	return nil
}

// IdempotencyKey implements the SPEC-11 fingerprint.
func IdempotencyKey(c CaptureInput) string {
	minute := c.ObservedAt.UTC().Truncate(time.Minute).Format(time.RFC3339)
	answerHash := sha256.Sum256([]byte(c.Answer.Text))
	raw := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%x", c.AuditID, c.QuestionID, c.Channel, c.Surface.Provider, c.Surface.Model, minute, answerHash)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// DecodeCaptures parses the raw captures array, keeping the raw bytes for signature checks.
func DecodeCaptures(raw json.RawMessage) ([]CaptureInput, error) {
	if len(raw) == 0 {
		return nil, invalid("CAPTURES_REQUIRED", "captures is required")
	}
	var items []CaptureInput
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, invalid("CAPTURES_INVALID", "captures is not a valid array")
	}
	if len(items) == 0 || len(items) > MaxCaptures {
		return nil, invalid("BATCH_TOO_LARGE", "batch must contain 1 to 100 captures")
	}
	return items, nil
}
