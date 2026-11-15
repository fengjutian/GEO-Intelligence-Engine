package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/fengjutian/geo-intelligence-engine/internal/factcheck"
	"github.com/google/uuid"
)

type factCheckStore interface {
	Verify(ctx context.Context, tenantID, analysisRunID string) (factcheck.Run, error)
	ListChecks(ctx context.Context, tenantID, projectID, status string, limit int) ([]factcheck.Check, error)
	UpdateCheckStatus(ctx context.Context, tenantID, checkID, status string) (factcheck.Check, error)
}

type factCheckHandler struct {
	store       factCheckStore
	devTenantID string
	logger      *slog.Logger
}

func newFactCheckHandler(store factCheckStore, devTenantID string, logger *slog.Logger) *factCheckHandler {
	return &factCheckHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *factCheckHandler) tenant(r *http.Request) string {
	if value := r.Header.Get(headerTenant); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *factCheckHandler) verify(w http.ResponseWriter, r *http.Request) {
	analysisRunID := r.PathValue("analysisRunId")
	if _, err := uuid.Parse(analysisRunID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ANALYSIS_RUN_ID", "Analysis run ID is invalid", traceID(r))
		return
	}
	run, err := h.store.Verify(r.Context(), h.tenant(r), analysisRunID)
	if err != nil {
		switch {
		case errors.Is(err, factcheck.ErrNotFound):
			writeProblem(w, http.StatusNotFound, "ANALYSIS_RUN_NOT_FOUND", "Content analysis run was not found", traceID(r))
		case errors.Is(err, factcheck.ErrNoContent):
			writeProblem(w, http.StatusUnprocessableEntity, "NO_CONTENT_TO_VERIFY", "No page text is available for fact checking", traceID(r))
		default:
			h.logger.Error("fact check failed", "error", err, "trace_id", traceID(r))
			writeProblem(w, http.StatusInternalServerError, "FACT_CHECK_FAILED", "Fact check request failed", traceID(r))
		}
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (h *factCheckHandler) list(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	items, err := h.store.ListChecks(r.Context(), h.tenant(r), projectID, r.URL.Query().Get("status"), limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *factCheckHandler) update(w http.ResponseWriter, r *http.Request) {
	checkID := r.PathValue("checkId")
	if _, err := uuid.Parse(checkID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_CHECK_ID", "Fact check ID is invalid", traceID(r))
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	item, err := h.store.UpdateCheckStatus(r.Context(), h.tenant(r), checkID, input.Status)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "FACT_CHECK_STATUS_INVALID", "Status must be confirmed or dismissed", traceID(r))
		return
	}
	writeJSON(w, http.StatusOK, item)
}
