package models

import (
	"context"
	"log"
	"time"
)

const (
	CloudTag         = ":cloud"
	OllamaRemoteHost = "https://ollama.com:443"
)

type Repository interface {
	ListCloudModels(ctx context.Context) ([]Record, error)
	ListCloudModelStats(ctx context.Context) ([]StatRecord, error)
	ListRequestHistory(ctx context.Context, since time.Time) ([]RequestHistoryRecord, error)
}

type Service struct {
	debug  bool
	mapper ModelMapper
	repo   Repository
	owner  string
}

type Record struct {
	ID        string
	CreatedAt time.Time
}

type StatRecord struct {
	ID          string
	ServerCount int
}

type RequestHistoryRecord struct {
	CreatedAt    time.Time
	RequestID    string
	Success      bool
	ResponseBody string
}

type ListResponse struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

type StatsResponse struct {
	Object         string                `json:"object"`
	Models         StatsModelsSection    `json:"models"`
	RequestHistory RequestHistorySection `json:"request_history"`
}

type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelStat struct {
	ID          string `json:"id"`
	ServerCount int    `json:"server_count"`
}

type StatsModelsSection struct {
	Object string      `json:"object"`
	Data   []ModelStat `json:"data"`
}

type RequestHistorySection struct {
	Last24Hours RequestHistoryWindow `json:"last_24_hours"`
	Last7Days   RequestHistoryWindow `json:"last_7_days"`
	Last28Days  RequestHistoryWindow `json:"last_28_days"`
}

type RequestHistoryWindow struct {
	RequestCount     int64 `json:"request_count"`
	AttemptCount     int64 `json:"attempt_count"`
	SuccessCount     int64 `json:"success_count"`
	FailureCount     int64 `json:"failure_count"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

func NewService(repo Repository, owner string, mapper ModelMapper) *Service {
	if mapper == nil {
		mapper = StaticModelMapper{}
	}

	return &Service{
		mapper: mapper,
		repo:   repo,
		owner:  owner,
	}
}

func (s *Service) WithDebug(enabled bool) *Service {
	s.debug = enabled
	return s
}

func (s *Service) ListModels(ctx context.Context) (ListResponse, error) {
	s.debugf("listing models")
	records, err := s.repo.ListCloudModels(ctx)
	if err != nil {
		s.debugf("list models failed: %v", err)
		return ListResponse{}, err
	}
	s.debugf("loaded %d model records", len(records))

	return BuildListResponse(records, s.owner, s.mapper), nil
}

func (s *Service) ListStats(ctx context.Context) (StatsResponse, error) {
	s.debugf("listing model stats")
	now := time.Now().UTC()
	modelRecords, err := s.repo.ListCloudModelStats(ctx)
	if err != nil {
		s.debugf("list stats failed: %v", err)
		return StatsResponse{}, err
	}
	s.debugf("loaded %d model stats records", len(modelRecords))

	historySince := now.Add(-28 * 24 * time.Hour)
	historyRecords, err := s.repo.ListRequestHistory(ctx, historySince)
	if err != nil {
		s.debugf("list request history failed: %v", err)
		return StatsResponse{}, err
	}
	s.debugf("loaded %d request history records", len(historyRecords))

	return BuildStatsResponse(modelRecords, historyRecords, s.mapper, now), nil
}

func (s *Service) debugf(format string, args ...any) {
	if !s.debug {
		return
	}

	log.Printf("DEBUG models: "+format, args...)
}

func BuildListResponse(records []Record, owner string, mapper ModelMapper) ListResponse {
	if mapper == nil {
		mapper = StaticModelMapper{}
	}

	data := make([]ModelObject, 0, len(records))

	for _, record := range records {
		if record.ID == "" {
			continue
		}

		modelID := mapper.Resolve(record.ID)
		if modelID == "" {
			continue
		}

		data = append(data, ModelObject{
			ID:      modelID,
			Object:  "model",
			Created: record.CreatedAt.Unix(),
			OwnedBy: owner,
		})
	}

	return ListResponse{
		Object: "list",
		Data:   data,
	}
}

func BuildStatsResponse(records []StatRecord, historyRecords []RequestHistoryRecord, mapper ModelMapper, now time.Time) StatsResponse {
	if mapper == nil {
		mapper = StaticModelMapper{}
	}

	data := make([]ModelStat, 0, len(records))

	for _, record := range records {
		if record.ID == "" {
			continue
		}

		modelID := mapper.Resolve(record.ID)
		if modelID == "" {
			continue
		}

		data = append(data, ModelStat{
			ID:          modelID,
			ServerCount: record.ServerCount,
		})
	}

	return StatsResponse{
		Object: "stats",
		Models: StatsModelsSection{
			Object: "list",
			Data:   data,
		},
		RequestHistory: BuildRequestHistory(historyRecords, now),
	}
}
