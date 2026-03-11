package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"stylerag/internal/api"
	"stylerag/internal/catalog"
	"stylerag/internal/config"
	"stylerag/internal/database"
	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/storage"
	"stylerag/internal/vision"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	imageStore, err := storage.NewR2Store(context.Background(), cfg)
	if err != nil {
		slog.Error("failed to initialize R2 storage", "error", err)
		os.Exit(1)
	}

	// PostgreSQL connection pool + repositories
	var (
		catalogRepo  *catalog.PostgresRepository
		retailerRepo *database.RetailerRepo
		sessionRepo  *database.SessionRepo
	)
	if cfg.PostgresURL != "" {
		dbPool, err := database.NewPostgresPool(context.Background(), cfg.PostgresURL)
		if err != nil {
			slog.Error("failed to connect to PostgreSQL", "error", err)
			os.Exit(1)
		}
		defer dbPool.Close()
		slog.Info("connected to PostgreSQL")

		catalogRepo = catalog.NewPostgresRepository(dbPool)
		retailerRepo = database.NewRetailerRepo(dbPool)
		sessionRepo = database.NewSessionRepo(dbPool)
	} else {
		slog.Warn("POSTGRES_URL not set, running without database")
	}

	// LLM providers and router
	var potentProvider, economyProvider llm.Provider
	switch cfg.LLMProvider {
	case "mistral":
		slog.Info("using Mistral LLM provider", "potent", cfg.MistralPotentModel, "economy", cfg.MistralEconomyModel, "api_key_len", len(cfg.MistralAPIKey), "api_key_prefix", cfg.MistralAPIKey[:min(4, len(cfg.MistralAPIKey))])
		potentProvider = llm.NewMistralProvider(cfg.MistralAPIKey, cfg.MistralPotentModel)
		economyProvider = llm.NewMistralProvider(cfg.MistralAPIKey, cfg.MistralEconomyModel)
	default: // "kimi"
		slog.Info("using Kimi LLM provider", "potent", cfg.KimiPotentModel, "economy", cfg.KimiEconomyModel)
		potentProvider = llm.NewKimiProvider(cfg.MoonshotAPIKey, cfg.KimiPotentModel)
		economyProvider = llm.NewKimiProvider(cfg.MoonshotAPIKey, cfg.KimiEconomyModel)
	}
	llmRouter := llm.NewRouter(potentProvider, economyProvider)

	// Vision: analyzer (potent) + style advisor (economy)
	analyzer := vision.NewLLMAnalyzer(llmRouter)
	advisor := vision.NewStyleAdvisor(llmRouter)

	// Conversational advisor dependencies
	sessionStore := session.NewInMemoryStore(30 * time.Minute)
	discovery := vision.NewDiscoveryManager(llmRouter)
	diagnosis := vision.NewDiagnosisGenerator(llmRouter)
	chatDeps := &api.ChatDeps{
		Store:      sessionStore,
		ImageStore: imageStore,
		Analyzer:   analyzer,
		Discovery:  discovery,
		Diagnosis:  diagnosis,
		Advisor:    advisor,
	}

	_ = retailerRepo // will be used for auth middleware
	_ = sessionRepo  // will be used for analytics tracking

	// Avoid typed-nil interface: a (*PostgresRepository)(nil) is not a nil catalog.Repository.
	var catRepo catalog.Repository
	if catalogRepo != nil {
		catRepo = catalogRepo
	}

	router := api.NewRouter(cfg, imageStore, analyzer, advisor, chatDeps, catRepo)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("starting server", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
}
