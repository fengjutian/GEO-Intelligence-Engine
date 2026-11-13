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

func TestCreateProjectValidatesAndNormalizesInput(t *testing.T) {
	store := &fakeProjectStore{}
	handler := newProjectHandler(store, testTenantID, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	handler := newProjectHandler(store, testTenantID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"name":"Project","brandName":"Acme","officialUrl":"http://127.0.0.1/","locale":"zh-CN","region":"CN","timezone":"Asia/Shanghai"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(body))
	response := httptest.NewRecorder()

	handler.create(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestListProjectsRejectsInvalidLimit(t *testing.T) {
	handler := newProjectHandler(&fakeProjectStore{}, testTenantID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects?limit=101", nil)
	response := httptest.NewRecorder()

	handler.list(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}
