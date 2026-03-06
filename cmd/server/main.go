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
	"stylerag/internal/config"
	"stylerag/internal/llm"
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

	router := api.NewRouter(cfg, imageStore, analyzer, advisor)

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
