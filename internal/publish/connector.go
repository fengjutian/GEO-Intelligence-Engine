package publish

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/drafts"
	"github.com/google/uuid"
)

// Connector kinds supported by publish targets.
const (
	KindWebhook    = "webhook"
	KindFilesystem = "filesystem"
)

// PublishRoot returns the directory filesystem connectors may write into.
func PublishRoot() string {
	root := strings.TrimSpace(os.Getenv("GEO_PUBLISH_ROOT"))
	if root == "" {
		root = ".run/publish-out"
	}
	return root
}

// ValidateFilesystemPath rejects paths outside the configured publish root.
func ValidateFilesystemPath(path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || clean == "." {
		return &TargetError{"filesystem path is required"}
	}
	if strings.Contains(clean, "..") {
		return &TargetError{"filesystem path must not contain .."}
	}
	return nil
}

func connectorExtension(kind string) string {
	switch kind {
	case "jsonld":
		return "json"
	case "llms_txt":
		return "txt"
	default:
		return "md"
	}
}

// RenderDraft renders the draft as the file content a static site would consume.
func RenderDraft(draft drafts.Draft) ([]byte, error) {
	if draft.Kind == "llms_txt" {
		for _, key := range []string{"content", "text"} {
			if text, ok := draft.Payload[key].(string); ok && strings.TrimSpace(text) != "" {
				return []byte(text), nil
			}
		}
	}
	return json.MarshalIndent(draft.Payload, "", "  ")
}

func (s *Store) deliverFilesystem(ctx context.Context, tenantID, projectID, draftID string, target Target, draft drafts.Draft) (Event, error) {
	root := PublishRoot()
	dir := filepath.Join(root, projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Event{}, err
	}
	path := filepath.Join(dir, draft.Kind+"."+connectorExtension(draft.Kind))
	body, err := RenderDraft(draft)
	if err != nil {
		return Event{}, err
	}
	event := Event{DraftID: draftID, Action: "publish", Status: "delivered"}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		event.Status = "failed"
		event.ErrorCode = "WRITE_FAILED"
		event.ResponseSnippet = truncateString(err.Error(), 500)
	}
	hash := sha256.Sum256(body)
	return s.recordEvent(ctx, tenantID, projectID, draftID, event, path, hash[:])
}

func (s *Store) rollbackFilesystem(ctx context.Context, tenantID, projectID, draftID string) (Event, error) {
	root := PublishRoot()
	event := Event{DraftID: draftID, Action: "rollback", Status: "delivered"}
	var kind string
	if err := s.db.QueryRowContext(ctx, `SELECT kind FROM content_drafts WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`,
		tenantID, projectID, draftID).Scan(&kind); err != nil {
		event.Status = "failed"
		event.ErrorCode = "DRAFT_LOOKUP_FAILED"
		return s.recordEvent(ctx, tenantID, projectID, draftID, event, "", nil)
	}
	path := filepath.Join(root, projectID, kind+"."+connectorExtension(kind))
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		event.Status = "failed"
		event.ErrorCode = "REMOVE_FAILED"
		event.ResponseSnippet = truncateString(err.Error(), 500)
	}
	return s.recordEvent(ctx, tenantID, projectID, draftID, event, path, nil)
}

func (s *Store) recordEvent(ctx context.Context, tenantID, projectID, draftID string, event Event, path string, payloadHash []byte) (Event, error) {
	targetID, err := s.targetID(ctx, tenantID, projectID)
	if err != nil {
		return Event{}, err
	}
	event.ID = uuid.NewString()
	event.CreatedAt = time.Now().UTC()
	event.ResponseSnippet = truncateString(strings.TrimSpace(event.ResponseSnippet+" written:"+path), 500)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO publish_events (id,tenant_id,project_id,draft_id,target_id,action,status,http_status,error_code,payload_hash,response_snippet,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,UTC_TIMESTAMP(6))`,
		event.ID, tenantID, projectID, draftID, targetID, event.Action, event.Status,
		nullInt(event.HTTPStatus), nullString(event.ErrorCode), payloadHash, nullString(event.ResponseSnippet)); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (s *Store) targetID(ctx context.Context, tenantID, projectID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM publish_targets WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`,
		tenantID, projectID).Scan(&id)
	return id, err
}
