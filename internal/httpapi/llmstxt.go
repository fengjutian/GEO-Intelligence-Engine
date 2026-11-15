package httpapi

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// llmsHandler checks whether a project publishes an llms.txt index for AI crawlers.
type llmsHandler struct {
	db          *sql.DB
	devTenantID string
	logger      *slog.Logger
	client      *http.Client
}

func newLLMsHandler(db *sql.DB, devTenantID string, logger *slog.Logger) *llmsHandler {
	return &llmsHandler{
		db: db, devTenantID: devTenantID, logger: logger,
		client: &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (h *llmsHandler) tenant(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(headerTenant)); value != "" {
		return value
	}
	return h.devTenantID
}

// isPublicHTTPURL rejects schemes and hosts that must never be fetched server side.
func isPublicHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
	}
	return !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".internal")
}

func (h *llmsHandler) check(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	var baseURL string
	err := h.db.QueryRowContext(r.Context(), `SELECT base_url FROM websites w
JOIN projects p ON p.id=w.project_id AND p.tenant_id=w.tenant_id
WHERE w.tenant_id=UUID_TO_BIN(?) AND w.project_id=UUID_TO_BIN(?) ORDER BY w.created_at LIMIT 1`, h.tenant(r), projectID).Scan(&baseURL)
	if err == sql.ErrNoRows {
		writeProblem(w, http.StatusNotFound, "WEBSITE_NOT_FOUND", "Project has no website configured", traceID(r))
		return
	}
	if err != nil {
		h.logger.Error("load website for llms.txt", "error", err, "trace_id", traceID(r))
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		return
	}
	result := h.inspect(r.Context(), baseURL)
	writeJSON(w, http.StatusOK, result)
}

type llmsTxtResult struct {
	URL        string `json:"url"`
	Found      bool   `json:"found"`
	HTTPStatus int    `json:"httpStatus"`
	Bytes      int    `json:"bytes"`
	Lines      int    `json:"lines"`
	HasSitemap bool   `json:"hasSitemap"`
	Preview    string `json:"preview"`
	Error      string `json:"error,omitempty"`
}

func (h *llmsHandler) inspect(ctx context.Context, baseURL string) llmsTxtResult {
	target := strings.TrimSuffix(strings.TrimSpace(baseURL), "/") + "/llms.txt"
	result := llmsTxtResult{URL: target}
	if !isPublicHTTPURL(target) {
		result.Error = "URL is not publicly fetchable"
		return result
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		result.Error = "invalid URL"
		return result
	}
	request.Header.Set("User-Agent", "geo-intelligence-engine/1.0 (llms.txt check)")
	response, err := h.client.Do(request)
	if err != nil {
		result.Error = "fetch failed"
		return result
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode
	body, _ := io.ReadAll(io.LimitReader(response.Body, 512<<10))
	result.Bytes = len(body)
	if response.StatusCode != http.StatusOK {
		result.Error = "llms.txt is not published"
		return result
	}
	content := string(body)
	result.Found = strings.TrimSpace(content) != ""
	result.Lines = strings.Count(strings.TrimSpace(content), "\n") + 1
	result.HasSitemap = strings.Contains(strings.ToLower(content), "sitemap")
	if len(content) > 400 {
		content = content[:400]
	}
	result.Preview = content
	return result
}
