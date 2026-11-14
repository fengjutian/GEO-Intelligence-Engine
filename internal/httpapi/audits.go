package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/google/uuid"
)

type auditStore interface {
	CreateEngineConfig(ctx context.Context, tenantID string, input audits.EngineConfigInput) (audits.EngineConfig, error)
	ListEngineConfigs(ctx context.Context, tenantID string) ([]audits.EngineConfig, error)
	PublishQuestionSet(ctx context.Context, tenantID, questionSetID string) (map[string]any, error)
	ListQuestionSets(ctx context.Context, tenantID, projectID string) ([]audits.QuestionSet, error)
	CreateProtocol(ctx context.Context, tenantID, projectID string, input audits.ProtocolInput) (audits.Protocol, error)
	PublishProtocol(ctx context.Context, tenantID, protocolID string) (audits.Protocol, error)
	GetProtocol(ctx context.Context, tenantID, protocolID string) (audits.Protocol, error)
	ListProtocols(ctx context.Context, tenantID, projectID string) ([]audits.Protocol, error)
	EstimateAudit(ctx context.Context, tenantID, protocolID string) (audits.Estimate, error)
	CreateAudit(ctx context.Context, tenantID, projectID, protocolID, idempotencyKey string) (audits.Audit, error)
	GetAudit(ctx context.Context, tenantID, auditID string) (audits.Audit, error)
	CancelAudit(ctx context.Context, tenantID, auditID string) (audits.Audit, error)
	ListSamples(ctx context.Context, tenantID, auditID string) ([]audits.Sample, error)
	GetMetrics(ctx context.Context, tenantID, auditID, aggregationMethod, channel string) ([]audits.MetricValue, error)
	GetMetricEvidence(ctx context.Context, tenantID, metricValueID string) (map[string]any, error)
	CreateReport(ctx context.Context, tenantID, auditID, metricSnapshotID, templateVersion string) (audits.Report, error)
	GetReport(ctx context.Context, tenantID, reportID string) (audits.Report, error)
	PublishReport(ctx context.Context, tenantID, reportID string) (audits.Report, error)
	CreateExport(ctx context.Context, tenantID, reportID, format, reportDir string) (audits.ReportArtifact, error)
	GetArtifactPath(ctx context.Context, tenantID, reportID, artifactID string) (string, string, error)
	RecordReportAccess(ctx context.Context, tenantID, actorID, reportID, action string) error
	PublishKnowledgeSnapshot(ctx context.Context, tenantID, projectID string) (map[string]any, error)
	ContentQuality(ctx context.Context, tenantID, projectID string) (map[string]any, error)
}

type auditHandler struct {
	store       auditStore
	devTenantID string
	reportDir   string
	logger      *slog.Logger
}

func newAuditHandler(store auditStore, devTenantID string, logger *slog.Logger, reportDir string) *auditHandler {
	if reportDir == "" {
		reportDir = ".run/reports"
	}
	return &auditHandler{store: store, devTenantID: devTenantID, reportDir: reportDir, logger: logger}
}

func (h *auditHandler) tenant(r *http.Request) string {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		return h.devTenantID
	}
	return tenantID
}

