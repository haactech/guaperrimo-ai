// Package config reads runtime settings from the environment (and .env).
package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full runtime configuration.
type Config struct {
	Port          string
	LogLevel      slog.Level
	APIKey        string        // optional shared secret for the iOS app
	PublicBaseURL string        // how clients reach this server, e.g. http://192.168.100.39:8080
	ImageURLTTL   time.Duration // validity of signed image links

	// LLM: one OpenAI-compatible provider for chat+tools and one model for vision.
	LLMProvider string // mistral | moonshot | openai
	LLMAPIKey   string
	LLMBaseURL  string
	LLMModel    string
	VisionModel string

	// Shopping (products + nearby stores)
	ShoppingProvider string // serpapi | fake
	SerpAPIKey       string
	SearchLocation   string // free-text origin for Google Shopping, e.g. "Mexico"
	DefaultRadiusM   int

	// Sessions
	PostgresURL string        // empty = in-memory
	SessionTTL  time.Duration // in-memory only

	// Storage (Cloudflare R2)
	R2AccountID       string
	R2AccessKeyID     string
	R2AccessKeySecret string
	R2BucketName      string
	R2PublicURL       string

	// Virtual try-on (Google Vertex); disabled when GCPProjectID is empty
	GCPProjectID          string
	GCPSAKeyJSON          string
	GCPRegion             string
	VTONBaseSteps         int
	VTONTimeout           time.Duration
	LookGenerationTimeout time.Duration
	RenderConcurrency     int
	PrefetchConcurrency   int
	MatrixMaxPerSlot      int
	ResolveProductDetails bool

	// Agent
	AgentMaxSteps     int
	AgentMaxQuestions int
	AgentMaxSearches  int
	AgentTurnTimeout  time.Duration
}

// Load reads the environment, falling back to a .env file in the working directory.
func Load() *Config {
	loadDotenv(".env")

	provider := getEnv("LLM_PROVIDER", "mistral")
	baseURL, model, apiKey := providerDefaults(provider)

	return &Config{
		Port:          getEnv("PORT", "8080"),
		LogLevel:      parseLogLevel(getEnv("LOG_LEVEL", "info")),
		APIKey:        getEnv("API_KEY", ""),
		PublicBaseURL: strings.TrimRight(getEnv("PUBLIC_BASE_URL", ""), "/"),
		ImageURLTTL:   parseDuration(getEnv("IMAGE_URL_TTL", "720h"), 30*24*time.Hour),

		LLMProvider: provider,
		LLMAPIKey:   getEnv("LLM_API_KEY", apiKey),
		LLMBaseURL:  getEnv("LLM_BASE_URL", baseURL),
		LLMModel:    getEnv("LLM_MODEL", model),
		VisionModel: getEnv("VISION_MODEL", getEnv("LLM_MODEL", model)),

		ShoppingProvider: getEnv("SHOPPING_PROVIDER", ""),
		SerpAPIKey:       getEnv("SERPAPI_KEY", ""),
		SearchLocation:   getEnv("SEARCH_LOCATION", "Mexico"),
		DefaultRadiusM:   getEnvInt("DEFAULT_RADIUS_M", 1500),

		PostgresURL: getEnv("POSTGRES_URL", ""),
		SessionTTL:  parseDuration(getEnv("SESSION_TTL", "24h"), 24*time.Hour),

		R2AccountID:       getEnv("R2_ACCOUNT_ID", ""),
		R2AccessKeyID:     getEnv("R2_ACCESS_KEY_ID", ""),
		R2AccessKeySecret: getEnv("R2_ACCESS_KEY_SECRET", ""),
		R2BucketName:      getEnv("R2_BUCKET_NAME", ""),
		R2PublicURL:       strings.TrimRight(getEnv("R2_PUBLIC_URL", ""), "/"),

		GCPProjectID:          getEnv("GCP_PROJECT_ID", ""),
		GCPSAKeyJSON:          getEnv("GCP_SA_KEY_JSON", ""),
		GCPRegion:             getEnv("GCP_REGION", "us-central1"),
		VTONBaseSteps:         getEnvInt("VTON_BASE_STEPS", 20),
		VTONTimeout:           parseDuration(getEnv("VTON_TIMEOUT", "60s"), 60*time.Second),
		LookGenerationTimeout: parseDuration(getEnv("LOOK_GENERATION_TIMEOUT", "180s"), 180*time.Second),
		RenderConcurrency:     getEnvInt("RENDER_CONCURRENCY", 3),
		PrefetchConcurrency:   getEnvInt("RENDER_PREFETCH_CONCURRENCY", 2),
		MatrixMaxPerSlot:      getEnvInt("MATRIX_MAX_PER_SLOT", 3),
		ResolveProductDetails: getEnv("RESOLVE_PRODUCT_DETAILS", "true") != "false",

		AgentMaxSteps:     getEnvInt("AGENT_MAX_STEPS", 12),
		AgentMaxQuestions: getEnvInt("AGENT_MAX_QUESTIONS", 6),
		AgentMaxSearches:  getEnvInt("AGENT_MAX_SEARCHES", 8),
		AgentTurnTimeout:  parseDuration(getEnv("AGENT_TURN_TIMEOUT", "150s"), 150*time.Second),
	}
}

