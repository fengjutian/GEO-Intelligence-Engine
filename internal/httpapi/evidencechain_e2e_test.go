package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fengjutian/geo-intelligence-engine/internal/evidencechain"
	_ "github.com/go-sql-driver/mysql"
)

// TestEvidenceChainRealAuditE2E exercises the HTTP contract against MySQL data shaped
// exactly like two completed audits. It is skipped unless GEO_TEST_DATABASE_DSN is set.
func TestEvidenceChainRealAuditE2E(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GEO_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("set GEO_TEST_DATABASE_DSN to run the real-audit end-to-end test")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

	ids := realAuditFixtureIDs()
	seedRealAuditFixture(t, db, ids)
	t.Cleanup(func() { cleanupRealAuditFixture(t, db, ids) })

	handler := newEvidenceChainHandler(evidencechain.NewStore(db), ids.tenant, ids.actor, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/findings/{findingId}/evidence-candidates", handler.candidates)
	mux.HandleFunc("GET /api/v1/findings/{findingId}/evidence-chain", handler.get)
	mux.HandleFunc("PUT /api/v1/findings/{findingId}/evidence-chain", handler.upsert)
	mux.HandleFunc("POST /api/v1/findings/{findingId}/evidence-chain/transition", handler.transition)
	mux.HandleFunc("POST /api/v1/findings/{findingId}/evidence-chain/evaluate", handler.evaluate)

	candidates := doEvidenceRequest(t, mux, http.MethodGet, "/api/v1/findings/"+ids.finding+"/evidence-candidates", nil, http.StatusOK)
	var candidatePayload struct {
		Answers []struct {
			ResponseID string `json:"responseId"`
		} `json:"answers"`
	}
	decodeEvidenceResponse(t, candidates, &candidatePayload)
	hasBaseline := false
	for _, answer := range candidatePayload.Answers {
		hasBaseline = hasBaseline || answer.ResponseID == ids.baselineResponse
	}
	if len(candidatePayload.Answers) != 2 || !hasBaseline {
		t.Fatalf("unexpected candidate answers: %+v", candidatePayload.Answers)
	}

	created := doEvidenceRequest(t, mux, http.MethodPut, "/api/v1/findings/"+ids.finding+"/evidence-chain", map[string]any{
		"questionId": ids.question, "responseId": ids.baselineResponse, "pageContentId": ids.pageContent,
		"changeSummary": "Add a concise evidence-backed product answer", "expectedOutcome": "Increase mention rate",
		"verificationPlan": "Repeat the same protocol after publishing", "metricCode": "mention_rate", "targetDelta": 0.1,
	}, http.StatusOK)
	var chain evidencechain.Chain
	decodeEvidenceResponse(t, created, &chain)
	if chain.Status != evidencechain.StatusEvidenceReady || chain.Snapshot.Baseline.Value == nil || *chain.Snapshot.Baseline.Value != 0.2 {
		t.Fatalf("unexpected baseline chain: status=%s baseline=%+v", chain.Status, chain.Snapshot.Baseline)
	}

	for _, transition := range []struct {
		status, auditID string
	}{{evidencechain.StatusPlanned, ""}, {evidencechain.StatusImplementing, ""}, {evidencechain.StatusAwaitingRetest, ""}, {evidencechain.StatusEvaluating, ids.retestAudit}} {
		response := doEvidenceRequest(t, mux, http.MethodPost, "/api/v1/findings/"+ids.finding+"/evidence-chain/transition", map[string]any{"targetStatus": transition.status, "reason": "e2e progression", "retestAuditId": transition.auditID}, http.StatusOK)
		decodeEvidenceResponse(t, response, &chain)
		if chain.Status != transition.status {
			t.Fatalf("transition wanted %s, got %s", transition.status, chain.Status)
		}
	}

	evaluated := doEvidenceRequest(t, mux, http.MethodPost, "/api/v1/findings/"+ids.finding+"/evidence-chain/evaluate", nil, http.StatusOK)
	decodeEvidenceResponse(t, evaluated, &chain)
	if chain.Status != evidencechain.StatusVerified || chain.Comparison == nil || !chain.Comparison.Comparable || chain.Comparison.Decision != evidencechain.OutcomeImproved {
		t.Fatalf("unexpected automatic decision: status=%s comparison=%+v", chain.Status, chain.Comparison)
	}
	if chain.Comparison.Delta == nil || *chain.Comparison.Delta != 0.4 || len(chain.Events) != 6 {
		t.Fatalf("unexpected delta/events: comparison=%+v events=%d", chain.Comparison, len(chain.Events))
	}
}

type evidenceFixtureIDs struct {
	tenant, actor, project, website, crawl, page, pageContent, finding                             string
	questionSet, questionVersion, question, engine, protocol, knowledge                            string
	baselineAudit, baselineSample, baselineRequest, baselineResponse, baselineEval, baselineMetric string
	retestAudit, retestSample, retestRequest, retestResponse, retestEval, retestMetric             string
}

func realAuditFixtureIDs() evidenceFixtureIDs {
	return evidenceFixtureIDs{
		tenant: "019b1000-0000-7000-8000-000000000001", actor: "019b1000-0000-7000-8000-000000000002", project: "019b1000-0000-7000-8000-000000000003",
		website: "019b1000-0000-7000-8000-000000000004", crawl: "019b1000-0000-7000-8000-000000000005", page: "019b1000-0000-7000-8000-000000000006", pageContent: "019b1000-0000-7000-8000-000000000007", finding: "019b1000-0000-7000-8000-000000000008",
		questionSet: "019b1000-0000-7000-8000-000000000009", questionVersion: "019b1000-0000-7000-8000-00000000000a", question: "019b1000-0000-7000-8000-00000000000b", engine: "019b1000-0000-7000-8000-00000000000c", protocol: "019b1000-0000-7000-8000-00000000000d", knowledge: "019b1000-0000-7000-8000-00000000000e",
		baselineAudit: "019b1000-0000-7000-8000-00000000000f", baselineSample: "019b1000-0000-7000-8000-000000000010", baselineRequest: "019b1000-0000-7000-8000-000000000011", baselineResponse: "019b1000-0000-7000-8000-000000000012", baselineEval: "019b1000-0000-7000-8000-000000000013", baselineMetric: "019b1000-0000-7000-8000-000000000014",
		retestAudit: "019b1000-0000-7000-8000-000000000015", retestSample: "019b1000-0000-7000-8000-000000000016", retestRequest: "019b1000-0000-7000-8000-000000000017", retestResponse: "019b1000-0000-7000-8000-000000000018", retestEval: "019b1000-0000-7000-8000-000000000019", retestMetric: "019b1000-0000-7000-8000-00000000001a",
	}
}

func seedRealAuditFixture(t *testing.T, db *sql.DB, id evidenceFixtureIDs) {
	t.Helper()
	cleanupRealAuditFixture(t, db, id)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO tenants (id,name,status,plan_code,data_region,created_at,updated_at) VALUES (UUID_TO_BIN(?),'Evidence E2E','active','test','local',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{id.tenant}},
		{`INSERT INTO projects (id,tenant_id,name,status,timezone,default_locale,created_at,updated_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'Evidence E2E Project','active','UTC','zh-CN',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{id.project, id.tenant}},
		{`INSERT INTO websites (id,tenant_id,project_id,base_url,normalized_host,ownership_status,crawl_policy,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'https://e2e.invalid','e2e.invalid','verified',JSON_OBJECT(),UTC_TIMESTAMP(6))`, []any{id.website, id.tenant, id.project}},
		{`INSERT INTO crawl_runs (id,tenant_id,project_id,website_id,status,config_snapshot,discovered_count,fetched_count,created_at,finished_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'succeeded',JSON_OBJECT(),1,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{id.crawl, id.tenant, id.project, id.website}},
		{`INSERT INTO website_pages (id,tenant_id,website_id,canonical_url,normalized_url_hash,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'https://e2e.invalid/product',UNHEX(SHA2('page-url',256)),UTC_TIMESTAMP(6))`, []any{id.page, id.tenant, id.website}},
		{`INSERT INTO page_contents (id,tenant_id,page_id,crawl_run_id,http_status,final_url,title,language,content_hash,parser_version,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),200,'https://e2e.invalid/product','Evidence product page','zh-CN',UNHEX(SHA2('page-v1',256)),'e2e-parser',UTC_TIMESTAMP(6))`, []any{id.pageContent, id.tenant, id.page, id.crawl}},
		{`UPDATE website_pages SET latest_content_id=UUID_TO_BIN(?) WHERE id=UUID_TO_BIN(?)`, []any{id.pageContent, id.page}},
		{`INSERT INTO findings (id,tenant_id,project_id,source_type,source_id,source_version,rule_code,severity,confidence,status,summary,evidence_json,created_at,updated_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'page_content',UUID_TO_BIN(?),'v1','GEO_ANSWER_GAP','high',0.95,'open','Product answer lacks verifiable facts',JSON_OBJECT('missing','facts'),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{id.finding, id.tenant, id.project, id.pageContent}},
		{`INSERT INTO question_sets (id,tenant_id,project_id,name,status,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'E2E Questions','published',UTC_TIMESTAMP(6))`, []any{id.questionSet, id.tenant, id.project}},
		{`INSERT INTO question_set_versions (id,tenant_id,question_set_id,version,content_hash,published_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),1,UNHEX(SHA2('questions-v1',256)),UTC_TIMESTAMP(6))`, []any{id.questionVersion, id.tenant, id.questionSet}},
		{`INSERT INTO questions (id,tenant_id,version_id,text_value,text_hash,language,question_type,weight,status,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'Which GEO product provides verifiable evidence?',UNHEX(SHA2('e2e-question',256)),'zh-CN','comparison',1,'active',UTC_TIMESTAMP(6))`, []any{id.question, id.tenant, id.questionVersion}},
		{`INSERT INTO engine_configs (id,tenant_id,provider,channel,credential_ref,region,limits_json,status,created_at,updated_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'deepseek','model_api','e2e-secret','CN',JSON_OBJECT(),'active',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{id.engine, id.tenant}},
		{`INSERT INTO measurement_protocols (id,tenant_id,project_id,semantic_version,config_json,config_hash,status,created_at,published_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'1.0.0',JSON_OBJECT('promptTemplateVersion','prompt-v1'),UNHEX(SHA2('protocol-v1',256)),'published',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{id.protocol, id.tenant, id.project}},
		{`INSERT INTO knowledge_snapshots (id,tenant_id,project_id,version,content_hash,snapshot_object_key,published_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),1,UNHEX(SHA2('knowledge-v1',256)),'db://e2e/knowledge',UTC_TIMESTAMP(6))`, []any{id.knowledge, id.tenant, id.project}},
	}
	for _, audit := range []struct {
		audit, sample, request, response, eval, metric string
		value, low, high                               float64
		suffix                                         string
	}{
		{id.baselineAudit, id.baselineSample, id.baselineRequest, id.baselineResponse, id.baselineEval, id.baselineMetric, 0.2, 0.1, 0.3, "baseline"},
		{id.retestAudit, id.retestSample, id.retestRequest, id.retestResponse, id.retestEval, id.retestMetric, 0.6, 0.5, 0.7, "retest"},
	} {
		statements = append(statements,
			struct {
				query string
				args  []any
			}{`INSERT INTO audits (id,tenant_id,project_id,protocol_id,question_version_id,knowledge_snapshot_id,status,planned_count,completed_count,eligible_count,budget_microunits,currency,idempotency_key,created_at,started_at,finished_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'succeeded',100,100,100,1000,'CNY',?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{audit.audit, id.tenant, id.project, id.protocol, id.questionVersion, id.knowledge, "e2e-" + audit.suffix}},
			struct {
				query string
				args  []any
			}{`INSERT INTO audit_samples (id,tenant_id,audit_id,question_id,engine_config_id,model_selector,channel,region,language,sample_no,sample_key,status,dimensions_json,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'deepseek-chat','model_api','CN','zh-CN',1,UNHEX(SHA2(?,256)),'succeeded',JSON_OBJECT(),UTC_TIMESTAMP(6))`, []any{audit.sample, id.tenant, audit.audit, id.question, id.engine, "sample-" + audit.suffix}},
			struct {
				query string
				args  []any
			}{`INSERT INTO ai_requests (id,tenant_id,sample_id,provider,request_object_key,request_hash,parameters_json,idempotency_key,sent_at,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'deepseek',?,UNHEX(SHA2(?,256)),JSON_OBJECT(),?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{audit.request, id.tenant, audit.sample, "db://e2e/" + audit.suffix, "request-" + audit.suffix, "request-" + audit.suffix}},
			struct {
				query string
				args  []any
			}{`INSERT INTO ai_responses (id,tenant_id,sample_id,request_id,provider_request_id,resolved_model,transport_status,content_text,finish_reason,created_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,'deepseek-chat','succeeded',?,'stop',UTC_TIMESTAMP(6))`, []any{audit.response, id.tenant, audit.sample, audit.request, audit.suffix, "Evidence answer " + audit.suffix}},
			struct {
				query string
				args  []any
			}{`INSERT INTO evaluation_runs (id,tenant_id,audit_id,judge_version,eligibility_rule_version,config_hash,status,created_at,finished_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'judge-v1','eligibility-v1',UNHEX(SHA2(?,256)),'succeeded',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, []any{audit.eval, id.tenant, audit.audit, "eval-" + audit.suffix}},
			struct {
				query string
				args  []any
			}{`INSERT INTO metric_snapshots (id,tenant_id,audit_id,evaluation_run_id,metric_version,status,generated_at) VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'metrics-v1','ready',UTC_TIMESTAMP(6))`, []any{audit.metric, id.tenant, audit.audit, audit.eval}},
			struct {
				query string
				args  []any
			}{`INSERT INTO metric_values (id,tenant_id,snapshot_id,metric_code,aggregation_method,dimension_hash,dimensions_json,numerator,denominator,value_decimal,ci_low,ci_high,interval_method,created_at) VALUES (UUID_TO_BIN(UUID()),UUID_TO_BIN(?),UUID_TO_BIN(?),'mention_rate','answer_equal',UNHEX(SHA2(?,256)),JSON_OBJECT('channel','model_api'),?,100,?,?,?,'wilson',UTC_TIMESTAMP(6))`, []any{id.tenant, audit.metric, "metric-" + audit.suffix, audit.value * 100, audit.value, audit.low, audit.high}},
		)
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed fixture: %v\nquery: %s", err, statement.query)
		}
	}
}

