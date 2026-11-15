package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/fengjutian/geo-intelligence-engine/internal/auth"
	"github.com/fengjutian/geo-intelligence-engine/internal/competitoraliases"
	"github.com/fengjutian/geo-intelligence-engine/internal/crawls"
	"github.com/fengjutian/geo-intelligence-engine/internal/embeddings"
	"github.com/fengjutian/geo-intelligence-engine/internal/entities"
	"github.com/fengjutian/geo-intelligence-engine/internal/factcheck"
	"github.com/fengjutian/geo-intelligence-engine/internal/members"
	"github.com/fengjutian/geo-intelligence-engine/internal/modelgateway"
	"github.com/fengjutian/geo-intelligence-engine/internal/monitoring"
	"github.com/fengjutian/geo-intelligence-engine/internal/notify"
	"github.com/fengjutian/geo-intelligence-engine/internal/projectmembers"
	"github.com/fengjutian/geo-intelligence-engine/internal/projects"
	"github.com/fengjutian/geo-intelligence-engine/internal/questions"
	"github.com/fengjutian/geo-intelligence-engine/internal/reviews"
	"github.com/fengjutian/geo-intelligence-engine/internal/serviceaccounts"
	"github.com/fengjutian/geo-intelligence-engine/internal/tasks"
)

