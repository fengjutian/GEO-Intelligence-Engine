package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/fengjutian/geo-intelligence-engine/internal/auth"
	"github.com/fengjutian/geo-intelligence-engine/internal/crawls"
	"github.com/fengjutian/geo-intelligence-engine/internal/members"
	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
	"github.com/fengjutian/geo-intelligence-engine/internal/projects"
	"github.com/fengjutian/geo-intelligence-engine/internal/questions"
	"github.com/fengjutian/geo-intelligence-engine/internal/tasks"
)

type Dependencies struct {
	DB           *sql.DB
	Logger       *slog.Logger
	DevTenantID  string
	DevActorID   string
	WorkerToken  string
	ModelGateway *modelgateway.Gateway
	ReportDir    string
	AuthMode     string
	AuthSecret   string
	DevRole      string
	OIDCIssuer   string
	OIDCAudience string
	OIDCJWKSURL  string
}

func New(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
			var cancel func()
			ctx, cancel = withTimeout(ctx, 2*time.Second)
			defer cancel()
		}
		if err := deps.DB.PingContext(ctx); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Database is unavailable", traceID(r))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"apiVersion": "1.0.0", "baseline": "P0-V1.0"})
	})
	projectHandler := newProjectHandler(projects.NewStore(deps.DB), deps.DevTenantID, deps.DevActorID, deps.Logger)
	mux.HandleFunc("GET /api/v1/projects", projectHandler.list)
	mux.HandleFunc("POST /api/v1/projects", requirePermission("edit_project", projectHandler.create))
	mux.HandleFunc("GET /api/v1/projects/{projectId}", projectAccess(deps.DB, projectHandler.get))
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}", requirePermission("edit_project", projectAccess(deps.DB, projectHandler.update)))
	mux.HandleFunc("POST /api/v1/projects/{projectAction}", requirePermission("edit_project", projectAccess(deps.DB, projectHandler.archiveAction)))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/config-versions", requirePermission("edit_project", projectAccess(deps.DB, projectHandler.publishConfig)))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/brand-profile", projectAccess(deps.DB, projectHandler.getBrandProfile))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/brand-aliases", projectAccess(deps.DB, projectHandler.createAlias))
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}/brand-aliases/{aliasId}", projectAccess(deps.DB, projectHandler.setAliasEnabled))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/products", projectAccess(deps.DB, projectHandler.createProduct))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/competitors", projectAccess(deps.DB, projectHandler.createCompetitor))
	crawlHandler := newCrawlHandler(crawls.NewStore(deps.DB), deps.DevTenantID, deps.WorkerToken, deps.Logger)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/crawl-runs", requirePermission("manage_crawl", projectAccess(deps.DB, crawlHandler.create)))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/crawl-runs", projectAccess(deps.DB, crawlHandler.listProjectRuns))
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}", crawlHandler.get)
	mux.HandleFunc("POST /api/v1/crawl-runs/{runAction}", requirePermission("manage_crawl", crawlHandler.controlAction))
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}/pages", crawlHandler.listPages)
	mux.HandleFunc("GET /api/v1/page-contents/{contentId}", crawlHandler.getPageContent)
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}/findings", crawlHandler.listFindings)
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}/robots", crawlHandler.listRobotsEvidence)
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}/sitemaps", crawlHandler.listSitemapEvidence)
	mux.HandleFunc("POST /internal/crawler/v1/claim", crawlHandler.claim)
	mux.HandleFunc("POST /internal/crawler/v1/runs/{crawlRunId}/complete", crawlHandler.complete)
	mux.HandleFunc("POST /internal/crawler/v1/runs/{crawlRunId}/fail", crawlHandler.fail)
	mux.HandleFunc("POST /internal/crawler/v1/runs/{crawlRunId}/blocked", crawlHandler.block)
	mux.HandleFunc("POST /internal/crawler/v1/runs/{crawlRunId}/robots", crawlHandler.recordRobots)
	mux.HandleFunc("POST /internal/crawler/v1/runs/{crawlRunId}/sitemaps", crawlHandler.reportSitemaps)
	modelHandler := newModelHandler(deps.ModelGateway, deps.Logger)
	mux.HandleFunc("GET /api/v1/model-providers", modelHandler.listProviders)
	mux.HandleFunc("POST /api/v1/model-providers/{provider}/health", modelHandler.health)
	mux.HandleFunc("POST /api/v1/model-gateway/chat", modelHandler.chat)
	questionHandler := newQuestionHandler(questions.NewStore(deps.DB), deps.DevTenantID, deps.Logger)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/questions", projectAccess(deps.DB, questionHandler.list))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/questions", projectAccess(deps.DB, questionHandler.create))
	auditHandler := newAuditHandler(audits.NewStore(deps.DB), deps.DevTenantID, deps.Logger, deps.ReportDir)
	mux.HandleFunc("GET /api/v1/engine-configs", auditHandler.listEngineConfigs)
	mux.HandleFunc("POST /api/v1/engine-configs", auditHandler.createEngineConfig)
	mux.HandleFunc("POST /api/v1/question-sets/{questionSetId}/versions", auditHandler.publishQuestionSet)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/question-sets", projectAccess(deps.DB, auditHandler.listQuestionSets))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/measurement-protocols", projectAccess(deps.DB, auditHandler.listProtocols))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/measurement-protocols", projectAccess(deps.DB, auditHandler.createProtocol))
	mux.HandleFunc("POST /api/v1/measurement-protocols/{protocolAction}", auditHandler.publishProtocol)
	mux.HandleFunc("POST /api/v1/audits:estimate", auditHandler.estimateAudit)
	mux.HandleFunc("POST /api/v1/audits", requirePermission("run_audit", auditHandler.createAudit))
	mux.HandleFunc("GET /api/v1/audits/{auditId}", auditHandler.getAudit)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/samples", auditHandler.listSamples)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/metrics", auditHandler.getMetrics)
	mux.HandleFunc("GET /api/v1/metric-values/{metricValueId}/evidence", auditHandler.getMetricEvidence)
	mux.HandleFunc("POST /api/v1/audits/{auditId}/reports", auditHandler.createReport)
	mux.HandleFunc("GET /api/v1/reports/{reportId}", auditHandler.getReport)
	mux.HandleFunc("POST /api/v1/reports/{reportAction}", auditHandler.publishReport)
	mux.HandleFunc("POST /api/v1/reports/{reportId}/exports", requirePermission("export_report", auditHandler.createReportExport))
	mux.HandleFunc("GET /api/v1/reports/{reportId}/artifacts/{artifactId}", auditHandler.downloadReportArtifact)
	taskHandler := newTaskHandler(tasks.NewStore(deps.DB), deps.DevTenantID, deps.DevActorID, deps.Logger)
	mux.HandleFunc("POST /api/v1/findings/{findingId}/optimization-tasks", requirePermission("edit_task", taskHandler.createFromFinding))
	mux.HandleFunc("GET /api/v1/optimization-tasks", taskHandler.list)
	mux.HandleFunc("GET /api/v1/optimization-tasks/{taskId}", taskHandler.get)
	mux.HandleFunc("PATCH /api/v1/optimization-tasks/{taskId}", requirePermission("edit_task", taskHandler.update))
	mux.HandleFunc("POST /api/v1/optimization-tasks/{taskAction}", requirePermission("edit_task", taskHandler.transition))
	mux.HandleFunc("PATCH /api/v1/optimization-tasks/{taskId}/acceptance-items/{itemId}", requirePermission("edit_task", taskHandler.updateAcceptance))
	mux.HandleFunc("POST /api/v1/optimization-tasks/{taskId}/comments", requirePermission("edit_task", taskHandler.addComment))
	mux.HandleFunc("GET /api/v1/optimization-tasks/{taskId}/events", taskHandler.listEvents)
	mux.HandleFunc("POST /api/v1/audits/{auditAction}", requirePermission("run_audit", auditHandler.cancelAudit))
	mux.HandleFunc("GET /api/v1/me", meHandler)
	memberHandler := newMemberHandler(members.NewStore(deps.DB))
	mux.HandleFunc("GET /api/v1/tenants/{tenantId}/members", requirePermission("read_members", memberHandler.list))
	mux.HandleFunc("POST /api/v1/tenants/{tenantId}/invitations", requirePermission("manage_members", memberHandler.invite))
	mux.HandleFunc("PATCH /api/v1/tenants/{tenantId}/members/{memberId}", requirePermission("manage_members", memberHandler.update))
	mux.HandleFunc("POST /api/v1/invitations/{invitationAction}", memberHandler.accept)

	var oidcVerifier *auth.OIDCVerifier
	if strings.EqualFold(deps.AuthMode, "oidc") {
		oidcVerifier = auth.NewOIDCVerifier(deps.OIDCIssuer, deps.OIDCAudience, deps.OIDCJWKSURL)
	}
	authMiddleware := newAuthMiddleware(authOptions{
		Mode: deps.AuthMode, Secret: deps.AuthSecret, DevRole: deps.DevRole,
		DevTenant: deps.DevTenantID, DevActor: deps.DevActorID,
		OIDC: oidcVerifier, DB: deps.DB, Logger: deps.Logger,
	})

	return requestContext(deps.Logger, recoverer(deps.Logger, securityHeaders(authMiddleware(mux))))
}
