package models

import (
	"context"
	"strings"
	"time"
)

const CloudTag = ":cloud"

type Repository interface {
	ListCloudModels(ctx context.Context) ([]Record, error)
}

type Service struct {
	mapper ModelMapper
	repo   Repository
	owner  string
}

type Record struct {
	ID        string
	CreatedAt time.Time
}

type ListResponse struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
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

func (s *Service) ListModels(ctx context.Context) (ListResponse, error) {
	records, err := s.repo.ListCloudModels(ctx)
	if err != nil {
		return ListResponse{}, err
	}

	return BuildListResponse(records, s.owner, s.mapper), nil
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
