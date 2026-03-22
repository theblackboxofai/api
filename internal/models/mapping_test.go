package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModelMapper(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "maps.yml")
	content := []byte("models:\n  \"deepseek-v3.2:cloud\": \"deepseek/deepseek-v3.2\"\n")

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write test map: %v", err)
	}

	mapper, err := LoadModelMapper(path)
	if err != nil {
		t.Fatalf("load mapper: %v", err)
	}

	if got := mapper.Resolve("deepseek-v3.2:cloud"); got != "deepseek/deepseek-v3.2" {
		t.Fatalf("expected mapped id, got %q", got)
	}

	if got := mapper.Resolve("unknown:cloud"); got != "unknown:cloud" {
		t.Fatalf("expected raw fallback, got %q", got)
	}

	rawID, ok := mapper.LookupRaw("deepseek/deepseek-v3.2")
	if !ok {
		t.Fatal("expected reverse lookup to succeed")
	}

	if rawID != "deepseek-v3.2:cloud" {
		t.Fatalf("expected reverse lookup to return raw id, got %q", rawID)
	}
}

func TestLoadModelMapperExplicitEmptyMappingDisablesModel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "maps.yml")
	content := []byte("models:\n  \"alpha:cloud\": \"\"\n")

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write test map: %v", err)
	}

	mapper, err := LoadModelMapper(path)
	if err != nil {
		t.Fatalf("load mapper: %v", err)
	}

	if got := mapper.Resolve("alpha:cloud"); got != "" {
		t.Fatalf("expected disabled model to resolve to empty id, got %q", got)
	}

	if _, ok := mapper.LookupRaw("alpha:cloud"); ok {
		t.Fatal("expected disabled raw model to be unavailable")
	}
}

func TestLoadModelMapperMissingFileFallsBackToRawIDs(t *testing.T) {
	t.Parallel()

	mapper, err := LoadModelMapper(filepath.Join(t.TempDir(), "missing.yml"))
	if err != nil {
		t.Fatalf("load mapper: %v", err)
	}

	if got := mapper.Resolve("alpha:cloud"); got != "alpha:cloud" {
		t.Fatalf("expected raw fallback for missing file, got %q", got)
	}

	rawID, ok := mapper.LookupRaw("alpha:cloud")
	if !ok {
		t.Fatal("expected raw cloud id to resolve without map file")
	}

	if rawID != "alpha:cloud" {
		t.Fatalf("expected raw cloud id passthrough, got %q", rawID)
	}
}
