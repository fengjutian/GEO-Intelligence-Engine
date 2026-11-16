package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/evidencechain"
	"github.com/google/uuid"
)

type evidenceChainStore interface {
	Get(ctx context.Context, tenantID, findingID string) (evidencechain.Chain, error)
	Upsert(ctx context.Context, tenantID, actorID, findingID string, input evidencechain.UpsertInput) (evidencechain.Chain, error)
}

type evidenceChainHandler struct {
	store       evidenceChainStore
	devTenantID string
	devActorID  string
	logger      *slog.Logger
}

func newEvidenceChainHandler(store evidenceChainStore, devTenantID, devActorID string, logger *slog.Logger) *evidenceChainHandler {
	return &evidenceChainHandler{store: store, devTenantID: devTenantID, devActorID: devActorID, logger: logger}
}

func (h *evidenceChainHandler) get(w http.ResponseWriter, r *http.Request) {
	findingID := r.PathValue("findingId")
	if _, err := uuid.Parse(findingID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_FINDING_ID", "Finding ID is invalid", traceID(r))
		return
	}
	chain, err := h.store.Get(r.Context(), h.tenant(r), findingID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, chain)
}

func (h *evidenceChainHandler) upsert(w http.ResponseWriter, r *http.Request) {
	findingID := r.PathValue("findingId")
	if _, err := uuid.Parse(findingID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_FINDING_ID", "Finding ID is invalid", traceID(r))
		return
	}
	var input evidencechain.UpsertInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	for _, value := range []string{input.QuestionID, input.ResponseID, input.PageContentID, input.TaskID, input.RetestAuditID} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "INVALID_EVIDENCE_REFERENCE", "Evidence references must be valid IDs", traceID(r))
			return
		}
	}
	chain, err := h.store.Upsert(r.Context(), h.tenant(r), h.actor(r), findingID, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, chain)
}

func (h *evidenceChainHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *evidenceChainHandler) actor(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Actor-ID")); value != "" {
		return value
	}
	return h.devActorID
}

func (h *evidenceChainHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, evidencechain.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "EVIDENCE_CHAIN_NOT_FOUND", "Finding or evidence chain was not found", traceID(r))
	case errors.Is(err, evidencechain.ErrInvalidInput):
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_EVIDENCE_CHAIN", "Question, answer, change, expected outcome and verification plan are required", traceID(r))
	case errors.Is(err, evidencechain.ErrInvalidReference):
		writeProblem(w, http.StatusConflict, "EVIDENCE_REFERENCE_MISMATCH", "Evidence must belong to the same tenant, project and question", traceID(r))
	default:
		h.logger.Error("evidence chain request failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Evidence chain request failed", traceID(r))
	}
}