func cleanupRealAuditFixture(t *testing.T, db *sql.DB, id evidenceFixtureIDs) {
	t.Helper()
	ctx := context.Background()
	queries := []string{
		`DELETE FROM finding_evidence_chain_events WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM finding_evidence_chains WHERE tenant_id=UUID_TO_BIN(?)`,
		`DELETE FROM metric_values WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM metric_snapshots WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM evaluation_runs WHERE tenant_id=UUID_TO_BIN(?)`,
		`DELETE FROM ai_responses WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM ai_requests WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM audit_samples WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM audits WHERE tenant_id=UUID_TO_BIN(?)`,
		`DELETE FROM findings WHERE tenant_id=UUID_TO_BIN(?)`, `UPDATE website_pages SET latest_content_id=NULL WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM page_contents WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM website_pages WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM crawl_runs WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM websites WHERE tenant_id=UUID_TO_BIN(?)`,
		`DELETE FROM questions WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM question_set_versions WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM question_sets WHERE tenant_id=UUID_TO_BIN(?)`,
		`DELETE FROM knowledge_snapshots WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM measurement_protocols WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM engine_configs WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM projects WHERE tenant_id=UUID_TO_BIN(?)`, `DELETE FROM tenants WHERE id=UUID_TO_BIN(?)`,
	}
	for _, query := range queries {
		if _, err := db.ExecContext(ctx, query, id.tenant); err != nil {
			t.Logf("cleanup fixture: %v", err)
		}
	}
}

func doEvidenceRequest(t *testing.T, handler http.Handler, method, path string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, response.Code, response.Body.String())
	}
	return response
}

func decodeEvidenceResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatal(err)
	}
}
