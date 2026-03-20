package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerReturnsOpenAIListShapeAndFiltersNonCloudModels(t *testing.T) {
	t.Parallel()

	repo := fakeRepository{
		records: []Record{
			{ID: "alpha:cloud", CreatedAt: time.Unix(1700000000, 0).UTC()},
			{ID: "beta", CreatedAt: time.Unix(1700000100, 0).UTC()},
			{ID: "gamma:cloud", CreatedAt: time.Unix(1700000200, 0).UTC()},
		},
	}

	handler := NewHandler(NewService(repo, "blackbox", StaticModelMapper{
		models: map[string]string{
			"alpha:cloud": "provider/alpha",
			"gamma:cloud": "provider/gamma",
		},
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected application/json content type, got %q", contentType)
	}

	var response ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Object != "list" {
		t.Fatalf("expected object=list, got %q", response.Object)
	}

	if len(response.Data) != 2 {
		t.Fatalf("expected 2 cloud models, got %d", len(response.Data))
	}

	if response.Data[0].ID != "provider/alpha" {
		t.Fatalf("expected first model provider/alpha, got %q", response.Data[0].ID)
	}

	if response.Data[0].Object != "model" {
		t.Fatalf("expected model object type, got %q", response.Data[0].Object)
	}

	if response.Data[0].OwnedBy != "blackbox" {
		t.Fatalf("expected owned_by=blackbox, got %q", response.Data[0].OwnedBy)
	}

	if response.Data[0].Created != 1700000000 {
		t.Fatalf("expected created timestamp 1700000000, got %d", response.Data[0].Created)
	}

	if response.Data[1].ID != "provider/gamma" {
		t.Fatalf("expected second model provider/gamma, got %q", response.Data[1].ID)
	}
}

type fakeRepository struct {
	records []Record
	err     error
}

func (f fakeRepository) ListCloudModels(context.Context) ([]Record, error) {
	return f.records, f.err
}
