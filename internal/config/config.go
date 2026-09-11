// Package config loads runtime settings from environment variables.
//
// The platform runs as three Go binaries (receiver, worker, api) that share
// one Config. Not every field is used by every binary, so validation is
// intentionally light and driven by the OFFLINE flag: in offline/demo mode we
// do not require a GitHub token or webhook secret, so the whole system runs
// locally with no external accounts.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime settings for the platform.
type Config struct {
	// Receiver
	Port          string // HTTP port for the webhook receiver (default 8080).
	WebhookSecret string // Shared secret used to verify GitHub webhook signatures.

	// Read API (dashboard backend)
	APIPort string // HTTP port for the read API (default 8081).

	// GitHub
	GitHubToken string // Token used to call the GitHub API (fetch diff/files, post review).

	// Infrastructure
	RedisAddr   string // Redis address for the Asynq queue (default 127.0.0.1:6379).
	DatabaseURL string // PostgreSQL DSN (pgvector-enabled).
	AnalyzerURL string // Base URL of the Python analyzer service.

	// Behaviour
	Offline       bool    // If true, use the fixture GitHub provider and skip secret/token checks.
	MaxDiffBytes  int     // Cap on diff size sent to the analyzer (keeps us within token limits).
	MinConfidence float64 // Only post comments at or above this confidence (0..1).
}

// Load reads config from the environment and validates required fields.
func Load() (Config, error) {
	cfg := Config{
		Port:          getEnv("PORT", "8080"),
		WebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		APIPort:       getEnv("API_PORT", "8081"),
		GitHubToken:   os.Getenv("GITHUB_TOKEN"),
		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://acr:acr@127.0.0.1:5432/acr?sslmode=disable"),
		AnalyzerURL:   getEnv("ANALYZER_URL", "http://127.0.0.1:9000"),
		Offline:       getEnvBool("OFFLINE", false),
		MaxDiffBytes:  getEnvInt("MAX_DIFF_BYTES", 60000),
		MinConfidence: getEnvFloat("MIN_CONFIDENCE", 0.5),
	}

	// In online mode we talk to real GitHub, so these are mandatory. In
	// offline/demo mode the fixture provider stands in for GitHub and no
	// secret is needed.
	if !cfg.Offline {
		if cfg.WebhookSecret == "" {
			return cfg, fmt.Errorf("GITHUB_WEBHOOK_SECRET is required (or set OFFLINE=true)")
		}
		if cfg.GitHubToken == "" {
			return cfg, fmt.Errorf("GITHUB_TOKEN is required (or set OFFLINE=true)")
		}
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
