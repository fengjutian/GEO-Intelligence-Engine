package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/drafts"
	"github.com/fengjutian/geo-intelligence-engine/internal/experiments"
	"github.com/fengjutian/geo-intelligence-engine/internal/signals"
	"github.com/google/uuid"
)

type signalStore interface {
	IngestBotLogs(ctx context.Context, tenantID, projectID string, inputs []signals.BotLogInput) (int, error)
	BotSummary(ctx context.Context, tenantID, projectID string, days int) ([]signals.BotStat, error)
	IngestMentions(ctx context.Context, tenantID, projectID string, inputs []signals.MentionInput) (int, error)
	ListMentions(ctx context.Context, tenantID, projectID, sourceType string, limit int) ([]signals.Mention, error)
	MentionSummary(ctx context.Context, tenantID, projectID string) ([]signals.SourceTypeCount, error)
	IngestAccessLog(ctx context.Context, tenantID, projectID, content string) (signals.IngestResult, error)
	Coverage(ctx context.Context, tenantID, projectID string, days int) (signals.CoverageReport, error)
	DiscoverMentions(ctx context.Context, tenantID, projectID string, urls []string) (signals.DiscoverResult, error)
}

type signalHandler struct {
	store       signalStore
	devTenantID string
	logger      *slog.Logger
}

func newSignalHandler(store signalStore, devTenantID string, logger *slog.Logger) *signalHandler {
	return &signalHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *signalHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *signalHandler) projectID(w http.ResponseWriter, r *http.Request) (string, bool) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return "", false
	}
	return projectID, true
}

func (h *signalHandler) ingestBotLogs(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	var input struct {
		Events []signals.BotLogInput `json:"events"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	inserted, err := h.store.IngestBotLogs(r.Context(), h.tenant(r), projectID, input.Events)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"inserted": inserted})
}

func (h *signalHandler) botSummary(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	stats, err := h.store.BotSummary(r.Context(), h.tenant(r), projectID, days)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	aiHits := 0
	for _, item := range stats {
		if item.IsAIBot {
			aiHits += item.Hits
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": stats, "aiHits": aiHits})
}

func (h *signalHandler) ingestMentions(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	var input struct {
		Mentions []signals.MentionInput `json:"mentions"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	inserted, err := h.store.IngestMentions(r.Context(), h.tenant(r), projectID, input.Mentions)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"inserted": inserted})
}

