package models

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type ModelMapper interface {
	Resolve(rawID string) string
	LookupRaw(modelID string) (string, bool)
}

type StaticModelMapper struct {
	models  map[string]string
	reverse map[string]string
}

type modelMapFile struct {
	Models map[string]string `yaml:"models"`
}

func LoadModelMapper(path string) (ModelMapper, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StaticModelMapper{}, nil
		}

		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file modelMapFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	reverse := make(map[string]string, len(file.Models))
	for rawID, mappedID := range file.Models {
		if mappedID == "" {
			continue
		}

		reverse[mappedID] = rawID
	}

	return StaticModelMapper{
		models:  file.Models,
		reverse: reverse,
	}, nil
}

func (m StaticModelMapper) Resolve(rawID string) string {
	if mapped, ok := m.models[rawID]; ok && mapped != "" {
		return mapped
	}

	return rawID
}

func (m StaticModelMapper) LookupRaw(modelID string) (string, bool) {
	if rawID, ok := m.reverse[modelID]; ok && rawID != "" {
		return rawID, true
	}

	if strings.Contains(modelID, CloudTag) {
		return modelID, true
	}

	return "", false
}
