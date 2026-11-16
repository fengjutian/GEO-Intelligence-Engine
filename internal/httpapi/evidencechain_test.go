package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fengjutian/geo-intelligence-engine/internal/evidencechain"
)

type fakeEvidenceChainStore struct {
	chain evidencechain.Chain
	err   error
}

func (s fakeEvidenceChainStore) Get(context.Context, string, string) (evidencechain.Chain, error) {
	return s.chain, s.err
}

func (s fakeEvidenceChainStore) Upsert(context.Context, string, string, string, evidencechain.UpsertInput) (evidencechain.Chain, error) {
	return s.chain, s.err
}

func (s fakeEvidenceChainStore) Candidates(context.Context, string, string, int) (evidencechain.Candidates, error) {
	return evidencechain.Candidates{}, s.err
}

func (s fakeEvidenceChainStore) Transition(context.Context, string, string, string, evidencechain.TransitionInput) (evidencechain.Chain, error) {
	return s.chain, s.err
}

func (s fakeEvidenceChainStore) Evaluate(context.Context, string, string, string) (evidencechain.Chain, error) {
	return s.chain, s.err
}

func TestEvidenceChainGetNotFound(t *testing.T) {
	handler := newEvidenceChainHandler(fakeEvidenceChainStore{err: evidencechain.ErrNotFound}, testTenantID, testTenantID, testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/findings/019a0000-0000-7000-8000-000000000001/evidence-chain", nil)
	request.SetPathValue("findingId", "019a0000-0000-7000-8000-000000000001")
	response := httptest.NewRecorder()
	handler.get(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "EVIDENCE_CHAIN_NOT_FOUND") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestEvidenceChainUpsertRejectsCrossProjectReference(t *testing.T) {
	handler := newEvidenceChainHandler(fakeEvidenceChainStore{err: evidencechain.ErrInvalidReference}, testTenantID, testTenantID, testLogger())
	body := `{"questionId":"019a0000-0000-7000-8000-000000000002","responseId":"019a0000-0000-7000-8000-000000000003","changeSummary":"Add direct answer","expectedOutcome":"Improve citations","verificationPlan":"Run the frozen protocol"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/findings/019a0000-0000-7000-8000-000000000001/evidence-chain", strings.NewReader(body))
	request.SetPathValue("findingId", "019a0000-0000-7000-8000-000000000001")
	response := httptest.NewRecorder()
	handler.upsert(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "EVIDENCE_REFERENCE_MISMATCH") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestEvidenceChainUpsertMapsInvalidInput(t *testing.T) {
	handler := newEvidenceChainHandler(fakeEvidenceChainStore{err: evidencechain.ErrInvalidInput}, testTenantID, testTenantID, testLogger())
	body := `{"questionId":"019a0000-0000-7000-8000-000000000002","responseId":"019a0000-0000-7000-8000-000000000003"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/findings/019a0000-0000-7000-8000-000000000001/evidence-chain", strings.NewReader(body))
	request.SetPathValue("findingId", "019a0000-0000-7000-8000-000000000001")
	response := httptest.NewRecorder()
	handler.upsert(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "INVALID_EVIDENCE_CHAIN") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
