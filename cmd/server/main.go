// Command server runs the guaperrimo.ai stylist backend.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"stylerag/internal/agent"
	"stylerag/internal/api"
	"stylerag/internal/config"
	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/storage"
	"stylerag/internal/telemetry"
	"stylerag/internal/tryon"
	"stylerag/internal/vision"
)

func main() {
	cfg := config.Load()

	otelShutdown, err := telemetry.Init(context.Background(), "guaperrimo", "0.2.0")
	if err != nil {
		slog.Error("telemetry init failed", "error", err)
		os.Exit(1)
	}
	defer otelShutdown(context.Background())

	slog.SetDefault(slog.New(telemetry.NewTracedHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))))
	ctx := context.Background()

	if err := cfg.Validate(); err != nil {
		slog.ErrorContext(ctx, "invalid configuration", "error", err)
		os.Exit(1)
	}

	r2, err := storage.NewR2Store(ctx, cfg)
	if err != nil {
		slog.ErrorContext(ctx, "r2 init failed", "error", err)
		os.Exit(1)
	}
	// Images are served through this API with signed links: the bucket stays
	// private and the app loads them with a plain GET.
	publicBase := cfg.PublicBaseURL
	if publicBase == "" {
		publicBase = config.DefaultPublicBaseURL(cfg.Port)
	}
	signingSecret := cfg.APIKey
	if signingSecret == "" {
		signingSecret = cfg.R2AccessKeySecret
	}
	images := storage.WithSignedURLs(r2, publicBase, signingSecret, cfg.ImageURLTTL)
	slog.InfoContext(ctx, "images served via API", "public_base_url", publicBase, "link_ttl", cfg.ImageURLTTL)

	// LLM: chat+tools and vision may be different models of the same provider.
	flavor := llm.Flavor(cfg.LLMProvider)
	if cfg.LLMProvider == "kimi" {
		flavor = llm.FlavorMoonshot
	}
	chatLLM := llm.NewOpenAICompat(llm.OpenAICompatConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, Flavor: flavor, MaxRetries: 2})
	visionLLM := llm.NewOpenAICompat(llm.OpenAICompatConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.VisionModel, Flavor: flavor, MaxRetries: 1})
	slog.InfoContext(ctx, "llm configured", "provider", cfg.LLMProvider, "chat_model", cfg.LLMModel, "vision_model", cfg.VisionModel)

	// Shopping provider: SerpAPI when a key exists, otherwise deterministic fake data.
	var shop shopping.Provider
	if cfg.SerpAPIKey != "" && cfg.ShoppingProvider != "fake" {
		shop = shopping.NewCache(shopping.NewSerpAPI(cfg.SerpAPIKey, cfg.SearchLocation), 15*time.Minute)
	} else {
		shop = shopping.Fake{}
		slog.WarnContext(ctx, "SERPAPI_KEY not set: using FAKE products and stores")
	}
	slog.InfoContext(ctx, "shopping configured", "provider", shop.Name(), "location", cfg.SearchLocation, "radius_m", cfg.DefaultRadiusM)

	// Session store.
	var store session.Store
	if cfg.PostgresURL != "" {
		pg, err := session.NewPostgresStore(ctx, cfg.PostgresURL)
		if err != nil {
			slog.ErrorContext(ctx, "postgres init failed", "error", err)
			os.Exit(1)
		}
		defer pg.Close()
		store = pg
		slog.InfoContext(ctx, "sessions: postgres")
	} else {
		mem := session.NewMemoryStore(cfg.SessionTTL)
		defer mem.Stop()
		store = mem
		slog.WarnContext(ctx, "sessions: in-memory (lost on restart)", "ttl", cfg.SessionTTL)
	}

	runner := agent.NewRunner(chatLLM, agent.Deps{
		Shopping:           shop,
		DefaultRadiusM:     cfg.DefaultRadiusM,
		DefaultLocation:    cfg.SearchLocation,
		MaxSearchesPerTurn: cfg.AgentMaxSearches,
	}, cfg.AgentMaxSteps, cfg.AgentMaxQuestions)

	chatDeps := &api.ChatDeps{
		Store:            store,
		Images:           images,
		Analyzer:         vision.NewLLMAnalyzer(visionLLM),
		Runner:           runner,
		MatrixMaxPerSlot: cfg.MatrixMaxPerSlot,
		TurnTimeout:      cfg.AgentTurnTimeout,
	}
	matrixDeps := &api.MatrixDeps{Store: store}

	// Virtual try-on is optional: it needs a GCP project and working Google credentials.
	var tryonDeps *api.TryOnDeps
	if cfg.GCPProjectID == "" {
		slog.WarnContext(ctx, "vton disabled (GCP_PROJECT_ID not set)")
	} else {
		if err := cfg.SetupGCPCredentials(); err != nil {
			slog.ErrorContext(ctx, "gcp credentials", "error", err)
		}
		if err := tryon.CheckCredentials(ctx); err != nil {
			slog.WarnContext(ctx, "vton disabled: Google credentials not available; run `gcloud auth application-default login` or set GCP_SA_KEY_JSON", "error", err)
		} else {
			vton := &tryon.GoogleVertexVTON{
				ProjectID:  cfg.GCPProjectID,
				Region:     cfg.GCPRegion,
				BaseSteps:  cfg.VTONBaseSteps,
				HTTPClient: &http.Client{Timeout: cfg.VTONTimeout},
			}
			httpClient := &http.Client{Timeout: 15 * time.Second}
			var details shopping.DetailResolver
			if dr, ok := shop.(shopping.DetailResolver); ok && cfg.ResolveProductDetails {
				details = dr
			}
			renderer := tryon.NewRenderer(store, images, vton, details, httpClient, cfg.RenderConcurrency, cfg.PrefetchConcurrency, cfg.VTONTimeout)
			tryonDeps = &api.TryOnDeps{Store: store, Images: images, VTON: vton, Timeout: cfg.VTONTimeout, HTTPClient: httpClient}
			chatDeps.Renderer = renderer
			chatDeps.Looks = &tryon.LookGenerator{Store: store, Renderer: renderer}
			matrixDeps.Renderer = renderer
			slog.InfoContext(ctx, "vton enabled", "region", cfg.GCPRegion, "render_concurrency", cfg.RenderConcurrency, "product_details", details != nil)
		}
	}

	router := api.NewRouter(api.Deps{APIKey: cfg.APIKey, Images: images, Store: store, Chat: chatDeps, TryOn: tryonDeps, Matrix: matrixDeps, Signed: images})

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: cfg.AgentTurnTimeout + 30*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.InfoContext(ctx, "server listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(ctx, "server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.InfoContext(ctx, "shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.ErrorContext(shutdownCtx, "shutdown error", "error", err)
	}
}
