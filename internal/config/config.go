// Package config reads the process environment into a typed Config.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every setting the binary needs. Secrets are never logged.
type Config struct {
	Port              int
	DatabaseURL       string
	LiteLLMBaseURL    string
	LiteLLMAPIKey     string
	AppName           string
	DefaultModelAlias string
	AppUsername       string
	AppPassword       string
	LLMTimeout        time.Duration
	RunTimeout        time.Duration
	SessionSecret     string
}

// Load reads the environment. It returns an error for anything the server
// cannot start without.
func Load() (Config, error) {
	cfg := Config{
		Port:              envInt("PORT", 8080),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		LiteLLMBaseURL:    strings.TrimRight(os.Getenv("LITELLM_BASE_URL"), "/"),
		LiteLLMAPIKey:     os.Getenv("LITELLM_API_KEY"),
		AppName:           envStr("APP_NAME", "writersguild"),
		DefaultModelAlias: os.Getenv("DEFAULT_MODEL_ALIAS"),
		AppUsername:       strings.ToLower(strings.TrimSpace(os.Getenv("APP_USERNAME"))),
		AppPassword:       os.Getenv("APP_PASSWORD"),
		LLMTimeout:        time.Duration(envInt("LLM_TIMEOUT_SECONDS", 120)) * time.Second,
		RunTimeout:        time.Duration(envInt("RUN_TIMEOUT_SECONDS", 900)) * time.Second,
		SessionSecret:     os.Getenv("SESSION_SECRET"),
	}
	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if cfg.LiteLLMBaseURL == "" {
		errs = append(errs, errors.New("LITELLM_BASE_URL is required"))
	}
	if cfg.AppName == "" {
		errs = append(errs, errors.New("APP_NAME must not be empty"))
	}
	if len(errs) > 0 {
		return cfg, errors.Join(errs...)
	}
	return cfg, nil
}

// AliasPrefix is the prefix every per-writer gateway alias carries.
func (c Config) AliasPrefix() string { return c.AppName + "-" }

func envStr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s=%q is not an integer, using %d\n", key, v, def)
		return def
	}
	return n
}
