package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/crawls"
	"github.com/google/uuid"
)

type crawlStore interface {
	PrimaryWebsiteID(ctx context.Context, tenantID, projectID string) (string, error)
	Create(ctx context.Context, tenantID, projectID string, input crawls.CreateInput) (crawls.Run, error)
	Get(ctx context.Context, tenantID, runID string) (crawls.Run, error)
	Claim(ctx context.Context, workerID string) (crawls.Claim, error)
	Complete(ctx context.Context, completion crawls.Completion) error
	Fail(ctx context.Context, failure crawls.Failure) error
	Control(ctx context.Context, tenantID, runID, action string) (crawls.Run, error)
	ListPages(ctx context.Context, tenantID, runID string) ([]crawls.PageSummary, error)
	GetPageContent(ctx context.Context, tenantID, contentID string) (crawls.PageDetail, error)
}

type crawlHandler struct {
	store       crawlStore
	devTenantID string
	workerToken string
	logger      *slog.Logger
}

func newCrawlHandler(store crawlStore, devTenantID, workerToken string, logger *slog.Logger) *crawlHandler {
	return &crawlHandler{store: store, devTenantID: devTenantID, workerToken: workerToken, logger: logger}
}

func (h *crawlHandler) create(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(tenantID); err != nil {
		writeProblem(w, http.StatusUnauthorized, "TENANT_CONTEXT_REQUIRED", "Valid tenant context is required", traceID(r))
		return
	}
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	var request struct {
		WebsiteID string   `json:"websiteId"`
		StartURLs []string `json:"startUrls"`
		Limits    struct {
			MaxPages    int `json:"maxPages"`
			MaxDepth    int `json:"maxDepth"`
			Concurrency int `json:"concurrency"`
		} `json:"limits"`
	}
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.WebsiteID == "" {
		websiteID, err := h.store.PrimaryWebsiteID(r.Context(), tenantID, projectID)
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		request.WebsiteID = websiteID
	}
	if _, err := uuid.Parse(request.WebsiteID); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_WEBSITE_ID", "Website ID is invalid", traceID(r))
		return
	}
	if request.Limits.MaxPages == 0 {
		request.Limits.MaxPages = 1
	}
	if request.Limits.MaxDepth < 0 || request.Limits.MaxDepth > 20 || request.Limits.MaxPages < 1 || request.Limits.MaxPages > 10000 {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_CRAWL_LIMITS", "Crawl limits are invalid", traceID(r))
		return
	}
	input := crawls.CreateInput{WebsiteID: request.WebsiteID, StartURLs: request.StartURLs, MaxPages: request.Limits.MaxPages, MaxDepth: request.Limits.MaxDepth, Concurrency: request.Limits.Concurrency}
	run, err := h.store.Create(r.Context(), tenantID, projectID, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/crawl-runs/"+run.ID)
	writeJSON(w, http.StatusAccepted, run)
}

func (h *crawlHandler) get(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	runID := r.PathValue("crawlRunId")
	if _, err := uuid.Parse(runID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_CRAWL_RUN_ID", "Crawl run ID is invalid", traceID(r))
		return
	}
	run, err := h.store.Get(r.Context(), tenantID, runID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *crawlHandler) controlAction(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("runAction")
	separator := strings.LastIndexByte(raw, ':')
	if separator < 1 || separator == len(raw)-1 {
		writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route was not found", traceID(r))
		return
	}
	runID, action := raw[:separator], raw[separator+1:]
	if _, err := uuid.Parse(runID); err != nil || (action != "pause" && action != "resume" && action != "cancel") {
		writeProblem(w, http.StatusBadRequest, "INVALID_CRAWL_ACTION", "Crawl action is invalid", traceID(r))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	run, err := h.store.Control(r.Context(), tenantID, runID, action)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (h *crawlHandler) listPages(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	runID := r.PathValue("crawlRunId")
	if _, err := uuid.Parse(runID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_CRAWL_RUN_ID", "Crawl run ID is invalid", traceID(r))
		return
	}
	pages, err := h.store.ListPages(r.Context(), tenantID, runID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": pages})
}

func (h *crawlHandler) getPageContent(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	contentID := r.PathValue("contentId")
	if _, err := uuid.Parse(contentID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_CONTENT_ID", "Page content ID is invalid", traceID(r))
		return
	}
	detail, err := h.store.GetPageContent(r.Context(), tenantID, contentID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *crawlHandler) claim(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeWorker(w, r) {
		return
	}
	workerID := strings.TrimSpace(r.Header.Get("X-Worker-ID"))
	if workerID == "" {
		workerID = "crawler-local"
	}
	claim, err := h.store.Claim(r.Context(), workerID)
	if errors.Is(err, crawls.ErrNoWork) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, claim)
}

func (h *crawlHandler) complete(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeWorker(w, r) {
		return
	}
	var input crawls.Completion
	if !decodeJSONBody(w, r, &input) {
		return
	}
	input.RunID = r.PathValue("crawlRunId")
	if err := h.store.Complete(r.Context(), input); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *crawlHandler) fail(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeWorker(w, r) {
		return
	}
	var input crawls.Failure
	if !decodeJSONBody(w, r, &input) {
		return
	}
	input.RunID = r.PathValue("crawlRunId")
	if err := h.store.Fail(r.Context(), input); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *crawlHandler) authorizeWorker(w http.ResponseWriter, r *http.Request) bool {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(provided) != len(h.workerToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(h.workerToken)) != 1 {
		writeProblem(w, http.StatusUnauthorized, "WORKER_UNAUTHORIZED", "Worker authentication failed", traceID(r))
		return false
	}
	return true
}

func (h *crawlHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, crawls.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "CRAWL_RESOURCE_NOT_FOUND", "Crawl resource was not found", traceID(r))
	case errors.Is(err, crawls.ErrProjectArchived):
		writeProblem(w, http.StatusConflict, "PROJECT_ARCHIVED", "Archived project cannot be crawled", traceID(r))
	case errors.Is(err, crawls.ErrInvalidState):
		writeProblem(w, http.StatusConflict, "INVALID_CRAWL_STATE", "Crawl action is not allowed in the current state", traceID(r))
	default:
		h.logger.Error("crawl operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