func (h *signalHandler) listMentions(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.store.ListMentions(r.Context(), h.tenant(r), projectID, strings.TrimSpace(r.URL.Query().Get("sourceType")), limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *signalHandler) mentionSummary(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	counts, err := h.store.MentionSummary(r.Context(), h.tenant(r), projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": counts})
}

func (h *signalHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, signals.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "SIGNAL_INPUT_INVALID", "Signal input is invalid", traceID(r))
	default:
		h.logger.Error("signal operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}

type experimentStore interface {
	Create(ctx context.Context, tenantID, projectID string, input experiments.CreateInput) (experiments.Experiment, error)
	List(ctx context.Context, tenantID, projectID string) ([]experiments.Experiment, error)
	Get(ctx context.Context, tenantID, projectID, id string) (experiments.Experiment, error)
	Attach(ctx context.Context, tenantID, projectID, id, baselineAuditID, treatmentAuditID string) (experiments.Experiment, error)
	Evaluate(ctx context.Context, tenantID, projectID, id string) (experiments.Experiment, error)
}

type experimentHandler struct {
	store       experimentStore
	devTenantID string
	logger      *slog.Logger
}

func newExperimentHandler(store experimentStore, devTenantID string, logger *slog.Logger) *experimentHandler {
	return &experimentHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *experimentHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *experimentHandler) create(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	var input experiments.CreateInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	item, err := h.store.Create(r.Context(), h.tenant(r), projectID, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *experimentHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.List(r.Context(), h.tenant(r), r.PathValue("projectId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *experimentHandler) get(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.Get(r.Context(), h.tenant(r), r.PathValue("projectId"), r.PathValue("experimentId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *experimentHandler) attach(w http.ResponseWriter, r *http.Request) {
	var input struct {
		BaselineAuditID  string `json:"baselineAuditId"`
		TreatmentAuditID string `json:"treatmentAuditId"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	item, err := h.store.Attach(r.Context(), h.tenant(r), r.PathValue("projectId"), r.PathValue("experimentId"), input.BaselineAuditID, input.TreatmentAuditID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *experimentHandler) evaluate(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.Evaluate(r.Context(), h.tenant(r), r.PathValue("projectId"), r.PathValue("experimentId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *experimentHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, experiments.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "EXPERIMENT_NOT_FOUND", "Experiment was not found", traceID(r))
	case errors.Is(err, experiments.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "EXPERIMENT_INPUT_INVALID", "Experiment input is invalid", traceID(r))
	case errors.Is(err, experiments.ErrConflict):
		writeProblem(w, http.StatusConflict, "EXPERIMENT_NOT_RUNNABLE", "Experiment is not runnable", traceID(r))
	default:
		h.logger.Error("experiment operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}

type draftStore interface {
	Generate(ctx context.Context, tenantID, projectID, kind string) (drafts.Draft, error)
	List(ctx context.Context, tenantID, projectID, kind string) ([]drafts.Draft, error)
	Get(ctx context.Context, tenantID, projectID, id string) (drafts.Draft, error)
	SetStatus(ctx context.Context, tenantID, projectID, id, status string) (drafts.Draft, error)
	Changes(ctx context.Context, tenantID, projectID, fromID, toID string) ([]drafts.Change, error)
}

type draftHandler struct {
	store       draftStore
	devTenantID string
	logger      *slog.Logger
}

func newDraftHandler(store draftStore, devTenantID string, logger *slog.Logger) *draftHandler {
	return &draftHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *draftHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

func (h *draftHandler) generate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Kind string `json:"kind"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	item, err := h.store.Generate(r.Context(), h.tenant(r), r.PathValue("projectId"), strings.TrimSpace(input.Kind))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *draftHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.List(r.Context(), h.tenant(r), r.PathValue("projectId"), strings.TrimSpace(r.URL.Query().Get("kind")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *draftHandler) get(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.Get(r.Context(), h.tenant(r), r.PathValue("projectId"), r.PathValue("draftId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *draftHandler) setStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	item, err := h.store.SetStatus(r.Context(), h.tenant(r), r.PathValue("projectId"), r.PathValue("draftId"), strings.TrimSpace(input.Status))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *draftHandler) diff(w http.ResponseWriter, r *http.Request) {
	against := strings.TrimSpace(r.URL.Query().Get("against"))
	if _, err := uuid.Parse(against); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_DRAFT_ID", "Against draft ID is invalid", traceID(r))
		return
	}
	changes, err := h.store.Changes(r.Context(), h.tenant(r), r.PathValue("projectId"), against, r.PathValue("draftId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": changes})
}

func (h *draftHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, drafts.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "Draft was not found", traceID(r))
	case errors.Is(err, drafts.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "DRAFT_INPUT_INVALID", "Draft kind is invalid", traceID(r))
	case errors.Is(err, drafts.ErrConflict):
		writeProblem(w, http.StatusConflict, "DRAFT_STATUS_INVALID", "Draft status transition is not allowed", traceID(r))
	default:
		h.logger.Error("draft operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}

func (h *signalHandler) ingestLogText(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	var input struct {
		Content string `json:"content"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	result, err := h.store.IngestAccessLog(r.Context(), h.tenant(r), projectID, input.Content)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *signalHandler) coverage(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	report, err := h.store.Coverage(r.Context(), h.tenant(r), projectID, days)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *signalHandler) discoverMentions(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	var input struct {
		URLs []string `json:"urls"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	result, err := h.store.DiscoverMentions(r.Context(), h.tenant(r), projectID, input.URLs)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
