package config

import (
	"os"
	"time"

	"github.com/addp/common/infra/database"
)

type Config struct {
	Port             string
	Env              string
	SystemServiceURL string

	// Database
	Database database.Config

	// Redis
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Authentication
	ServiceClientSecret string

	ModuleWatchTimeout       time.Duration
	MetricsControlListen     string
	MetricsControlServerName string
	MetricsControlTLSDir     string
}

func Load() *Config {
	port := getEnv("GATEWAY_PORT", "8000")
	if len(port) > 0 && port[0] != ':' {
		port = ":" + port
	}
	moduleWatchTimeout := getEnvDuration("MODULE_WATCH_TIMEOUT", "10s")
	if moduleWatchTimeout < time.Second || moduleWatchTimeout > 30*time.Second {
		moduleWatchTimeout = 10 * time.Second
	}

	return &Config{
		Port:             port,
		Env:              getEnv("ENV", "development"),
		SystemServiceURL: getEnv("SYSTEM_URL", "http://localhost:8180"),

		Database: PlatformDatabase(),

		// Redis (defaults to system Redis)
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", "addp_redis"),
		RedisDB:       0, // Default DB

		ServiceClientSecret: getEnv("GATEWAY_SERVICE_CLIENT_SECRET", ""),

		ModuleWatchTimeout:       moduleWatchTimeout,
		MetricsControlListen:     os.Getenv("GATEWAY_METRICS_CONTROL_LISTEN"),
		MetricsControlServerName: os.Getenv("GATEWAY_METRICS_CONTROL_SERVER_NAME"),
		MetricsControlTLSDir:     os.Getenv("GATEWAY_METRICS_CONTROL_TLS_DIR"),
	}
}

// PlatformDatabase reads the PostgreSQL endpoint shared by deployment and runtime.
func PlatformDatabase() database.Config {
	return database.Config{Host: getEnv("POSTGRES_HOST", "localhost"), Port: getEnv("POSTGRES_PORT", "5432"),
		DBName: getEnv("POSTGRES_DB", "addp"), Schema: "gateway"}
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