type Dependencies struct {
	DB           *sql.DB
	Logger       *slog.Logger
	DevTenantID  string
	DevActorID   string
	WorkerToken  string
	ModelGateway *modelgateway.Gateway
	Embeddings   *embeddings.Client
	Notifier     *notify.Notifier
	ReportDir    string
	AuthMode     string
	AuthSecret   string
	DevRole      string
	OIDCIssuer   string
	OIDCAudience string
	OIDCJWKSURL  string
	OIDCClientID string
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
	mux.HandleFunc("GET /api/v1/projects/{projectId}/findings", projectAccess(deps.DB, crawlHandler.listProjectFindings))
	mux.HandleFunc("PATCH /api/v1/findings/{findingId}", requirePermission("edit_project", crawlHandler.updateFinding))
	mux.HandleFunc("POST /api/v1/crawl-runs/{crawlRunId}/content-analysis-runs", requirePermission("edit_project", crawlHandler.runContentAnalysis))
	mux.HandleFunc("GET /api/v1/content-analysis-runs/{analysisRunId}", crawlHandler.getContentAnalysisRun)
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
	questionHandler := newQuestionHandler(questions.NewStore(deps.DB), deps.DevTenantID, deps.Logger, deps.ModelGateway, deps.Embeddings)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/questions", projectAccess(deps.DB, questionHandler.list))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/questions", projectAccess(deps.DB, questionHandler.create))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/question-generation-runs", requirePermission("edit_project", projectAccess(deps.DB, questionHandler.generateQuestions)))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/questions:import", requirePermission("edit_project", projectAccess(deps.DB, questionHandler.importQuestions)))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/question-clusters:rebuild", requirePermission("edit_project", projectAccess(deps.DB, questionHandler.rebuildClusters)))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/question-clusters", projectAccess(deps.DB, questionHandler.listClusters))
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}/question-clusters/{clusterId}", requirePermission("edit_project", projectAccess(deps.DB, questionHandler.updateCluster)))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/question-clusters/{clusterId}/members", requirePermission("edit_project", projectAccess(deps.DB, questionHandler.moveClusterMember)))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/question-clusters/{clusterId}/split", requirePermission("edit_project", projectAccess(deps.DB, questionHandler.splitCluster)))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/question-clusters/{clusterId}/merge", requirePermission("edit_project", projectAccess(deps.DB, questionHandler.mergeClusters)))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/question-coverage", projectAccess(deps.DB, questionHandler.coverage))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/competitor-question-coverage", projectAccess(deps.DB, questionHandler.competitorCoverage))
	projectMemberHandler := newProjectMemberHandler(projectmembers.NewStore(deps.DB), deps.DevTenantID)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/members", projectAccess(deps.DB, projectMemberHandler.list))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/members", requirePermission("edit_project", projectAccess(deps.DB, projectMemberHandler.add)))
	mux.HandleFunc("DELETE /api/v1/projects/{projectId}/members/{userId}", requirePermission("edit_project", projectAccess(deps.DB, projectMemberHandler.remove)))
	auditTrendHandler := newAuditTrendHandler(audits.NewStore(deps.DB), deps.DevTenantID)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/audit-trends", projectAccess(deps.DB, auditTrendHandler.get))
	auditCompareHandler := newAuditCompareHandler(audits.NewStore(deps.DB), deps.DevTenantID)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/comparison", auditCompareHandler.get)
	answerHandler := newAnswerHandler(audits.NewStore(deps.DB), deps.DevTenantID)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/answers", answerHandler.list)
	shareHandler := newShareOfVoiceHandler(audits.NewStore(deps.DB), deps.DevTenantID)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/share-of-voice", shareHandler.get)
	questionTrendHandler := newQuestionTrendHandler(audits.NewStore(deps.DB), deps.DevTenantID)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/question-trends", projectAccess(deps.DB, questionTrendHandler.list))
	monitoringHandler := newMonitoringHandler(monitoring.NewStore(deps.DB, deps.Notifier), deps.DevTenantID)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/monitoring-schedules", projectAccess(deps.DB, monitoringHandler.list))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/monitoring-schedules", requirePermission("run_audit", projectAccess(deps.DB, monitoringHandler.create)))
	mux.HandleFunc("PATCH /api/v1/monitoring-schedules/{scheduleId}", requirePermission("run_audit", monitoringHandler.update))
	mux.HandleFunc("DELETE /api/v1/monitoring-schedules/{scheduleId}", requirePermission("run_audit", monitoringHandler.remove))
	mux.HandleFunc("POST /api/v1/monitoring-schedules/{scheduleId}/run", requirePermission("run_audit", monitoringHandler.runNow))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/monitoring-alerts", projectAccess(deps.DB, monitoringHandler.alerts))
	mux.HandleFunc("PATCH /api/v1/monitoring-alerts/{alertId}/resolve", requirePermission("run_audit", monitoringHandler.resolveAlert))
	factCheckHandler := newFactCheckHandler(factcheck.NewStore(deps.DB, deps.ModelGateway), deps.DevTenantID, deps.Logger)
	mux.HandleFunc("POST /api/v1/content-analysis-runs/{analysisRunId}/fact-checks", requirePermission("edit_project", factCheckHandler.verify))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/fact-checks", projectAccess(deps.DB, factCheckHandler.list))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/fact-checks", requirePermission("edit_project", projectAccess(deps.DB, factCheckHandler.verifyLatest)))
	mux.HandleFunc("PATCH /api/v1/fact-checks/{checkId}", requirePermission("edit_project", factCheckHandler.update))
	aliasHandler := newCompetitorAliasHandler(competitoraliases.NewStore(deps.DB), deps.DevTenantID)
	mux.HandleFunc("GET /api/v1/competitors/{competitorId}/aliases", aliasHandler.list)
	mux.HandleFunc("POST /api/v1/competitors/{competitorId}/aliases", requirePermission("edit_project", aliasHandler.create))
	mux.HandleFunc("DELETE /api/v1/competitors/{competitorId}/aliases/{aliasId}", requirePermission("edit_project", aliasHandler.remove))
	auditHandler := newAuditHandler(audits.NewStore(deps.DB), deps.DevTenantID, deps.Logger, deps.ReportDir)
	mux.HandleFunc("GET /api/v1/engine-configs", auditHandler.listEngineConfigs)
	mux.HandleFunc("POST /api/v1/engine-configs", auditHandler.createEngineConfig)
	mux.HandleFunc("POST /api/v1/question-sets/{questionSetId}/versions", auditHandler.publishQuestionSet)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/question-sets", projectAccess(deps.DB, auditHandler.listQuestionSets))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/measurement-protocols", projectAccess(deps.DB, auditHandler.listProtocols))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/measurement-protocols", projectAccess(deps.DB, auditHandler.createProtocol))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/knowledge-snapshots", requirePermission("edit_project", projectAccess(deps.DB, auditHandler.publishKnowledgeSnapshot)))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/knowledge-snapshots", projectAccess(deps.DB, auditHandler.listKnowledgeSnapshots))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/knowledge-snapshots/diff", projectAccess(deps.DB, auditHandler.compareKnowledgeSnapshots))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/content-quality", projectAccess(deps.DB, auditHandler.getContentQuality))
	mux.HandleFunc("POST /api/v1/measurement-protocols/{protocolAction}", auditHandler.publishProtocol)
	mux.HandleFunc("POST /api/v1/audits:estimate", auditHandler.estimateAudit)
	mux.HandleFunc("POST /api/v1/audits", requirePermission("run_audit", auditHandler.createAudit))
	mux.HandleFunc("GET /api/v1/audits/{auditId}", auditHandler.getAudit)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/samples", auditHandler.listSamples)
	mux.HandleFunc("GET /api/v1/audits/{auditId}/metrics", auditHandler.getMetrics)
	mux.HandleFunc("POST /api/v1/audits/{auditId}/metrics/recompute", requirePermission("approve_task", auditHandler.recomputeMetrics))
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
	mux.HandleFunc("GET /api/v1/auth-config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"mode":     deps.AuthMode,
			"issuer":   deps.OIDCIssuer,
			"clientId": deps.OIDCClientID,
			"audience": deps.OIDCAudience,
			"scope":    "openid profile email",
		})
	})
	memberHandler := newMemberHandler(members.NewStore(deps.DB))
	serviceAccountHandler := newServiceAccountHandler(serviceaccounts.NewStore(deps.DB), deps.DevTenantID)
	mux.HandleFunc("POST /api/v1/service-accounts", requirePermission("manage_keys", serviceAccountHandler.createAccount))
	mux.HandleFunc("GET /api/v1/service-accounts", requirePermission("manage_keys", serviceAccountHandler.listAccounts))
	mux.HandleFunc("POST /api/v1/api-tokens", requirePermission("manage_keys", serviceAccountHandler.createToken))
	mux.HandleFunc("GET /api/v1/api-tokens", requirePermission("manage_keys", serviceAccountHandler.listTokens))
	mux.HandleFunc("DELETE /api/v1/api-tokens/{tokenId}", requirePermission("manage_keys", serviceAccountHandler.revokeToken))
	mux.HandleFunc("GET /api/v1/tenants/{tenantId}/members", requirePermission("read_members", memberHandler.list))
	mux.HandleFunc("POST /api/v1/tenants/{tenantId}/invitations", requirePermission("manage_members", memberHandler.invite))
	mux.HandleFunc("PATCH /api/v1/tenants/{tenantId}/members/{memberId}", requirePermission("manage_members", memberHandler.update))
	mux.HandleFunc("POST /api/v1/invitations/{invitationAction}", memberHandler.accept)
	entityHandler := newEntityHandler(entities.NewStore(deps.DB), deps.DevTenantID, deps.Logger)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/entities", projectAccess(deps.DB, entityHandler.list))
	mux.HandleFunc("GET /api/v1/entities/{entityId}", entityHandler.get)
	reviewHandler := newReviewHandler(reviews.NewStore(deps.DB), deps.DevTenantID, deps.Logger)
	mux.HandleFunc("GET /api/v1/review-queue", reviewHandler.list)
	mux.HandleFunc("GET /api/v1/review-queue/stats", reviewHandler.stats)
	mux.HandleFunc("POST /api/v1/review-queue", requirePermission("edit_task", reviewHandler.enqueue))
	mux.HandleFunc("POST /api/v1/review-queue/batch-annotations", requirePermission("edit_task", reviewHandler.batchAnnotate))
	mux.HandleFunc("POST /api/v1/review-queue/batch-adjudications", requirePermission("approve_task", reviewHandler.batchAdjudicate))
	mux.HandleFunc("POST /api/v1/review-queue/batch-assignments", requirePermission("edit_task", reviewHandler.batchAssign))
	mux.HandleFunc("PATCH /api/v1/review-queue/{reviewItemId}", requirePermission("edit_task", reviewHandler.assign))
	mux.HandleFunc("POST /api/v1/review-queue/{reviewItemId}/annotations", requirePermission("edit_task", reviewHandler.annotate))
	mux.HandleFunc("POST /api/v1/review-queue/{reviewItemId}/adjudications", requirePermission("approve_task", reviewHandler.adjudicate))
	mux.HandleFunc("PATCH /api/v1/entities/{entityId}", requirePermission("edit_project", entityHandler.update))
	mux.HandleFunc("POST /api/v1/entities:merge", requirePermission("edit_project", entityHandler.merge))
	mux.HandleFunc("POST /api/v1/entity-merges/{mergeAction}", requirePermission("edit_project", entityHandler.undoMerge))

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
