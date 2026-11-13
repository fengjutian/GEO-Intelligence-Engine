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
