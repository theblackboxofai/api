package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerReturnsOpenAIListShapeForRepositorySelectedModels(t *testing.T) {
	t.Parallel()

	repo := fakeRepository{
		records: []Record{
			{ID: "alpha:cloud", CreatedAt: time.Unix(1700000000, 0).UTC()},
			{ID: "", CreatedAt: time.Unix(1700000100, 0).UTC()},
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
		t.Fatalf("expected 2 models, got %d", len(response.Data))
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

func TestStatsHandlerReturnsMappedServerCounts(t *testing.T) {
	t.Parallel()

	repo := fakeRepository{
		stats: []StatRecord{
			{ID: "alpha:cloud", ServerCount: 3},
			{ID: "", ServerCount: 99},
			{ID: "gamma:cloud", ServerCount: 1},
		},
	}

	handler := NewStatsHandler(NewService(repo, "blackbox", StaticModelMapper{
		models: map[string]string{
			"alpha:cloud": "provider/alpha",
			"gamma:cloud": "provider/gamma",
		},
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Object != "list" {
		t.Fatalf("expected object=list, got %q", response.Object)
	}

	if len(response.Data) != 2 {
		t.Fatalf("expected 2 stats entries, got %d", len(response.Data))
	}

	if response.Data[0].ID != "provider/alpha" || response.Data[0].ServerCount != 3 {
		t.Fatalf("expected provider/alpha count 3, got %#v", response.Data[0])
	}

	if response.Data[1].ID != "provider/gamma" || response.Data[1].ServerCount != 1 {
		t.Fatalf("expected provider/gamma count 1, got %#v", response.Data[1])
	}
}

type fakeRepository struct {
	records []Record
	stats   []StatRecord
	err     error
}

func (f fakeRepository) ListCloudModels(context.Context) ([]Record, error) {
	return f.records, f.err
}

func (f fakeRepository) ListCloudModelStats(context.Context) ([]StatRecord, error) {
	return f.stats, f.err
}
