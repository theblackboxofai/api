package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHandlerHidesModelsMappedToEmptyString(t *testing.T) {
	t.Parallel()

	repo := fakeRepository{
		records: []Record{
			{ID: "alpha:cloud", CreatedAt: time.Unix(1700000000, 0).UTC()},
			{ID: "beta:cloud", CreatedAt: time.Unix(1700000100, 0).UTC()},
			{ID: "gamma:cloud", CreatedAt: time.Unix(1700000200, 0).UTC()},
		},
	}

	handler := NewHandler(NewService(repo, "blackbox", StaticModelMapper{
		models: map[string]string{
			"alpha:cloud": "provider/alpha",
			"beta:cloud":  "",
			"gamma:cloud": "provider/gamma",
		},
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var response ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(response.Data) != 2 {
		t.Fatalf("expected 2 visible models, got %d", len(response.Data))
	}

	for _, model := range response.Data {
		if model.ID == "" || model.ID == "beta:cloud" {
			t.Fatalf("expected disabled model to be omitted, got %#v", response.Data)
		}
	}
}

func TestStatsHandlerReturnsMappedServerCounts(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	repo := fakeRepository{
		stats: []StatRecord{
			{ID: "alpha:cloud", ServerCount: 3},
			{ID: "", ServerCount: 99},
			{ID: "gamma:cloud", ServerCount: 1},
		},
		history: []RequestHistoryRecord{
			{
				CreatedAt:    now.Add(-2 * time.Hour),
				RequestID:    "req-1",
				Success:      false,
				ResponseBody: "",
			},
			{
				CreatedAt:    now.Add(-90 * time.Minute),
				RequestID:    "req-1",
				Success:      true,
				ResponseBody: `{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			},
			{
				CreatedAt: now.Add(-3 * 24 * time.Hour),
				RequestID: "req-2",
				Success:   true,
				ResponseBody: strings.Join([]string{
					`data: {"id":"chatcmpl-1"}`,
					`data: {"usage":{"prompt_tokens":20,"completion_tokens":7,"total_tokens":27}}`,
					`data: [DONE]`,
				}, "\n"),
			},
			{
				CreatedAt:    now.Add(-10 * 24 * time.Hour),
				RequestID:    "req-3",
				Success:      false,
				ResponseBody: "",
			},
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

	if response.Object != "stats" {
		t.Fatalf("expected object=stats, got %q", response.Object)
	}

	if response.Models.Object != "list" {
		t.Fatalf("expected models.object=list, got %q", response.Models.Object)
	}

	if len(response.Models.Data) != 2 {
		t.Fatalf("expected 2 stats entries, got %d", len(response.Models.Data))
	}

	if response.Models.Data[0].ID != "provider/alpha" || response.Models.Data[0].ServerCount != 3 {
		t.Fatalf("expected provider/alpha count 3, got %#v", response.Models.Data[0])
	}

	if response.Models.Data[1].ID != "provider/gamma" || response.Models.Data[1].ServerCount != 1 {
		t.Fatalf("expected provider/gamma count 1, got %#v", response.Models.Data[1])
	}

	if response.RequestHistory.Last24Hours.RequestCount != 1 {
		t.Fatalf("expected last_24_hours request_count 1, got %d", response.RequestHistory.Last24Hours.RequestCount)
	}

	if response.RequestHistory.Last24Hours.AttemptCount != 2 {
		t.Fatalf("expected last_24_hours attempt_count 2, got %d", response.RequestHistory.Last24Hours.AttemptCount)
	}

	if response.RequestHistory.Last24Hours.SuccessCount != 1 || response.RequestHistory.Last24Hours.FailureCount != 1 {
		t.Fatalf("expected last_24_hours success/failure 1/1, got %#v", response.RequestHistory.Last24Hours)
	}

	if response.RequestHistory.Last24Hours.TotalTokens != 15 {
		t.Fatalf("expected last_24_hours total_tokens 15, got %d", response.RequestHistory.Last24Hours.TotalTokens)
	}

	if response.RequestHistory.Last7Days.RequestCount != 2 || response.RequestHistory.Last7Days.TotalTokens != 42 {
		t.Fatalf("expected last_7_days request_count 2 and total_tokens 42, got %#v", response.RequestHistory.Last7Days)
	}

	if response.RequestHistory.Last28Days.RequestCount != 3 || response.RequestHistory.Last28Days.AttemptCount != 4 {
		t.Fatalf("expected last_28_days request_count 3 and attempt_count 4, got %#v", response.RequestHistory.Last28Days)
	}

	if response.RequestHistory.Last28Days.SuccessCount != 2 || response.RequestHistory.Last28Days.FailureCount != 2 {
		t.Fatalf("expected last_28_days success/failure 2/2, got %#v", response.RequestHistory.Last28Days)
	}
}

func TestStatsHandlerHidesModelsMappedToEmptyString(t *testing.T) {
	t.Parallel()

	repo := fakeRepository{
		stats: []StatRecord{
			{ID: "alpha:cloud", ServerCount: 3},
			{ID: "beta:cloud", ServerCount: 99},
			{ID: "gamma:cloud", ServerCount: 1},
		},
	}

	handler := NewStatsHandler(NewService(repo, "blackbox", StaticModelMapper{
		models: map[string]string{
			"alpha:cloud": "provider/alpha",
			"beta:cloud":  "",
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

	if len(response.Models.Data) != 2 {
		t.Fatalf("expected 2 visible stats entries, got %d", len(response.Models.Data))
	}

	for _, model := range response.Models.Data {
		if model.ID == "" || model.ID == "beta:cloud" {
			t.Fatalf("expected disabled model stats to be omitted, got %#v", response.Models.Data)
		}
	}
}

type fakeRepository struct {
	records []Record
	stats   []StatRecord
	history []RequestHistoryRecord
	err     error
}

func (f fakeRepository) ListCloudModels(context.Context) ([]Record, error) {
	return f.records, f.err
}

func (f fakeRepository) ListCloudModelStats(context.Context) ([]StatRecord, error) {
	return f.stats, f.err
}

func (f fakeRepository) ListRequestHistory(context.Context, time.Time) ([]RequestHistoryRecord, error) {
	return f.history, f.err
}
