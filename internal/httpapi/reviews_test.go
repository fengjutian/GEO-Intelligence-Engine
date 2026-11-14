package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fengjutian/geo-intelligence-engine/internal/reviews"
)

type fakeReviewStore struct {
	batchAnnotate   func(context.Context, string, string, []reviews.AnnotationInput) (reviews.BatchResult, error)
	batchAdjudicate func(context.Context, string, string, []reviews.AdjudicationInput) (reviews.BatchResult, error)
	batchAssign     func(context.Context, string, string, []string) (reviews.BatchResult, error)
}

func (f *fakeReviewStore) List(context.Context, string, string, string, int) ([]reviews.Item, error) {
	return nil, nil
}

func (f *fakeReviewStore) Annotate(context.Context, string, string, string, map[string]any, string) (reviews.Annotation, error) {
	return reviews.Annotation{}, nil
}

func (f *fakeReviewStore) EnqueueManual(context.Context, string, string, string, string) error {
	return nil
}

func (f *fakeReviewStore) Assign(context.Context, string, string, string) error { return nil }

func (f *fakeReviewStore) Adjudicate(context.Context, string, string, string, map[string]any, string) (reviews.Adjudication, error) {
	return reviews.Adjudication{}, nil
}

func (f *fakeReviewStore) Stats(context.Context, string) (map[string]any, error) {
	return map[string]any{}, nil
}

func (f *fakeReviewStore) BatchAnnotate(ctx context.Context, tenantID, annotatorID string, inputs []reviews.AnnotationInput) (reviews.BatchResult, error) {
	return f.batchAnnotate(ctx, tenantID, annotatorID, inputs)
}

func (f *fakeReviewStore) BatchAdjudicate(ctx context.Context, tenantID, adjudicatorID string, inputs []reviews.AdjudicationInput) (reviews.BatchResult, error) {
	return f.batchAdjudicate(ctx, tenantID, adjudicatorID, inputs)
}

func (f *fakeReviewStore) BatchAssign(ctx context.Context, tenantID, assigneeID string, itemIDs []string) (reviews.BatchResult, error) {
	return f.batchAssign(ctx, tenantID, assigneeID, itemIDs)
}

func newTestReviewHandler(store reviewStore) *reviewHandler {
	return newReviewHandler(store, "00000000-0000-0000-0000-000000000001", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestBatchAnnotateReturnsPerItemResult(t *testing.T) {
	store := &fakeReviewStore{
		batchAnnotate: func(_ context.Context, _, annotatorID string, inputs []reviews.AnnotationInput) (reviews.BatchResult, error) {
			if annotatorID != "" {
				t.Fatalf("annotator id = %q", annotatorID)
			}
			if len(inputs) != 1 || inputs[0].ReviewItemID != "11111111-1111-1111-1111-111111111111" {
				t.Fatalf("unexpected inputs: %+v", inputs)
			}
			return reviews.BatchResult{
				Applied: 1,
				Results: []reviews.BatchOutcome{{ReviewItemID: inputs[0].ReviewItemID, Status: "applied"}},
			}, nil
		},
	}
	handler := newTestReviewHandler(store)
	body := `{"items":[{"reviewItemId":"11111111-1111-1111-1111-111111111111","labels":{"decision":"agree"},"reason":"checked"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/review-queue/batch-annotations", strings.NewReader(body))
	res := httptest.NewRecorder()
	handler.batchAnnotate(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var result reviews.BatchResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Applied != 1 || len(result.Results) != 1 || result.Results[0].Status != "applied" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestBatchAdjudicateRejectsEmptyBatch(t *testing.T) {
	store := &fakeReviewStore{
		batchAdjudicate: func(context.Context, string, string, []reviews.AdjudicationInput) (reviews.BatchResult, error) {
			return reviews.BatchResult{}, reviews.ErrInvalid
		},
	}
	handler := newTestReviewHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/review-queue/batch-adjudications", strings.NewReader(`{"items":[]}`))
	res := httptest.NewRecorder()
	handler.batchAdjudicate(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "REVIEW_INPUT_INVALID") {
		t.Fatalf("missing problem code: %s", res.Body.String())
	}
}

func TestBatchAssignPassesAssigneeAndItems(t *testing.T) {
	var gotAssignee string
	var gotItems []string
	store := &fakeReviewStore{
		batchAssign: func(_ context.Context, _, assigneeID string, itemIDs []string) (reviews.BatchResult, error) {
			gotAssignee, gotItems = assigneeID, itemIDs
			return reviews.BatchResult{Applied: len(itemIDs), Results: []reviews.BatchOutcome{}}, nil
		},
	}
	handler := newTestReviewHandler(store)
	body := `{"assigneeId":"22222222-2222-2222-2222-222222222222","reviewItemIds":["33333333-3333-3333-3333-333333333333"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/review-queue/batch-assignments", strings.NewReader(body))
	res := httptest.NewRecorder()
	handler.batchAssign(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	if gotAssignee != "22222222-2222-2222-2222-222222222222" || len(gotItems) != 1 {
		t.Fatalf("assignee=%q items=%v", gotAssignee, gotItems)
	}
}

func TestBatchAnnotateRejectsUnknownFields(t *testing.T) {
	store := &fakeReviewStore{
		batchAnnotate: func(context.Context, string, string, []reviews.AnnotationInput) (reviews.BatchResult, error) {
			return reviews.BatchResult{}, nil
		},
	}
	handler := newTestReviewHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/review-queue/batch-annotations", strings.NewReader(`{"unexpected":true}`))
	res := httptest.NewRecorder()
	handler.batchAnnotate(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
}