// providerDefaults returns base URL, default model and the legacy key env for a provider.
func providerDefaults(provider string) (baseURL, model, apiKey string) {
	switch provider {
	case "moonshot", "kimi":
		return "https://api.moonshot.ai/v1", "kimi-k2.5", os.Getenv("MOONSHOT_API_KEY")
	case "openai":
		return "https://api.openai.com/v1", "", os.Getenv("OPENAI_API_KEY")
	default:
		return "https://api.mistral.ai/v1", "mistral-large-latest", os.Getenv("MISTRAL_API_KEY")
	}
}

// Validate reports configuration that would make the server useless.
func (c *Config) Validate() error {
	var missing []string
	if c.LLMAPIKey == "" {
		missing = append(missing, "LLM_API_KEY (or MISTRAL_API_KEY / MOONSHOT_API_KEY / OPENAI_API_KEY)")
	}
	if c.LLMModel == "" {
		missing = append(missing, "LLM_MODEL")
	}
	for _, kv := range []struct{ k, v string }{
		{"R2_ACCOUNT_ID", c.R2AccountID}, {"R2_ACCESS_KEY_ID", c.R2AccessKeyID},
		{"R2_ACCESS_KEY_SECRET", c.R2AccessKeySecret}, {"R2_BUCKET_NAME", c.R2BucketName}, {"R2_PUBLIC_URL", c.R2PublicURL},
	} {
		if kv.v == "" {
			missing = append(missing, kv.k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing configuration: %s", strings.Join(missing, ", "))
	}
	return nil
}

// SetupGCPCredentials writes the service account JSON to a temp file and points
// GOOGLE_APPLICATION_CREDENTIALS at it. No-op when unset (local ADC is used).
func (c *Config) SetupGCPCredentials() error {
	if c.GCPSAKeyJSON == "" || os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		return nil
	}
	f, err := os.CreateTemp("", "gcp-sa-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file for GCP credentials: %w", err)
	}
	if _, err := f.WriteString(c.GCPSAKeyJSON); err != nil {
		f.Close()
		return fmt.Errorf("writing GCP credentials: %w", err)
	}
	f.Close()
	return os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", f.Name())
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
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

func parseDuration(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

// loadDotenv sets variables from a .env file that are not already set.
// Supports "KEY=VAL" and "export KEY=VAL".
func loadDotenv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), "\"'")
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

// DefaultPublicBaseURL guesses how devices on the LAN reach this server when
// PUBLIC_BASE_URL is not set: the first non-loopback IPv4 address plus the port.
func DefaultPublicBaseURL(port string) string {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, ifc := range ifaces {
			if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := ifc.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				ipn, ok := a.(*net.IPNet)
				if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
					continue
				}
				return "http://" + ipn.IP.String() + ":" + port
			}
		}
	}
	return "http://localhost:" + port
}