func (h *auditHandler) createEngineConfig(w http.ResponseWriter, r *http.Request) {
	var input audits.EngineConfigInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	item, err := h.store.CreateEngineConfig(r.Context(), h.tenant(r), input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *auditHandler) listEngineConfigs(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListEngineConfigs(r.Context(), h.tenant(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *auditHandler) publishQuestionSet(w http.ResponseWriter, r *http.Request) {
	questionSetID := r.PathValue("questionSetId")
	if _, err := uuid.Parse(questionSetID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_QUESTION_SET_ID", "Question set ID is invalid", traceID(r))
		return
	}
	result, err := h.store.PublishQuestionSet(r.Context(), h.tenant(r), questionSetID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *auditHandler) listQuestionSets(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	items, err := h.store.ListQuestionSets(r.Context(), h.tenant(r), projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *auditHandler) createProtocol(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	var input audits.ProtocolInput
	if !decodeJSONBody(w, r, &input) {
		return
	}
	protocol, err := h.store.CreateProtocol(r.Context(), h.tenant(r), projectID, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, protocol)
}

func (h *auditHandler) listProtocols(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	items, err := h.store.ListProtocols(r.Context(), h.tenant(r), projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *auditHandler) publishProtocol(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("protocolAction")
	separator := strings.LastIndexByte(raw, ':')
	if separator < 1 || raw[separator+1:] != "publish" {
		writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route was not found", traceID(r))
		return
	}
	protocolID := raw[:separator]
	if _, err := uuid.Parse(protocolID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROTOCOL_ID", "Protocol ID is invalid", traceID(r))
		return
	}
	protocol, err := h.store.PublishProtocol(r.Context(), h.tenant(r), protocolID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol)
}

func (h *auditHandler) estimateAudit(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID  string `json:"projectId"`
		ProtocolID string `json:"protocolId"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.ProtocolID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROTOCOL_ID", "Protocol ID is invalid", traceID(r))
		return
	}
	estimate, err := h.store.EstimateAudit(r.Context(), h.tenant(r), input.ProtocolID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, estimate)
}

func (h *auditHandler) createAudit(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		writeProblem(w, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required", traceID(r))
		return
	}
	var input struct {
		ProjectID  string `json:"projectId"`
		ProtocolID string `json:"protocolId"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if _, err := uuid.Parse(input.ProjectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	if _, err := uuid.Parse(input.ProtocolID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROTOCOL_ID", "Protocol ID is invalid", traceID(r))
		return
	}
	audit, err := h.store.CreateAudit(r.Context(), h.tenant(r), input.ProjectID, input.ProtocolID, idempotencyKey)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, audit)
}

func (h *auditHandler) getAudit(w http.ResponseWriter, r *http.Request) {
	auditID := r.PathValue("auditId")
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	audit, err := h.store.GetAudit(r.Context(), h.tenant(r), auditID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, audit)
}

func (h *auditHandler) cancelAudit(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("auditAction")
	separator := strings.LastIndexByte(raw, ':')
	if separator < 1 || raw[separator+1:] != "cancel" {
		writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route was not found", traceID(r))
		return
	}
	auditID := raw[:separator]
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	audit, err := h.store.CancelAudit(r.Context(), h.tenant(r), auditID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, audit)
}

func (h *auditHandler) getMetrics(w http.ResponseWriter, r *http.Request) {
	auditID := r.PathValue("auditId")
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	aggregationMethod := strings.TrimSpace(r.URL.Query().Get("aggregationMethod"))
	if aggregationMethod == "" {
		aggregationMethod = "answer_equal"
	}
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	items, err := h.store.GetMetrics(r.Context(), h.tenant(r), auditID, aggregationMethod, channel)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *auditHandler) getMetricEvidence(w http.ResponseWriter, r *http.Request) {
	metricValueID := r.PathValue("metricValueId")
	if _, err := uuid.Parse(metricValueID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_METRIC_VALUE_ID", "Metric value ID is invalid", traceID(r))
		return
	}
	evidence, err := h.store.GetMetricEvidence(r.Context(), h.tenant(r), metricValueID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, evidence)
}

func (h *auditHandler) createReport(w http.ResponseWriter, r *http.Request) {
	auditID := r.PathValue("auditId")
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	var input struct {
		MetricSnapshotID string `json:"metricSnapshotId"`
		TemplateVersion  string `json:"templateVersion"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	report, err := h.store.CreateReport(r.Context(), h.tenant(r), auditID, input.MetricSnapshotID, input.TemplateVersion)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, report)
}

func (h *auditHandler) publishKnowledgeSnapshot(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	snapshot, err := h.store.PublishKnowledgeSnapshot(r.Context(), h.tenant(r), projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, snapshot)
}

func (h *auditHandler) getContentQuality(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	quality, err := h.store.ContentQuality(r.Context(), h.tenant(r), projectID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, quality)
}

func (h *auditHandler) getReport(w http.ResponseWriter, r *http.Request) {
	reportID := r.PathValue("reportId")
	if _, err := uuid.Parse(reportID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_REPORT_ID", "Report ID is invalid", traceID(r))
		return
	}
	report, err := h.store.GetReport(r.Context(), h.tenant(r), reportID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordReportAccess(r, reportID, "report.viewed")
	writeJSON(w, http.StatusOK, report)
}

func (h *auditHandler) recordReportAccess(r *http.Request, reportID, action string) {
	actorID := r.Header.Get(headerActor)
	if err := h.store.RecordReportAccess(r.Context(), h.tenant(r), actorID, reportID, action); err != nil {
		h.logger.Warn("record report access failed", "error", err, "report_id", reportID, "action", action)
	}
}

func (h *auditHandler) publishReport(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("reportAction")
	separator := strings.LastIndexByte(raw, ':')
	if separator < 1 || raw[separator+1:] != "publish" {
		writeProblem(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route was not found", traceID(r))
		return
	}
	reportID := raw[:separator]
	if _, err := uuid.Parse(reportID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_REPORT_ID", "Report ID is invalid", traceID(r))
		return
	}
	report, err := h.store.PublishReport(r.Context(), h.tenant(r), reportID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *auditHandler) createReportExport(w http.ResponseWriter, r *http.Request) {
	reportID := r.PathValue("reportId")
	if _, err := uuid.Parse(reportID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_REPORT_ID", "Report ID is invalid", traceID(r))
		return
	}
	var input struct {
		Format string `json:"format"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	artifact, err := h.store.CreateExport(r.Context(), h.tenant(r), reportID, strings.ToLower(strings.TrimSpace(input.Format)), h.reportDir)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordReportAccess(r, reportID, "report.exported")
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": artifact.ID, "status": "ready", "downloadUrl": artifact.DownloadURL, "sizeBytes": artifact.SizeBytes})
}

func (h *auditHandler) downloadReportArtifact(w http.ResponseWriter, r *http.Request) {
	reportID := r.PathValue("reportId")
	artifactID := r.PathValue("artifactId")
	if _, err := uuid.Parse(reportID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_REPORT_ID", "Report ID is invalid", traceID(r))
		return
	}
	if _, err := uuid.Parse(artifactID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_ARTIFACT_ID", "Artifact ID is invalid", traceID(r))
		return
	}
	path, name, err := h.store.GetArtifactPath(r.Context(), h.tenant(r), reportID, artifactID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordReportAccess(r, reportID, "report.downloaded")
	if strings.HasSuffix(strings.ToLower(name), ".pdf") {
		w.Header().Set("Content-Type", "application/pdf")
	} else {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeFile(w, r, path)
}

func (h *auditHandler) listSamples(w http.ResponseWriter, r *http.Request) {
	auditID := r.PathValue("auditId")
	if _, err := uuid.Parse(auditID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_AUDIT_ID", "Audit ID is invalid", traceID(r))
		return
	}
	items, err := h.store.ListSamples(r.Context(), h.tenant(r), auditID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *auditHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, audits.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "AUDIT_RESOURCE_NOT_FOUND", "Audit resource was not found", traceID(r))
	case errors.Is(err, audits.ErrConflict):
		writeProblem(w, http.StatusConflict, "AUDIT_CONFLICT", "Request conflicts with existing audit state", traceID(r))
	case errors.Is(err, audits.ErrArchived):
		writeProblem(w, http.StatusConflict, "PROJECT_ARCHIVED", "Archived project cannot start an audit", traceID(r))
	case errors.Is(err, audits.ErrPlanTooLarge):
		writeProblem(w, http.StatusUnprocessableEntity, "AUDIT_PLAN_TOO_LARGE", "Audit plan exceeds the supported sample count", traceID(r))
	case errors.Is(err, audits.ErrUnsupportedExport):
		writeProblem(w, http.StatusUnprocessableEntity, "EXPORT_FORMAT_UNSUPPORTED", "Export format is not supported", traceID(r))
	case errors.Is(err, audits.ErrPDFRenderFailed):
		h.logger.Error("pdf rendering failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "PDF_RENDER_FAILED", "PDF rendering failed", traceID(r))
	case errors.Is(err, audits.ErrBudgetExceeded):
		writeProblem(w, http.StatusUnprocessableEntity, "AUDIT_BUDGET_EXCEEDED", "Estimated cost exceeds the protocol budget", traceID(r))
	case errors.Is(err, audits.ErrInvalidInput):
		writeProblem(w, http.StatusUnprocessableEntity, "AUDIT_INPUT_INVALID", "Audit input is invalid", traceID(r))
	default:
		h.logger.Error("audit operation failed", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
	}
}
