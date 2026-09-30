// Package config provides application configuration for the Beacon agent.
// Configuration is loaded from environment variables with sensible defaults.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds the application configuration.
type Config struct {
	// LLM provider settings
	LLMProvider string // "ollama", "openai", "anthropic", "gemini"
	LLMModel    string // model name (e.g., "llama3", "gpt-4o", "claude-3-5-sonnet")
	LLMEndpoint string // API endpoint (required for Ollama, optional for others)
	LLMAPIKey   string // API key (not needed for Ollama)

	// AWS settings
	AWSRegion  string
	AWSProfile string // optional AWS profile name

	// Budget & payment
	MaxBudget float64 // maximum budget in USD per workload

	// State store
	DBPath string // path to SQLite database file

	// Logging
	LogLevel  string // "debug", "info", "warn", "error"
	LogFormat string // "json", "console"

	// Safety limits
	MaxProvisionRetries int     // max retries for provisioning API calls
	MaxDiscoveryRetries int     // max retries for discovery API calls
	RateLimitPerSecond  float64 // rate limit for cloud API calls
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		LLMProvider:         "ollama",
		LLMModel:            "llama3",
		LLMEndpoint:         "http://localhost:11434",
		AWSRegion:           "us-east-1",
		MaxBudget:           100.0,
		DBPath:              "beacon.db",
		LogLevel:            "info",
		LogFormat:           "console",
		MaxProvisionRetries: 3,
		MaxDiscoveryRetries: 5,
		RateLimitPerSecond:  10.0,
	}
}

// LoadFromEnv loads configuration from environment variables, using defaults
// for any values not set.
func LoadFromEnv() *Config {
	cfg := DefaultConfig()

	if v := os.Getenv("BEACON_LLM_PROVIDER"); v != "" {
		cfg.LLMProvider = v
	}
	if v := os.Getenv("BEACON_LLM_MODEL"); v != "" {
		cfg.LLMModel = v
	}
	if v := os.Getenv("BEACON_LLM_ENDPOINT"); v != "" {
		cfg.LLMEndpoint = v
	}
	if v := os.Getenv("BEACON_LLM_API_KEY"); v != "" {
		cfg.LLMAPIKey = v
	}
	if v := os.Getenv("BEACON_AWS_REGION"); v != "" {
		cfg.AWSRegion = v
	}
	if v := os.Getenv("BEACON_AWS_PROFILE"); v != "" {
		cfg.AWSProfile = v
	}
	if v := os.Getenv("BEACON_MAX_BUDGET"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.MaxBudget = f
		}
	}
	if v := os.Getenv("BEACON_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("BEACON_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("BEACON_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("BEACON_MAX_PROVISION_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxProvisionRetries = n
		}
	}
	if v := os.Getenv("BEACON_MAX_DISCOVERY_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxDiscoveryRetries = n
		}
	}
	if v := os.Getenv("BEACON_RATE_LIMIT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.RateLimitPerSecond = f
		}
	}

	return cfg
}

// Validate checks the configuration for required fields and returns an error
// if any required configuration is missing.
func (c *Config) Validate() error {
	switch c.LLMProvider {
	case "ollama":
		if c.LLMEndpoint == "" {
			return fmt.Errorf("BEACON_LLM_ENDPOINT is required for Ollama provider")
		}
	case "openai", "anthropic", "gemini":
		if c.LLMAPIKey == "" {
			return fmt.Errorf("BEACON_LLM_API_KEY is required for %s provider", c.LLMProvider)
		}
	default:
		return fmt.Errorf("unsupported LLM provider: %s (supported: ollama, openai, anthropic, gemini)", c.LLMProvider)
	}

	if c.MaxBudget <= 0 {
		return fmt.Errorf("BEACON_MAX_BUDGET must be positive, got %f", c.MaxBudget)
	}

	return nil
}
