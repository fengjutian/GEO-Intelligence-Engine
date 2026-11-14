package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/serviceaccounts"
	"github.com/google/uuid"
)

type serviceAccountStore interface {
	CreateAccount(ctx context.Context, tenantID, name string) (serviceaccounts.Account, error)
	ListAccounts(ctx context.Context, tenantID string) ([]serviceaccounts.Account, error)
	CreateToken(ctx context.Context, tenantID, accountID string, scopes []string, expiresInDays int) (serviceaccounts.Token, error)
	ListTokens(ctx context.Context, tenantID, accountID string) ([]serviceaccounts.Token, error)
	RevokeToken(ctx context.Context, tenantID, tokenID string) error
}

type serviceAccountHandler struct {
	store       serviceAccountStore
	devTenantID string
}

func newServiceAccountHandler(store serviceAccountStore, devTenantID string) *serviceAccountHandler {
	return &serviceAccountHandler{store: store, devTenantID: devTenantID}
}

func (h *serviceAccountHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *serviceAccountHandler) createAccount(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	account, err := h.store.CreateAccount(r.Context(), h.tenant(r), input.Name)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

func (h *serviceAccountHandler) listAccounts(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListAccounts(r.Context(), h.tenant(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *serviceAccountHandler) createToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ServiceAccountID string   `json:"serviceAccountId"`
		Scopes           []string `json:"scopes"`
		ExpiresInDays    int      `json:"expiresInDays"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.ServiceAccountID); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_SERVICE_ACCOUNT_ID", "Service account ID is invalid", traceID(r))
		return
	}
	token, err := h.store.CreateToken(r.Context(), h.tenant(r), input.ServiceAccountID, input.Scopes, input.ExpiresInDays)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, token)
}

func (h *serviceAccountHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.URL.Query().Get("serviceAccountId"))
	if accountID != "" {
		if _, err := uuid.Parse(accountID); err != nil {
			writeProblem(w, http.StatusBadRequest, "INVALID_SERVICE_ACCOUNT_ID", "Service account ID is invalid", traceID(r))
			return
		}
	}
	items, err := h.store.ListTokens(r.Context(), h.tenant(r), accountID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *serviceAccountHandler) revokeToken(w http.ResponseWriter, r *http.Request) {
	tokenID := r.PathValue("tokenId")
	if _, err := uuid.Parse(tokenID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_TOKEN_ID", "Token ID is invalid", traceID(r))
		return
	}
	if err := h.store.RevokeToken(r.Context(), h.tenant(r), tokenID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *serviceAccountHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, serviceaccounts.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "SERVICE_ACCOUNT_NOT_FOUND", "Service account resource was not found", traceID(r))
	case errors.Is(err, serviceaccounts.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "SERVICE_ACCOUNT_INVALID", "Service account input is invalid", traceID(r))
	default:
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
