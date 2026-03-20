package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	repo := models.NewPostgresRepository(db)
	mapper, err := models.LoadModelMapper(cfg.ModelMapPath)
	if err != nil {
		log.Fatalf("load model mapper: %v", err)
	}

	modelService := models.NewService(repo, cfg.ModelsOwner, mapper)
	chatService := chat.NewService(chat.NewPostgresRepository(db), mapper)

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
	ModelMapPath string
	ModelsOwner string
	Port        string
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

	return config{
		DatabaseURL: databaseURL,
		ModelMapPath: filepath.Clean(modelMapPath),
		ModelsOwner: modelsOwner,
		Port:        port,
	}
}

func newRouter(modelService *models.Service, chatService *chat.Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/models", models.NewHandler(modelService))
	mux.HandleFunc("/v1/chat/completions", chatService.HandleCompletions)
	return mux
}
