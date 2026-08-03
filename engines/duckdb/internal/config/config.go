package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr               string
	SystemURL          string
	MetaURL            string
	ClientSecret       string
	MaxRows            int
	MaxMemory          string
	Threads            int
	DefaultTimeout     time.Duration
	AllowedCallerIDs   map[string]struct{}
	SourceLoopbackHost string
}

func Load() (Config, error) {
	port := env("DUCKDB_RUNTIME_PORT", "8104")
	maxRows, err := positiveInt("DUCKDB_MAX_ROWS", 10000)
	if err != nil {
		return Config{}, err
	}
	threads, err := positiveInt("DUCKDB_THREADS", 4)
	if err != nil {
		return Config{}, err
	}
	timeout, err := time.ParseDuration(env("DUCKDB_QUERY_TIMEOUT", "60s"))
	if err != nil || timeout <= 0 {
		return Config{}, fmt.Errorf("DUCKDB_QUERY_TIMEOUT must be a positive duration")
	}
	secret := strings.TrimSpace(os.Getenv("DUCKDB_SERVICE_CLIENT_SECRET"))
	if len(secret) < 32 {
		return Config{}, fmt.Errorf("DUCKDB_SERVICE_CLIENT_SECRET must contain at least 32 bytes")
	}
	return Config{
		Addr:               ":" + port,
		SystemURL:          strings.TrimRight(env("SYSTEM_URL", "http://localhost:8180"), "/"),
		MetaURL:            strings.TrimRight(env("META_URL", "http://localhost:8182"), "/"),
		ClientSecret:       secret,
		MaxRows:            maxRows,
		MaxMemory:          env("DUCKDB_MAX_MEMORY", "1GB"),
		Threads:            threads,
		DefaultTimeout:     timeout,
		AllowedCallerIDs:   map[string]struct{}{"addp-develop": {}, "addp-service": {}},
		SourceLoopbackHost: strings.TrimSpace(os.Getenv("DUCKDB_SOURCE_LOOPBACK_HOST")),
	}, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func positiveInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}
