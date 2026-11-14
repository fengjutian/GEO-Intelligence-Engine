package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/fengjutian/geo-intelligence-engine/internal/members"
	"github.com/google/uuid"
)

type memberStore interface {
	List(ctx context.Context, tenantID string) ([]members.Member, error)
	Invite(ctx context.Context, tenantID, actorID string, input members.InviteInput) (members.Invitation, error)
	Update(ctx context.Context, tenantID, actorID, memberID string, input members.UpdateInput) (members.Member, error)
}

type memberHandler struct {
	store memberStore
}

func newMemberHandler(store memberStore) *memberHandler {
	return &memberHandler{store: store}
}

func (h *memberHandler) tenantMatches(r *http.Request) (string, bool) {
	tenantID := r.PathValue("tenantId")
	current := r.Header.Get(headerTenant)
	if current == "" {
		return "", false
	}
	if _, err := uuid.Parse(tenantID); err != nil || tenantID != current {
		return "", false
	}
	return tenantID, true
}

func (h *memberHandler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantMatches(r)
	if !ok {
		writeProblem(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant was not found", traceID(r))
		return
	}
	items, err := h.store.List(r.Context(), tenantID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": map[string]any{"hasMore": false}})
}

func (h *memberHandler) invite(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantMatches(r)
	if !ok {
		writeProblem(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant was not found", traceID(r))
		return
	}
	var input members.InviteInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	invitation, err := h.store.Invite(r.Context(), tenantID, r.Header.Get(headerActor), input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, invitation)
}

func (h *memberHandler) update(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantMatches(r)
	if !ok {
		writeProblem(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant was not found", traceID(r))
		return
	}
	memberID := r.PathValue("memberId")
	if _, err := uuid.Parse(memberID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_MEMBER_ID", "Member ID is invalid", traceID(r))
		return
	}
	var input members.UpdateInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	member, err := h.store.Update(r.Context(), tenantID, r.Header.Get(headerActor), memberID, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (h *memberHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, members.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "MEMBER_NOT_FOUND", "Member was not found", traceID(r))
	case errors.Is(err, members.ErrConflict):
		writeProblem(w, http.StatusConflict, "MEMBER_CONFLICT", "Member change would remove the last owner", traceID(r))
	case errors.Is(err, members.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "MEMBER_INPUT_INVALID", "Member input is invalid", traceID(r))
	default:
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
