package publish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/drafts"
	"github.com/google/uuid"
)

// ErrTargetMissing marks a project without a publish target.
var ErrTargetMissing = errors.New("publish target is not configured")

// ErrNotApproved marks a draft that has not passed review.
var ErrNotApproved = errors.New("draft must be approved before publishing")

// ErrEventNotFound marks a missing publish event.
var ErrEventNotFound = errors.New("publish event was not found")

// ErrNotDelivered marks a rollback on an undelivered publish.
var ErrNotDelivered = errors.New("only delivered publishes can be rolled back")

type Store struct {
	db     *sql.DB
	drafts *drafts.Store
	client *http.Client
}

func NewStore(db *sql.DB, draftStore *drafts.Store) *Store {
	return &Store{
		db: db, drafts: draftStore,
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// SetTarget upserts the project webhook.
func (s *Store) SetTarget(ctx context.Context, tenantID, projectID, endpoint string, enabled bool) (Target, error) {
	endpoint = strings.TrimSpace(endpoint)
	if err := ValidateEndpoint(endpoint); err != nil {
		return Target{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO publish_targets (id,tenant_id,project_id,endpoint,enabled,created_at,updated_at)
VALUES (UUID_TO_BIN(UUID()),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE endpoint=VALUES(endpoint),enabled=VALUES(enabled),updated_at=UTC_TIMESTAMP(6)`,
		tenantID, projectID, endpoint, enabled); err != nil {
		return Target{}, err
	}
	return s.GetTarget(ctx, tenantID, projectID)
}

// GetTarget returns the configured webhook.
func (s *Store) GetTarget(ctx context.Context, tenantID, projectID string) (Target, error) {
	var target Target
	err := s.db.QueryRowContext(ctx, `SELECT endpoint,enabled,updated_at FROM publish_targets WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`,
		tenantID, projectID).Scan(&target.Endpoint, &target.Enabled, &target.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, ErrTargetMissing
	}
	return target, err
}

// Publish delivers an approved draft to the project's webhook.
func (s *Store) Publish(ctx context.Context, tenantID, projectID, draftID string) (Event, error) {
	secret := strings.TrimSpace(os.Getenv("GEO_PUBLISH_SECRET"))
	if secret == "" {
		return Event{}, errors.New("GEO_PUBLISH_SECRET is not configured")
	}
	target, err := s.GetTarget(ctx, tenantID, projectID)
	if err != nil {
		return Event{}, err
	}
	if !target.Enabled {
		return Event{}, ErrTargetMissing
	}
	draft, err := s.drafts.Get(ctx, tenantID, projectID, draftID)
	if err != nil {
		return Event{}, err
	}
	if draft.Status != "approved" {
		return Event{}, ErrNotApproved
	}
	payload := BuildPayload("publish", projectID, draftID, draft.Kind, draft.Title, draft.Version+1, draft.Payload, time.Now())
	event, err := s.deliver(ctx, tenantID, projectID, draftID, target, payload, secret)
	if err != nil {
		return Event{}, err
	}
	if event.Status == "delivered" {
		if _, err := s.drafts.SetStatus(ctx, tenantID, projectID, draftID, "published"); err != nil {
			return Event{}, err
		}
	}
	return event, nil
}

// Rollback notifies the endpoint that a previously published draft must be reverted.
func (s *Store) Rollback(ctx context.Context, tenantID, projectID, eventID, reason string) (Event, error) {
	secret := strings.TrimSpace(os.Getenv("GEO_PUBLISH_SECRET"))
	if secret == "" {
		return Event{}, errors.New("GEO_PUBLISH_SECRET is not configured")
	}
	var draftID, action, status, targetEndpoint string
	var targetID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(e.draft_id),e.action,e.status,BIN_TO_UUID(e.target_id),t.endpoint
FROM publish_events e JOIN publish_targets t ON t.id=e.target_id
WHERE e.tenant_id=UUID_TO_BIN(?) AND e.project_id=UUID_TO_BIN(?) AND e.id=UUID_TO_BIN(?)`,
		tenantID, projectID, eventID).Scan(&draftID, &action, &status, &targetID, &targetEndpoint)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrEventNotFound
	}
	if err != nil {
		return Event{}, err
	}
	if action != "publish" || status != "delivered" {
		return Event{}, ErrNotDelivered
	}
	draft, err := s.drafts.Get(ctx, tenantID, projectID, draftID)
	if err != nil {
		return Event{}, err
	}
	payload := BuildPayload("rollback", projectID, draftID, draft.Kind, draft.Title, draft.Version, draft.Payload, time.Now())
	payload.Reason = strings.TrimSpace(reason)
	target := Target{Endpoint: targetEndpoint, Enabled: true}
	return s.deliver(ctx, tenantID, projectID, draftID, target, payload, secret)
}

func (s *Store) deliver(ctx context.Context, tenantID, projectID, draftID string, target Target, payload Payload, secret string) (Event, error) {
	body, err := Encode(payload)
	if err != nil {
		return Event{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Event{}, err
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set(SignatureHeader, SignedValue(secret, body))
	request.Header.Set(EventHeader, payload.Event)
	event := Event{DraftID: draftID, Action: payload.Event, Status: "failed"}
	response, err := s.client.Do(request)
	if err != nil {
		event.ErrorCode = "DELIVERY_FAILED"
		event.ResponseSnippet = truncateString(err.Error(), 500)
	} else {
		defer response.Body.Close()
		event.HTTPStatus = response.StatusCode
		snippet, _ := io.ReadAll(io.LimitReader(response.Body, 500))
		event.ResponseSnippet = truncateString(string(snippet), 500)
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			event.Status = "delivered"
		} else {
			event.ErrorCode = "NON_2XX"
		}
	}
	hash := sha256.Sum256(body)
	event.ID = uuid.NewString()
	event.CreatedAt = time.Now().UTC()
	var targetID string
	if err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM publish_targets WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`, tenantID, projectID).Scan(&targetID); err != nil {
		return Event{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO publish_events (id,tenant_id,project_id,draft_id,target_id,action,status,http_status,error_code,payload_hash,response_snippet,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,UTC_TIMESTAMP(6))`,
		event.ID, tenantID, projectID, draftID, targetID, event.Action, event.Status, nullInt(event.HTTPStatus), nullString(event.ErrorCode), hash[:], nullString(event.ResponseSnippet)); err != nil {
		return Event{}, err
	}
	return event, nil
}

// ListEvents returns recent write-back deliveries.
func (s *Store) ListEvents(ctx context.Context, tenantID, projectID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(draft_id),action,status,COALESCE(http_status,0),COALESCE(error_code,''),COALESCE(response_snippet,''),created_at
FROM publish_events WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at DESC LIMIT 100`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Event{}
	for rows.Next() {
		var item Event
		if err := rows.Scan(&item.ID, &item.DraftID, &item.Action, &item.Status, &item.HTTPStatus, &item.ErrorCode, &item.ResponseSnippet, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func truncateString(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
