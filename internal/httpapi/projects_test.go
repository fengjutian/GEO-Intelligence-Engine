package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fengjutian/geo-intelligence-engine/internal/projects"
)

const testTenantID = "019a7f64-7c2c-7000-8000-000000000001"

type fakeProjectStore struct {
	created projects.CreateInput
}

func (f *fakeProjectStore) Create(_ context.Context, _ string, input projects.CreateInput) (projects.Project, error) {
	f.created = input
	return projects.Project{ID: "019a7f64-7c2c-7000-8000-000000000002", Name: input.Name, Version: 1}, nil
}

func (f *fakeProjectStore) Get(_ context.Context, _, _ string) (projects.Project, error) {
	return projects.Project{}, projects.ErrNotFound
}

func (f *fakeProjectStore) List(_ context.Context, _ string, _ int) (projects.Page, error) {
	return projects.Page{Items: []projects.Project{}, Page: projects.PageMeta{HasMore: false}}, nil
}

func (f *fakeProjectStore) Update(_ context.Context, _, _ string, _ uint64, input projects.PatchInput) (projects.Project, error) {
	return projects.Project{ID: "019a7f64-7c2c-7000-8000-000000000002", Name: *input.Name, Version: 2}, nil
}

func (f *fakeProjectStore) Archive(_ context.Context, _, _ string, _ uint64) (projects.JobRef, error) {
	return projects.JobRef{JobID: "019a7f64-7c2c-7000-8000-000000000003", Status: "succeeded"}, nil
}

func (f *fakeProjectStore) PublishConfig(_ context.Context, _, _, _ string) (projects.ConfigVersion, error) {
	return projects.ConfigVersion{ID: "019a7f64-7c2c-7000-8000-000000000004", Version: 1}, nil
}

func (f *fakeProjectStore) GetBrandProfile(_ context.Context, _, _ string) (projects.BrandProfile, error) {
	return projects.BrandProfile{Aliases: []projects.BrandAlias{}, Products: []projects.Product{}}, nil
}

func (f *fakeProjectStore) CreateAlias(_ context.Context, _, _ string, input projects.AliasCreateInput) (projects.BrandAlias, error) {
	return projects.BrandAlias{ID: "019a7f64-7c2c-7000-8000-000000000005", Alias: input.Alias, AliasType: input.AliasType, Enabled: true}, nil
}

func (f *fakeProjectStore) SetAliasEnabled(_ context.Context, _, _, aliasID string, enabled bool) (projects.BrandAlias, error) {
	return projects.BrandAlias{ID: aliasID, Enabled: enabled}, nil
}

func (f *fakeProjectStore) CreateProduct(_ context.Context, _, _ string, input projects.ProductCreateInput) (projects.Product, error) {
	return projects.Product{ID: "019a7f64-7c2c-7000-8000-000000000006", Name: input.Name}, nil
}

func (f *fakeProjectStore) CreateCompetitor(_ context.Context, _, _ string, input projects.CompetitorCreateInput) (projects.Competitor, error) {
	return projects.Competitor{ID: "019a7f64-7c2c-7000-8000-000000000007", Name: input.Name}, nil
}

func TestCreateProjectValidatesAndNormalizesInput(t *testing.T) {
	store := &fakeProjectStore{}
	handler := newProjectHandler(store, testTenantID, testTenantID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"name":"  GEO Project ","brandName":" Acme ","officialUrl":"https://example.com/","locale":"zh-CN","region":"CN","timezone":"Asia/Shanghai"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(body))
	response := httptest.NewRecorder()

	handler.create(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.created.Name != "GEO Project" || store.created.BrandName != "Acme" {
		t.Fatalf("input was not normalized: %#v", store.created)
	}
	if response.Header().Get("ETag") != `"1"` {
		t.Fatalf("ETag = %q", response.Header().Get("ETag"))
	}
}

func TestCreateProjectRejectsPrivateOfficialURL(t *testing.T) {
	store := &fakeProjectStore{}
	handler := newProjectHandler(store, testTenantID, testTenantID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"name":"Project","brandName":"Acme","officialUrl":"http://127.0.0.1/","locale":"zh-CN","region":"CN","timezone":"Asia/Shanghai"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(body))
	response := httptest.NewRecorder()

	handler.create(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestListProjectsRejectsInvalidLimit(t *testing.T) {
	handler := newProjectHandler(&fakeProjectStore{}, testTenantID, testTenantID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects?limit=101", nil)
	response := httptest.NewRecorder()

	handler.list(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestUpdateProjectRequiresIfMatch(t *testing.T) {
	handler := newProjectHandler(&fakeProjectStore{}, testTenantID, testTenantID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/019a7f64-7c2c-7000-8000-000000000002", strings.NewReader(`{"name":"Updated"}`))
	request.SetPathValue("projectId", "019a7f64-7c2c-7000-8000-000000000002")
	response := httptest.NewRecorder()

	handler.update(response, request)

	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d", response.Code)
	}
}
