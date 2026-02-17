package config

import (
	"log/slog"
	"os"
)

type Config struct {
	Port     string
	LogLevel slog.Level

	// Database
	PostgresURL string
	RedisURL    string
	QdrantURL   string

	// LLM Providers
	AnthropicAPIKey string
	OpenAIAPIKey    string

	// Voice
	AvalonAPIKey string
}

func Load() *Config {
	return &Config{
		Port:     getEnv("PORT", "8080"),
		LogLevel: parseLogLevel(getEnv("LOG_LEVEL", "info")),

		PostgresURL: getEnv("POSTGRES_URL", ""),
		RedisURL:    getEnv("REDIS_URL", ""),
		QdrantURL:   getEnv("QDRANT_URL", ""),

		AnthropicAPIKey: getEnv("ANTHROPIC_API_KEY", ""),
		OpenAIAPIKey:    getEnv("OPENAI_API_KEY", ""),

		AvalonAPIKey: getEnv("AVALON_API_KEY", ""),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
