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
	"stylerag/internal/rag"
	"stylerag/internal/session"
	"stylerag/internal/storage"
	"stylerag/internal/telemetry"
	"stylerag/internal/tryon"
	"stylerag/internal/vision"
)

func main() {
	cfg := config.Load()

	// Initialize OpenTelemetry tracing
	otelShutdown, err := telemetry.Init(context.Background(), "stylerag", "0.1.0")
	if err != nil {
		slog.Error("failed to initialize telemetry", "error", err)
		os.Exit(1)
	}
	defer otelShutdown(context.Background())

	logger := slog.New(telemetry.NewTracedHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})))
	slog.SetDefault(logger)

	initCtx := context.Background()

	imageStore, err := storage.NewR2Store(initCtx, cfg)
	if err != nil {
		slog.ErrorContext(initCtx, "failed to initialize R2 storage", "error", err)
		os.Exit(1)
	}

	// PostgreSQL connection pool + repositories
	var (
		catalogRepo  *catalog.PostgresRepository
		retailerRepo *database.RetailerRepo
		sessionRepo  *database.SessionRepo
	)
	if cfg.PostgresURL != "" {
		dbPool, err := database.NewPostgresPool(initCtx, cfg.PostgresURL)
		if err != nil {
			slog.ErrorContext(initCtx, "failed to connect to PostgreSQL", "error", err)
			os.Exit(1)
		}
		defer dbPool.Close()
		slog.InfoContext(initCtx, "connected to PostgreSQL")

		catalogRepo = catalog.NewPostgresRepository(dbPool)
		retailerRepo = database.NewRetailerRepo(dbPool)
		sessionRepo = database.NewSessionRepo(dbPool)
	} else {
		slog.WarnContext(initCtx, "POSTGRES_URL not set, running without database")
	}

	// LLM providers and router
	var potentProvider, economyProvider llm.Provider
	switch cfg.LLMProvider {
	case "mistral":
		slog.InfoContext(initCtx, "using Mistral LLM provider", "potent", cfg.MistralPotentModel, "economy", cfg.MistralEconomyModel, "api_key_len", len(cfg.MistralAPIKey), "api_key_prefix", cfg.MistralAPIKey[:min(4, len(cfg.MistralAPIKey))])
		potentProvider = llm.NewMistralProvider(cfg.MistralAPIKey, cfg.MistralPotentModel)
		economyProvider = llm.NewMistralProvider(cfg.MistralAPIKey, cfg.MistralEconomyModel)
	default: // "kimi"
		slog.InfoContext(initCtx, "using Kimi LLM provider", "potent", cfg.KimiPotentModel, "economy", cfg.KimiEconomyModel)
		potentProvider = llm.NewKimiProvider(cfg.MoonshotAPIKey, cfg.KimiPotentModel)
		economyProvider = llm.NewKimiProvider(cfg.MoonshotAPIKey, cfg.KimiEconomyModel)
	}
	llmRouter := llm.NewRouter(potentProvider, economyProvider)

	// Vision: analyzer (potent) + style advisor (economy)
	analyzer := vision.NewLLMAnalyzer(llmRouter)
	advisor := vision.NewStyleAdvisor(llmRouter)

	// RAG engine (optional — degrades gracefully if not configured)
	var ragEngine rag.Engine
	if cfg.QdrantURL != "" && cfg.OpenAIAPIKey != "" && catalogRepo != nil {
		embedder := rag.NewOpenAIEmbedder(cfg.OpenAIAPIKey, cfg.EmbeddingModel)
		ragEngine = rag.NewQdrantEngine(cfg.QdrantURL, cfg.QdrantCollection, embedder, catalogRepo)
		slog.InfoContext(initCtx, "RAG engine initialized", "qdrant", cfg.QdrantURL, "model", cfg.EmbeddingModel)
	} else {
		slog.WarnContext(initCtx, "RAG engine disabled",
			"qdrant_url_set", cfg.QdrantURL != "",
			"openai_key_set", cfg.OpenAIAPIKey != "",
			"catalog_repo_set", catalogRepo != nil,
		)
	}

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
		RAGEngine:  ragEngine,
	}

	_ = retailerRepo // will be used for auth middleware
	_ = sessionRepo  // will be used for analytics tracking

	// Virtual Try-On (optional — degrades gracefully)
	if err := cfg.SetupGCPCredentials(); err != nil {
		slog.ErrorContext(initCtx, "failed to setup GCP credentials", "error", err)
	}
	var tryonDeps *api.TryOnDeps
	if cfg.GCPProjectID != "" {
		var vtonProvider tryon.VTONProvider
		switch cfg.VTONProvider {
		case "fashn":
			vtonProvider = &tryon.FashnVTON{
				APIKey:     cfg.FashnAPIKey,
				BaseURL:    "https://api.fashn.ai/v1",
				Mode:       cfg.FashnMode,
				HTTPClient: &http.Client{Timeout: cfg.VTONTimeout},
			}
		default:
			vtonProvider = &tryon.GoogleVertexVTON{
				ProjectID:  cfg.GCPProjectID,
				Region:     cfg.GCPRegion,
				BaseSteps:  cfg.VTONBaseSteps,
				HTTPClient: &http.Client{Timeout: cfg.VTONTimeout},
			}
		}
		tryonDeps = &api.TryOnDeps{
			Store:        sessionStore,
			ImageStore:   imageStore,
			RAGEngine:    ragEngine,
			VTONProvider: vtonProvider,
		}
		slog.InfoContext(initCtx, "VTON enabled", "provider", cfg.VTONProvider)

		// Look generation pipeline (requires VTON + RAG)
		if ragEngine != nil {
			lookComposer := tryon.NewLookComposer(llmRouter, cfg.LookCount)
			lookGenerator := &tryon.LookGenerator{
				Store:        sessionStore,
				ImageStore:   imageStore,
				RAGEngine:    ragEngine,
				VTONProvider: vtonProvider,
				Timeout:      cfg.LookGenerationTimeout,
			}
			chatDeps.LookComposer = lookComposer
			chatDeps.LookGenerator = lookGenerator
			slog.InfoContext(initCtx, "look generation enabled", "look_count", cfg.LookCount, "timeout", cfg.LookGenerationTimeout)
		} else {
			slog.WarnContext(initCtx, "look generation disabled (RAG not available)")
		}
	} else {
		slog.WarnContext(initCtx, "VTON disabled (GCP_PROJECT_ID not set)")
	}

	// Avoid typed-nil interface: a (*PostgresRepository)(nil) is not a nil catalog.Repository.
	var catRepo catalog.Repository
	if catalogRepo != nil {
		catRepo = catalogRepo
	}

	router := api.NewRouter(cfg, imageStore, analyzer, advisor, chatDeps, catRepo, tryonDeps)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.InfoContext(initCtx, "starting server", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(initCtx, "server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.InfoContext(initCtx, "shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.ErrorContext(shutdownCtx, "server shutdown error", "error", err)
	}
}
