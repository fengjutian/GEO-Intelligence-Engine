package audits

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnsupportedExport = errors.New("export format is not supported")
	ErrPDFRenderFailed   = errors.New("pdf rendering failed")
)

// CreateReport snapshots one audit into a versioned, ready-to-read report.
func (s *Store) CreateReport(ctx context.Context, tenantID, auditID string, metricSnapshotID, templateVersion string) (Report, error) {
	if templateVersion == "" {
		templateVersion = "report-v1"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback()
	var projectID, auditStatus, protocolID string
	var planned, completed, eligible int
	var budget int64
	var currency string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id),status,planned_count,completed_count,eligible_count,budget_microunits,currency,BIN_TO_UUID(protocol_id)
FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) FOR UPDATE`, tenantID, auditID).Scan(&projectID, &auditStatus, &planned, &completed, &eligible, &budget, &currency, &protocolID)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, err
	}
	if auditStatus != "succeeded" && auditStatus != "partial_success" {
		return Report{}, ErrInvalidInput
	}
	if metricSnapshotID == "" {
		err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM metric_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?) ORDER BY generated_at DESC LIMIT 1`, tenantID, auditID).Scan(&metricSnapshotID)
		if errors.Is(err, sql.ErrNoRows) {
			return Report{}, ErrInvalidInput
		}
		if err != nil {
			return Report{}, err
		}
	}
	var evaluationRunID, metricVersion, judgeVersion string
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(evaluation_run_id),metric_version FROM metric_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND audit_id=UUID_TO_BIN(?)`, tenantID, metricSnapshotID, auditID).Scan(&evaluationRunID, &metricVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrInvalidInput
	}
	if err != nil {
		return Report{}, err
	}
	_ = tx.QueryRowContext(ctx, `SELECT judge_version FROM evaluation_runs WHERE id=UUID_TO_BIN(?)`, evaluationRunID).Scan(&judgeVersion)

	metrics, err := loadMetricValues(ctx, tx, tenantID, metricSnapshotID)
	if err != nil {
		return Report{}, err
	}
	var protocolVersion string
	var configJSON []byte
	if err := tx.QueryRowContext(ctx, `SELECT semantic_version,config_json FROM measurement_protocols WHERE id=UUID_TO_BIN(?)`, protocolID).Scan(&protocolVersion, &configJSON); err != nil {
		return Report{}, err
	}
	var protocolConfig map[string]any
	_ = json.Unmarshal(configJSON, &protocolConfig)
	findings, err := loadReportFindings(ctx, tx, tenantID, projectID)
	if err != nil {
		return Report{}, err
	}

	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM reports WHERE audit_id=UUID_TO_BIN(?)`, auditID).Scan(&version); err != nil {
		return Report{}, err
	}
	dependencies := map[string]any{
		"protocolId":       protocolID,
		"protocolVersion":  protocolVersion,
		"evaluationRunId":  evaluationRunID,
		"metricSnapshotId": metricSnapshotID,
		"metricVersion":    metricVersion,
		"judgeVersion":     judgeVersion,
		"templateVersion":  templateVersion,
	}
	dependencyJSON, _ := json.Marshal(dependencies)
	reportID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO reports (id,tenant_id,project_id,audit_id,version,dependency_versions,template_version,status,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,CAST(? AS JSON),?,'ready',UTC_TIMESTAMP(6))`,
		reportID.String(), tenantID, projectID, auditID, version, dependencyJSON, templateVersion)
	if err != nil {
		return Report{}, fmt.Errorf("insert report: %w", err)
	}
	limitations := []string{}
	if auditStatus == "partial_success" {
		limitations = append(limitations, "部分样本失败，报告只覆盖已完成样本")
	}
	if eligible > 0 && eligible < 30 {
		limitations = append(limitations, fmt.Sprintf("有效样本仅 %d 条，比例区间较宽，不应据此做趋势结论", eligible))
	}
	sections := []struct {
		code    string
		content map[string]any
	}{
		{"summary", map[string]any{
			"auditId": auditID, "projectId": projectID, "status": auditStatus,
			"plannedCount": planned, "completedCount": completed, "eligibleCount": eligible,
			"budgetMicrounits": budget, "currency": currency,
			"generatedAt": time.Now().UTC().Format(time.RFC3339),
		}},
		{"scope", map[string]any{"protocolVersion": protocolVersion, "protocol": protocolConfig, "metricVersion": metricVersion, "judgeVersion": judgeVersion}},
		{"metrics", map[string]any{"metricVersion": metricVersion, "values": metrics}},
		{"findings", map[string]any{"items": findings}},
		{"limitations", map[string]any{"items": limitations}},
	}
	for _, section := range sections {
		content, _ := json.Marshal(section.content)
		hash := sha256.Sum256(content)
		sectionID, _ := uuid.NewV7()
		_, err = tx.ExecContext(ctx, `INSERT INTO report_sections (id,tenant_id,report_id,section_code,data_snapshot_object_key,content_hash,content_json) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,CAST(? AS JSON))`,
			sectionID.String(), tenantID, reportID.String(), section.code, fmt.Sprintf("db://report/%s/%s", reportID, section.code), hash[:], content)
		if err != nil {
			return Report{}, fmt.Errorf("insert report section: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Report{}, err
	}
	return s.GetReport(ctx, tenantID, reportID.String())
}

func loadMetricValues(ctx context.Context, tx *sql.Tx, tenantID, snapshotID string) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, `SELECT metric_code,aggregation_method,dimensions_json,numerator,denominator,value_decimal,ci_low,ci_high,interval_method
FROM metric_values WHERE tenant_id=UUID_TO_BIN(?) AND snapshot_id=UUID_TO_BIN(?) ORDER BY metric_code,dimensions_json`, tenantID, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var code, method, interval string
		var dimensions []byte
		var numerator, denominator, value, ciLow, ciHigh sql.NullFloat64
		if err := rows.Scan(&code, &method, &dimensions, &numerator, &denominator, &value, &ciLow, &ciHigh, &interval); err != nil {
			return nil, err
		}
		var dimensionsValue map[string]any
		_ = json.Unmarshal(dimensions, &dimensionsValue)
		items = append(items, map[string]any{
			"metricCode": code, "aggregationMethod": method, "dimensions": dimensionsValue,
			"numerator": nullFloat(numerator), "denominator": nullFloat(denominator),
			"value": nullFloat(value), "ciLow": nullFloat(ciLow), "ciHigh": nullFloat(ciHigh), "intervalMethod": interval,
		})
	}
	return items, rows.Err()
}

func loadReportFindings(ctx context.Context, tx *sql.Tx, tenantID, projectID string) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, `SELECT rule_code,severity,status,summary FROM findings WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY FIELD(severity,'critical','high','medium','low'),created_at DESC LIMIT 50`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var code, severity, status, summary string
		if err := rows.Scan(&code, &severity, &status, &summary); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"ruleCode": code, "severity": severity, "status": status, "summary": summary})
	}
	return items, rows.Err()
}

func (s *Store) GetReport(ctx context.Context, tenantID, reportID string) (Report, error) {
	var report Report
	var dependencyJSON []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),BIN_TO_UUID(audit_id),BIN_TO_UUID(project_id),version,status,template_version,dependency_versions,created_at,published_at
FROM reports WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, reportID).Scan(&report.ID, &report.AuditID, &report.ProjectID, &report.Version, &report.Status, &report.TemplateVersion, &dependencyJSON, &report.CreatedAt, &report.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, err
	}
	_ = json.Unmarshal(dependencyJSON, &report.DependencyVersions)
	rows, err := s.db.QueryContext(ctx, `SELECT section_code,COALESCE(content_json,JSON_OBJECT()) FROM report_sections WHERE tenant_id=UUID_TO_BIN(?) AND report_id=UUID_TO_BIN(?) ORDER BY id`, tenantID, reportID)
	if err != nil {
		return Report{}, err
	}
	report.Sections = []ReportSection{}
	for rows.Next() {
		var section ReportSection
		var content []byte
		if err := rows.Scan(&section.Code, &content); err != nil {
			rows.Close()
			return Report{}, err
		}
		_ = json.Unmarshal(content, &section.Content)
		report.Sections = append(report.Sections, section)
	}
	rows.Close()
	artifactRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),artifact_type,size_bytes,LOWER(HEX(sha256)),created_at FROM report_artifacts WHERE tenant_id=UUID_TO_BIN(?) AND report_id=UUID_TO_BIN(?) ORDER BY created_at`, tenantID, reportID)
	if err != nil {
		return Report{}, err
	}
	report.Artifacts = []ReportArtifact{}
	for artifactRows.Next() {
		var artifact ReportArtifact
		if err := artifactRows.Scan(&artifact.ID, &artifact.Type, &artifact.SizeBytes, &artifact.SHA256, &artifact.CreatedAt); err != nil {
			artifactRows.Close()
			return Report{}, err
		}
		artifact.DownloadURL = fmt.Sprintf("/api/v1/reports/%s/artifacts/%s", report.ID, artifact.ID)
		report.Artifacts = append(report.Artifacts, artifact)
	}
	artifactRows.Close()
	return report, nil
}

