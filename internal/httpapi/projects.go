package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/projects"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type projectStore interface {
	Create(ctx context.Context, tenantID string, input projects.CreateInput) (projects.Project, error)
	Get(ctx context.Context, tenantID, projectID string) (projects.Project, error)
	List(ctx context.Context, tenantID string, limit int) (projects.Page, error)
	Update(ctx context.Context, tenantID, projectID string, version uint64, input projects.PatchInput) (projects.Project, error)
	Archive(ctx context.Context, tenantID, projectID string, version uint64) (projects.JobRef, error)
}

type projectHandler struct {
	store       projectStore
	devTenantID string
	logger      *slog.Logger
}

func newProjectHandler(store projectStore, devTenantID string, logger *slog.Logger) *projectHandler {
	return &projectHandler{store: store, devTenantID: devTenantID, logger: logger}
}

func (h *projectHandler) create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input projects.CreateInput
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid", traceID(r))
		return
	}
	input = normalizeProjectInput(input)
	if err := validateProjectInput(input); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), traceID(r))
		return
	}
	project, err := h.store.Create(r.Context(), tenantID, input)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			writeProblem(w, http.StatusConflict, "PROJECT_NAME_CONFLICT", "Project name already exists", traceID(r))
			return
		}
		h.internalError(w, r, "create project", err)
		return
	}
	w.Header().Set("Location", "/api/v1/projects/"+project.ID)
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, project.Version))
	writeJSON(w, http.StatusCreated, project)
}

func (h *projectHandler) get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	project, err := h.store.Get(r.Context(), tenantID, projectID)
	if errors.Is(err, projects.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project was not found", traceID(r))
		return
	}
	if err != nil {
		h.internalError(w, r, "get project", err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, project.Version))
	writeJSON(w, http.StatusOK, project)
}

func (h *projectHandler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return
	}
	limit := 20
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(w, http.StatusBadRequest, "INVALID_LIMIT", "Limit must be between 1 and 100", traceID(r))
			return
		}
		limit = parsed
	}
	page, err := h.store.List(r.Context(), tenantID, limit)
	if err != nil {
		h.internalError(w, r, "list projects", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *projectHandler) update(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, version, ok := h.mutationContext(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input projects.PatchInput
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_JSON", "Request body is invalid", traceID(r))
		return
	}
	if input.Name == nil && input.Locale == nil && input.Region == nil && input.Timezone == nil {
		writeProblem(w, http.StatusUnprocessableEntity, "EMPTY_PATCH", "At least one project field is required", traceID(r))
		return
	}
	createInput := projects.CreateInput{Name: "placeholder", BrandName: "placeholder", OfficialURL: "https://example.com", Locale: "en", Region: "global", Timezone: "UTC"}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		input.Name, createInput.Name = &value, value
	}
	if input.Locale != nil {
		value := strings.TrimSpace(*input.Locale)
		input.Locale, createInput.Locale = &value, value
	}
	if input.Region != nil {
		value := strings.TrimSpace(*input.Region)
		input.Region, createInput.Region = &value, value
	}
	if input.Timezone != nil {
		value := strings.TrimSpace(*input.Timezone)
		input.Timezone, createInput.Timezone = &value, value
	}
	if err := validateProjectInput(createInput); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), traceID(r))
		return
	}
	project, err := h.store.Update(r.Context(), tenantID, projectID, version, input)
	if h.writeMutationError(w, r, err) {
		return
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, project.Version))
	writeJSON(w, http.StatusOK, project)
}

func (h *projectHandler) archive(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, version, ok := h.mutationContext(w, r)
	if !ok {
		return
	}
	job, err := h.store.Archive(r.Context(), tenantID, projectID, version)
	if h.writeMutationError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *projectHandler) archiveAction(w http.ResponseWriter, r *http.Request) {
	projectID, ok := strings.CutSuffix(r.PathValue("projectAction"), ":archive")
	if !ok || projectID == "" {
		writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route was not found", traceID(r))
		return
	}
	r.SetPathValue("projectId", projectID)
	h.archive(w, r)
}

func (h *projectHandler) mutationContext(w http.ResponseWriter, r *http.Request) (string, string, uint64, bool) {
	tenantID, ok := h.tenantID(w, r)
	if !ok {
		return "", "", 0, false
	}
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return "", "", 0, false
	}
	versionText := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`)
	if versionText == "" {
		writeProblem(w, http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "If-Match is required", traceID(r))
		return "", "", 0, false
	}
	version, err := strconv.ParseUint(versionText, 10, 64)
	if err != nil || version < 1 {
		writeProblem(w, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must contain the project version", traceID(r))
		return "", "", 0, false
	}
	return tenantID, projectID, version, true
}

func (h *projectHandler) writeMutationError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, projects.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project was not found", traceID(r))
	case errors.Is(err, projects.ErrVersionConflict):
		writeProblem(w, http.StatusConflict, "VERSION_CONFLICT", "Project has been modified; reload and retry", traceID(r))
	case errors.Is(err, projects.ErrArchived):
		writeProblem(w, http.StatusConflict, "PROJECT_ARCHIVED", "Archived project cannot be modified", traceID(r))
	default:
		h.internalError(w, r, "mutate project", err)
	}
	return true
}

func (h *projectHandler) tenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		writeProblem(w, http.StatusUnauthorized, "TENANT_CONTEXT_REQUIRED", "Valid tenant context is required", traceID(r))
		return "", false
	}
	return tenantID, true
}

func (h *projectHandler) internalError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err, "trace_id", traceID(r))
	writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
}

func normalizeProjectInput(input projects.CreateInput) projects.CreateInput {
	input.Name = strings.TrimSpace(input.Name)
	input.BrandName = strings.TrimSpace(input.BrandName)
	input.OfficialURL = strings.TrimSpace(input.OfficialURL)
	input.Locale = strings.TrimSpace(input.Locale)
	input.Region = strings.TrimSpace(input.Region)
	input.Timezone = strings.TrimSpace(input.Timezone)
	return input
}

func validateProjectInput(input projects.CreateInput) error {
	if input.Name == "" || len([]rune(input.Name)) > 200 {
		return errors.New("project name is required and must not exceed 200 characters")
	}
	if input.BrandName == "" || len([]rune(input.BrandName)) > 300 {
		return errors.New("brand name is required and must not exceed 300 characters")
	}
	parsedURL, err := url.ParseRequestURI(input.OfficialURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" {
		return errors.New("official URL must be an absolute HTTP or HTTPS URL")
	}
	host := strings.ToLower(parsedURL.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(host) != nil {
		return errors.New("official URL must use a public domain name")
	}
	if input.Locale == "" || len(input.Locale) > 35 {
		return errors.New("locale is required and must not exceed 35 characters")
	}
	if input.Region == "" || len(input.Region) > 35 {
		return errors.New("region is required and must not exceed 35 characters")
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return errors.New("timezone must be a valid IANA timezone")
	}
	return nil
}
