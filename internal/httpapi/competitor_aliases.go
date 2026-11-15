package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/competitoraliases"
	"github.com/google/uuid"
)

type competitorAliasStore interface {
	List(ctx context.Context, tenantID, competitorID string) ([]competitoraliases.Alias, error)
	Create(ctx context.Context, tenantID, competitorID, alias string) (competitoraliases.Alias, error)
	Delete(ctx context.Context, tenantID, competitorID, aliasID string) error
}

type competitorAliasHandler struct {
	store       competitorAliasStore
	devTenantID string
}

func newCompetitorAliasHandler(store competitorAliasStore, devTenantID string) *competitorAliasHandler {
	return &competitorAliasHandler{store: store, devTenantID: devTenantID}
}

func (h *competitorAliasHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *competitorAliasHandler) list(w http.ResponseWriter, r *http.Request) {
	competitorID := r.PathValue("competitorId")
	if _, err := uuid.Parse(competitorID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_COMPETITOR_ID", "Competitor ID is invalid", traceID(r))
		return
	}
	items, err := h.store.List(r.Context(), h.tenant(r), competitorID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *competitorAliasHandler) create(w http.ResponseWriter, r *http.Request) {
	competitorID := r.PathValue("competitorId")
	if _, err := uuid.Parse(competitorID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_COMPETITOR_ID", "Competitor ID is invalid", traceID(r))
		return
	}
	var input struct {
		Alias string `json:"alias"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	item, err := h.store.Create(r.Context(), h.tenant(r), competitorID, input.Alias)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *competitorAliasHandler) remove(w http.ResponseWriter, r *http.Request) {
	competitorID := r.PathValue("competitorId")
	aliasID := r.PathValue("aliasId")
	if _, err := uuid.Parse(competitorID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_COMPETITOR_ID", "Competitor ID is invalid", traceID(r))
		return
	}
	if _, err := uuid.Parse(aliasID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ALIAS_ID", "Alias ID is invalid", traceID(r))
		return
	}
	if err := h.store.Delete(r.Context(), h.tenant(r), competitorID, aliasID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *competitorAliasHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, competitoraliases.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "COMPETITOR_ALIAS_NOT_FOUND", "Competitor or alias was not found", traceID(r))
	case errors.Is(err, competitoraliases.ErrConflict):
		writeProblem(w, http.StatusConflict, "COMPETITOR_ALIAS_CONFLICT", "Competitor alias already exists", traceID(r))
	case errors.Is(err, competitoraliases.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "COMPETITOR_ALIAS_INVALID", "Competitor alias is invalid", traceID(r))
	default:
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
