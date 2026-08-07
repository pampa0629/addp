package config

import (
	"os"
	"time"
)

type Config struct {
	Port             string
	Env              string
	SystemServiceURL string

	// Database
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSchema   string

	// Redis
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Authentication
	ServiceClientSecret string

	ModuleRefreshInterval time.Duration
}

func Load() *Config {
	port := getEnv("GATEWAY_PORT", "8000")
	if len(port) > 0 && port[0] != ':' {
		port = ":" + port
	}

	return &Config{
		Port:             port,
		Env:              getEnv("ENV", "development"),
		SystemServiceURL: getEnv("SYSTEM_URL", "http://localhost:8180"),

		// Database (defaults to system PostgreSQL)
		DBHost:     getEnv("POSTGRES_HOST", "localhost"),
		DBPort:     getEnv("POSTGRES_PORT", "5432"),
		DBUser:     getEnv("POSTGRES_USER", "addp"),
		DBPassword: getEnv("POSTGRES_PASSWORD", "addp_password"),
		DBName:     getEnv("POSTGRES_DB", "addp"),
		DBSchema:   getEnv("DB_SCHEMA", "gateway"),

		// Redis (defaults to system Redis)
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", "addp_redis"),
		RedisDB:       0, // Default DB

		ServiceClientSecret: getEnv("GATEWAY_SERVICE_CLIENT_SECRET", ""),

		ModuleRefreshInterval: getEnvDuration("MODULE_REFRESH_INTERVAL", "30s"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvDuration(key, defaultValue string) time.Duration {
	value := getEnv(key, defaultValue)
	duration, err := time.ParseDuration(value)
	if err != nil {
		duration, _ = time.ParseDuration(defaultValue)
	}
	return duration
}
