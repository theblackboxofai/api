package models

import (
	"context"
	"log"
	"strings"
	"time"
)

const CloudTag = ":cloud"

type Repository interface {
	ListCloudModels(ctx context.Context) ([]Record, error)
	ListCloudModelStats(ctx context.Context) ([]StatRecord, error)
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

type ListResponse struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

type StatsResponse struct {
	Object string      `json:"object"`
	Data   []ModelStat `json:"data"`
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
	records, err := s.repo.ListCloudModelStats(ctx)
	if err != nil {
		s.debugf("list stats failed: %v", err)
		return StatsResponse{}, err
	}
	s.debugf("loaded %d stats records", len(records))

	return BuildStatsResponse(records, s.mapper), nil
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
		if record.ID == "" || !strings.Contains(record.ID, CloudTag) {
			continue
		}

		data = append(data, ModelObject{
			ID:      mapper.Resolve(record.ID),
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

func BuildStatsResponse(records []StatRecord, mapper ModelMapper) StatsResponse {
	if mapper == nil {
		mapper = StaticModelMapper{}
	}

	data := make([]ModelStat, 0, len(records))

	for _, record := range records {
		if record.ID == "" || !strings.Contains(record.ID, CloudTag) {
			continue
		}

		data = append(data, ModelStat{
			ID:          mapper.Resolve(record.ID),
			ServerCount: record.ServerCount,
		})
	}

	return StatsResponse{
		Object: "list",
		Data:   data,
	}
}
