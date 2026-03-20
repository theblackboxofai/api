package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"blackbox-api/internal/chat"
	"blackbox-api/internal/models"
)

func main() {
	cfg := loadConfig()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	if err := applySchema(ctx, db, cfg.SchemaPath); err != nil {
		log.Fatalf("apply schema: %v", err)
	}

	repo := models.NewPostgresRepository(db)
	mapper, err := models.LoadModelMapper(cfg.ModelMapPath)
	if err != nil {
		log.Fatalf("load model mapper: %v", err)
	}

	modelService := models.NewService(repo, cfg.ModelsOwner, mapper)
	chatService := chat.NewService(chat.NewPostgresRepository(db), mapper)
	modelService.WithDebug(cfg.Debug)
	chatService.WithDebug(cfg.Debug)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(modelService, chatService),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

type config struct {
	DatabaseURL string
	Debug       bool
	ModelMapPath string
	ModelsOwner string
	Port        string
	SchemaPath  string
}

func loadConfig() config {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	modelsOwner := os.Getenv("MODELS_OWNED_BY")
	if modelsOwner == "" {
		modelsOwner = "blackbox"
	}

	modelMapPath := os.Getenv("MODEL_MAPS_FILE")
	if modelMapPath == "" {
		modelMapPath = "maps.yml"
	}

	schemaPath := os.Getenv("SCHEMA_FILE")
	if schemaPath == "" {
		schemaPath = "sql/schema.sql"
	}

	debug := parseBoolEnv(os.Getenv("DEBUG"))

	return config{
		DatabaseURL: databaseURL,
		Debug:       debug,
		ModelMapPath: filepath.Clean(modelMapPath),
		ModelsOwner: modelsOwner,
		Port:        port,
		SchemaPath:  filepath.Clean(schemaPath),
	}
}

func parseBoolEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func newRouter(modelService *models.Service, chatService *chat.Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/models", models.NewHandler(modelService))
	mux.Handle("/v1/stats", models.NewStatsHandler(modelService))
	mux.HandleFunc("/v1/chat/completions", chatService.HandleCompletions)
	return mux
}

func applySchema(ctx context.Context, db *sql.DB, schemaPath string) error {
	schemaSQL, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, string(schemaSQL))
	return err
}
