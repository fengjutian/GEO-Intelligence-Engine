package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/crawls"
	"github.com/fengjutian/geo-intelligence-engine/internal/projects"
)

type Dependencies struct {
	DB          *sql.DB
	Logger      *slog.Logger
	DevTenantID string
	DevActorID  string
	WorkerToken string
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
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}", crawlHandler.get)
	mux.HandleFunc("POST /api/v1/crawl-runs/{runAction}", crawlHandler.controlAction)
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}/pages", crawlHandler.listPages)
	mux.HandleFunc("GET /api/v1/page-contents/{contentId}", crawlHandler.getPageContent)
	mux.HandleFunc("GET /api/v1/crawl-runs/{crawlRunId}/findings", crawlHandler.listFindings)
	mux.HandleFunc("POST /internal/crawler/v1/claim", crawlHandler.claim)
	mux.HandleFunc("POST /internal/crawler/v1/runs/{crawlRunId}/complete", crawlHandler.complete)
	mux.HandleFunc("POST /internal/crawler/v1/runs/{crawlRunId}/fail", crawlHandler.fail)

	return requestContext(deps.Logger, recoverer(deps.Logger, securityHeaders(mux)))
}
