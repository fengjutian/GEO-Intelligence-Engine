package publish

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// SignatureHeader carries the HMAC of the exact request body.
const SignatureHeader = "X-Geo-Signature"

// EventHeader names the delivery kind.
const EventHeader = "X-Geo-Event"

// Event is one write-back delivery.
type Event struct {
	ID              string    `json:"id"`
	DraftID         string    `json:"draftId"`
	Action          string    `json:"action"`
	Status          string    `json:"status"`
	HTTPStatus      int       `json:"httpStatus"`
	ErrorCode       string    `json:"errorCode,omitempty"`
	ResponseSnippet string    `json:"responseSnippet,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

// Target is the configured webhook receiver.
type Target struct {
	Endpoint  string    `json:"endpoint"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Payload is what we POST to the customer endpoint.
type Payload struct {
	Event       string         `json:"event"`
	ProjectID   string         `json:"projectId"`
	DraftID     string         `json:"draftId"`
	Kind        string         `json:"kind"`
	Title       string         `json:"title"`
	Version     int            `json:"version"`
	Content     map[string]any `json:"content"`
	PublishedAt string         `json:"publishedAt"`
	Reason      string         `json:"reason,omitempty"`
}

// Sign returns the hex HMAC-SHA256 of the body using the shared secret.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignedValue is the header value receivers should compare against.
func SignedValue(secret string, body []byte) string {
	return "sha256=" + Sign(secret, body)
}

// BuildPayload assembles the delivery body for one draft version.
func BuildPayload(event, projectID, draftID, kind, title string, version int, content map[string]any, now time.Time) Payload {
	return Payload{
		Event: event, ProjectID: projectID, DraftID: draftID, Kind: kind, Title: title,
		Version: version, Content: content, PublishedAt: now.UTC().Format(time.RFC3339),
	}
}

// Encode serializes a payload deterministically for signing and sending.
func Encode(payload Payload) ([]byte, error) {
	return json.Marshal(payload)
}

// ValidateEndpoint rejects anything that is not a plain public webhook URL.
func ValidateEndpoint(endpoint string) error {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return &TargetError{"endpoint is required"}
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		return &TargetError{"endpoint must be http(s)"}
	}
	return nil
}

// TargetError marks an invalid publish target.
type TargetError struct{ Message string }

func (e *TargetError) Error() string { return e.Message }
