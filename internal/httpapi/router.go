package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/fengjutian/geo-intelligence-engine/internal/crawls"
	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
	"github.com/fengjutian/geo-intelligence-engine/internal/projects"
	"github.com/fengjutian/geo-intelligence-engine/internal/questions"
)

type Dependencies struct {
	DB           *sql.DB
	Logger       *slog.Logger
	DevTenantID  string
	DevActorID   string
	WorkerToken  string
	ModelGateway *modelgateway.Gateway
	ReportDir    string
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
	mux.HandleFunc("POST /api/v1/projects", projectHandler.create)
	mux.HandleFunc("GET /api/v1/projects/{projectId}", projectHandler.get)
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}", projectHandler.update)
	mux.HandleFunc("POST /api/v1/projects/{projectAction}", projectHandler.archiveAction)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/config-versions", projectHandler.publishConfig)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/brand-profile", projectHandler.getBrandProfile)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/brand-aliases", projectHandler.createAlias)
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}/brand-aliases/{aliasId}", projectHandler.setAliasEnabled)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/products", projectHandler.createProduct)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/competitors", projectHandler.createCompetitor)
	crawlHandler := newCrawlHandler(crawls.NewStore(deps.DB), deps.DevTenantID, deps.WorkerToken, deps.Logger)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/crawl-runs", crawlHandler.create)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/crawl-runs", crawlHandler.listProjectRuns)
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}", crawlHandler.get)
	mux.HandleFunc("POST /api/v1/crawl-runs/{runAction}", crawlHandler.controlAction)
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
	mux.HandleFunc("GET /api/v1/projects/{projectId}/questions", questionHandler.list)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/questions", questionHandler.create)
	auditHandler := newAuditHandler(audits.NewStore(deps.DB), deps.DevTenantID, deps.Logger, deps.ReportDir)
	mux.HandleFunc("GET /api/v1/engine-configs", auditHandler.listEngineConfigs)
	mux.HandleFunc("POST /api/v1/engine-configs", auditHandler.createEngineConfig)
	mux.HandleFunc("POST /api/v1/question-sets/{questionSetId}/versions", auditHandler.publishQuestionSet)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/question-sets", auditHandler.listQuestionSets)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/measurement-protocols", auditHandler.listProtocols)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/measurement-protocols", auditHandler.createProtocol)
	mux.HandleFunc("POST /api/v1/measurement-protocols/{protocolAction}", auditHandler.publishProtocol)
	mux.HandleFunc("POST /api/v1/audits:estimate", auditHandler.estimateAudit)
	mux.HandleFunc("POST /api/v1/audits", auditHandler.createAudit)
	mux.HandleFunc("GET /api/v1/audits/{auditId}", auditHandler.getAudit)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/samples", auditHandler.listSamples)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/metrics", auditHandler.getMetrics)
	mux.HandleFunc("POST /api/v1/audits/{auditId}/reports", auditHandler.createReport)
	mux.HandleFunc("GET /api/v1/reports/{reportId}", auditHandler.getReport)
	mux.HandleFunc("POST /api/v1/reports/{reportAction}", auditHandler.publishReport)
	mux.HandleFunc("POST /api/v1/reports/{reportId}/exports", auditHandler.createReportExport)
	mux.HandleFunc("GET /api/v1/reports/{reportId}/artifacts/{artifactId}", auditHandler.downloadReportArtifact)
	mux.HandleFunc("POST /api/v1/audits/{auditAction}", auditHandler.cancelAudit)

	return requestContext(deps.Logger, recoverer(deps.Logger, securityHeaders(mux)))
}