func (s *Store) PublishReport(ctx context.Context, tenantID, reportID string) (Report, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE reports SET status='published',published_at=UTC_TIMESTAMP(6) WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?) AND status='ready'`, tenantID, reportID)
	if err != nil {
		return Report{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		if _, err := s.GetReport(ctx, tenantID, reportID); err != nil {
			return Report{}, err
		}
		return Report{}, ErrConflict
	}
	return s.GetReport(ctx, tenantID, reportID)
}

// CreateExport writes a CSV or PDF artifact and records its hash and size.
func (s *Store) CreateExport(ctx context.Context, tenantID, reportID, format, reportDir string) (ReportArtifact, error) {
	if format != "csv" && format != "pdf" {
		return ReportArtifact{}, ErrUnsupportedExport
	}
	report, err := s.GetReport(ctx, tenantID, reportID)
	if err != nil {
		return ReportArtifact{}, err
	}
	if report.Status != "ready" && report.Status != "published" {
		return ReportArtifact{}, ErrConflict
	}
	if format == "pdf" {
		return s.createPDFExport(ctx, tenantID, report, reportDir)
	}
	content := buildCSV(report)
	if err := s.removeArtifact(ctx, tenantID, reportID, "csv"); err != nil {
		return ReportArtifact{}, err
	}
	return s.writeArtifact(ctx, tenantID, reportID, "csv", reportDir, "csv", content, map[string]any{"format": "csv", "templateVersion": report.TemplateVersion})
}

func (s *Store) createPDFExport(ctx context.Context, tenantID string, report Report, reportDir string) (ReportArtifact, error) {
	if err := s.removeArtifact(ctx, tenantID, report.ID, "pdf"); err != nil {
		return ReportArtifact{}, err
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return ReportArtifact{}, err
	}
	artifactID, _ := uuid.NewV7()
	directory := filepath.Join(reportDir, report.ID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return ReportArtifact{}, err
	}
	jsonPath := filepath.Join(directory, artifactID.String()+".json")
	pdfPath := filepath.Join(directory, artifactID.String()+".pdf")
	if err := os.WriteFile(jsonPath, payload, 0o644); err != nil {
		return ReportArtifact{}, err
	}
	defer os.Remove(jsonPath)
	nodeBinary := os.Getenv("GEO_NODE_BINARY")
	if nodeBinary == "" {
		nodeBinary = "node"
	}
	renderer := os.Getenv("GEO_REPORT_RENDERER")
	if renderer == "" {
		renderer = filepath.Join("apps", "report-renderer", "render.mjs")
	}
	renderCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	command := exec.CommandContext(renderCtx, nodeBinary, renderer, jsonPath, pdfPath)
	if chromium := os.Getenv("GEO_CHROMIUM_PATH"); chromium != "" {
		command.Env = append(os.Environ(), "GEO_CHROMIUM_PATH="+chromium)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return ReportArtifact{}, fmt.Errorf("%w: %s", ErrPDFRenderFailed, strings.TrimSpace(string(output)))
	}
	defer os.Remove(pdfPath + ".meta.json")
	content, err := os.ReadFile(pdfPath)
	if err != nil {
		return ReportArtifact{}, fmt.Errorf("%w: %s", ErrPDFRenderFailed, err)
	}
	metadata := map[string]any{"format": "pdf", "templateVersion": report.TemplateVersion}
	if rawMeta, metaErr := os.ReadFile(pdfPath + ".meta.json"); metaErr == nil {
		var decoded map[string]any
		if json.Unmarshal(rawMeta, &decoded) == nil {
			metadata = decoded
			metadata["format"] = "pdf"
		}
	}
	return s.writeArtifactWithID(ctx, tenantID, report.ID, artifactID.String(), "pdf", pdfPath, content, metadata)
}

func (s *Store) writeArtifact(ctx context.Context, tenantID, reportID, artifactType, reportDir, extension string, content []byte, metadata map[string]any) (ReportArtifact, error) {
	artifactID, _ := uuid.NewV7()
	directory := filepath.Join(reportDir, reportID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return ReportArtifact{}, err
	}
	path := filepath.Join(directory, fmt.Sprintf("%s.%s", artifactID, extension))
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return ReportArtifact{}, err
	}
	return s.writeArtifactWithID(ctx, tenantID, reportID, artifactID.String(), artifactType, path, content, metadata)
}

func (s *Store) writeArtifactWithID(ctx context.Context, tenantID, reportID, artifactID, artifactType, path string, content []byte, metadata map[string]any) (ReportArtifact, error) {
	hash := sha256.Sum256(content)
	metadataJSON, _ := json.Marshal(metadata)
	_, err := s.db.ExecContext(ctx, `INSERT INTO report_artifacts (id,tenant_id,report_id,artifact_type,object_key,sha256,size_bytes,renderer_metadata,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,CAST(? AS JSON),UTC_TIMESTAMP(6))`,
		artifactID, tenantID, reportID, artifactType, path, hash[:], len(content), metadataJSON)
	if err != nil {
		return ReportArtifact{}, err
	}
	return ReportArtifact{ID: artifactID, Type: artifactType, SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(hash[:]), DownloadURL: fmt.Sprintf("/api/v1/reports/%s/artifacts/%s", reportID, artifactID), CreatedAt: time.Now().UTC()}, nil
}

// removeArtifact deletes a previous artifact of the same type so exports stay replaceable.
func (s *Store) removeArtifact(ctx context.Context, tenantID, reportID, artifactType string) error {
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT object_key FROM report_artifacts WHERE tenant_id=UUID_TO_BIN(?) AND report_id=UUID_TO_BIN(?) AND artifact_type=?`, tenantID, reportID, artifactType).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM report_artifacts WHERE tenant_id=UUID_TO_BIN(?) AND report_id=UUID_TO_BIN(?) AND artifact_type=?`, tenantID, reportID, artifactType); err != nil {
		return err
	}
	_ = os.Remove(path)
	return nil
}

// GetArtifactPath resolves a report artifact for download after tenant checks.
func (s *Store) GetArtifactPath(ctx context.Context, tenantID, reportID, artifactID string) (string, string, error) {
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT object_key FROM report_artifacts WHERE tenant_id=UUID_TO_BIN(?) AND report_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, reportID, artifactID).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return path, filepath.Base(path), nil
}

func buildCSV(report Report) []byte {
	buffer := &bytes.Buffer{}
	buffer.WriteString("\uFEFF")
	writer := csv.NewWriter(buffer)
	write := func(values ...string) {
		row := make([]string, 9)
		copy(row, values)
		_ = writer.Write(row)
	}
	write("section", "item", "dimensions", "numerator", "denominator", "value", "ciLow", "ciHigh", "intervalMethod")
	write("report", "id", report.ID)
	write("report", "version", fmt.Sprintf("%d", report.Version))
	write("report", "status", report.Status)
	write("report", "templateVersion", report.TemplateVersion)
	write("report", "createdAt", report.CreatedAt.Format(time.RFC3339))
	for _, section := range report.Sections {
		switch section.Code {
		case "summary":
			for key, value := range section.Content {
				write("summary", key, csvCell(csvValue(value)))
			}
		case "metrics":
			values, _ := section.Content["values"].([]any)
			for _, entry := range values {
				metric, _ := entry.(map[string]any)
				dimensions, _ := json.Marshal(metric["dimensions"])
				write("metric", csvValue(metric["metricCode"]), string(dimensions),
					csvValue(metric["numerator"]), csvValue(metric["denominator"]),
					csvValue(metric["value"]), csvValue(metric["ciLow"]), csvValue(metric["ciHigh"]),
					csvValue(metric["intervalMethod"]))
			}
		case "findings":
			items, _ := section.Content["items"].([]any)
			for _, entry := range items {
				finding, _ := entry.(map[string]any)
				write("finding", csvValue(finding["ruleCode"]), csvCell(csvValue(finding["severity"])), "", "", csvCell(csvValue(finding["summary"])))
			}
		case "limitations":
			items, _ := section.Content["items"].([]any)
			for _, entry := range items {
				write("limitation", "", csvCell(csvValue(entry)))
			}
		}
	}
	writer.Flush()
	return buffer.Bytes()
}

// csvCell neutralizes spreadsheet formula injection.
func csvCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	}
	return value
}

func csvValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
