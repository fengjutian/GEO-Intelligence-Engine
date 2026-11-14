package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/reviews"
	"github.com/google/uuid"
)

type reviewStore interface {
	List(ctx context.Context, tenantID, status, priority string, limit int) ([]reviews.Item, error)
	Annotate(ctx context.Context, tenantID, itemID, annotatorID string, labels map[string]any, reason string) (reviews.Annotation, error)
	EnqueueManual(ctx context.Context, tenantID, evaluationID, reasonCode, priority string) error
	Assign(ctx context.Context, tenantID, itemID, assigneeID string) error
	Adjudicate(ctx context.Context, tenantID, itemID, adjudicatorID string, result map[string]any, reason string) (reviews.Adjudication, error)
	Stats(ctx context.Context, tenantID string) (map[string]any, error)
}

type reviewHandler struct {
	store       reviewStore
	devTenantID string
	logger      *slog.Logger
}

func newReviewHandler(store reviewStore, devTenantID string, logger *slog.Logger) *reviewHandler {
	return &reviewHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *reviewHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *reviewHandler) list(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	items, err := h.store.List(r.Context(), h.tenant(r), strings.TrimSpace(r.URL.Query().Get("status")), strings.TrimSpace(r.URL.Query().Get("priority")), limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *reviewHandler) enqueue(w http.ResponseWriter, r *http.Request) {
	var input struct {
		EvaluationID string `json:"evaluationId"`
		ReasonCode   string `json:"reasonCode"`
		Priority     string `json:"priority"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.EvaluationID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_EVALUATION_ID", "Evaluation ID is invalid", traceID(r))
		return
	}
	if err := h.store.EnqueueManual(r.Context(), h.tenant(r), input.EvaluationID, input.ReasonCode, input.Priority); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *reviewHandler) assign(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("reviewItemId")
	if _, err := uuid.Parse(itemID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_REVIEW_ITEM_ID", "Review item ID is invalid", traceID(r))
		return
	}
	var input struct {
		AssigneeID string `json:"assigneeId"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.AssigneeID); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "REVIEW_INPUT_INVALID", "Assignee ID is invalid", traceID(r))
		return
	}
	if err := h.store.Assign(r.Context(), h.tenant(r), itemID, input.AssigneeID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *reviewHandler) adjudicate(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("reviewItemId")
	if _, err := uuid.Parse(itemID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_REVIEW_ITEM_ID", "Review item ID is invalid", traceID(r))
		return
	}
	var input struct {
		Result map[string]any `json:"result"`
		Reason string         `json:"reason"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	adjudication, err := h.store.Adjudicate(r.Context(), h.tenant(r), itemID, r.Header.Get(headerActor), input.Result, input.Reason)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, adjudication)
}

func (h *reviewHandler) stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.Stats(r.Context(), h.tenant(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *reviewHandler) annotate(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("reviewItemId")
	if _, err := uuid.Parse(itemID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_REVIEW_ITEM_ID", "Review item ID is invalid", traceID(r))
		return
	}
	var input struct {
		Labels map[string]any `json:"labels"`
		Reason string         `json:"reason"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	annotation, err := h.store.Annotate(r.Context(), h.tenant(r), itemID, r.Header.Get(headerActor), input.Labels, input.Reason)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, annotation)
}

func (h *reviewHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, reviews.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "REVIEW_ITEM_NOT_FOUND", "Review item was not found", traceID(r))
	case errors.Is(err, reviews.ErrConflict):
		writeProblem(w, http.StatusConflict, "REVIEW_ITEM_NOT_PENDING", "Review item is not pending", traceID(r))
	case errors.Is(err, reviews.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "REVIEW_INPUT_INVALID", "Review labels and reason are required", traceID(r))
	default:
		h.logger.Error("review operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
