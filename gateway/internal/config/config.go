package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port               string
	Env                string
	SystemServiceURL   string
	ManagerServiceURL  string
	MetaServiceURL     string
	TransferServiceURL string
	DevelopServiceURL  string
	ServiceServiceURL  string
	CopilotServiceURL  string
	MonitorServiceURL  string
	StandardServiceURL string
	ModelServiceURL    string
	QualityServiceURL  string
	AssetServiceURL    string
	PortalServiceURL   string
	AgentServiceURL    string
	GraphServiceURL    string

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
	InternalAPIKey string

	// 模块发现配置
	ModuleRegistryEnabled bool          // 是否启用模块注册
	ModuleRefreshInterval time.Duration // 刷新间隔
}

func Load() *Config {
	port := getEnv("GATEWAY_PORT", "8000")
	if len(port) > 0 && port[0] != ':' {
		port = ":" + port
	}

	return &Config{
		Port:               port,
		Env:                getEnv("ENV", "development"),
		SystemServiceURL:   getEnv("SYSTEM_URL", "http://localhost:8180"),
		ManagerServiceURL:  getEnv("MANAGER_URL", "http://localhost:8081"),
		MetaServiceURL:     getEnv("META_URL", "http://localhost:8082"),
		TransferServiceURL: getEnv("TRANSFER_URL", "http://localhost:8083"),
		DevelopServiceURL:  getEnv("DEVELOP_URL", "http://localhost:8084"),
		ServiceServiceURL:  getEnv("SERVICE_URL", "http://localhost:8086"),
		CopilotServiceURL:  getEnv("COPILOT_URL", "http://localhost:8087"),
		MonitorServiceURL:  getEnv("MONITOR_URL", "http://localhost:8100"),
		StandardServiceURL: getEnv("STANDARD_URL", "http://localhost:8110"),
		ModelServiceURL:    getEnv("MODEL_URL", "http://localhost:8181"),
		QualityServiceURL:  getEnv("QUALITY_URL", "http://localhost:8182"),
		AssetServiceURL:    getEnv("ASSET_URL", "http://localhost:8183"),
		PortalServiceURL:   getEnv("PORTAL_URL", "http://localhost:8184"),
		AgentServiceURL:    getEnv("AGENT_URL", "http://localhost:8190"),
		GraphServiceURL:    getEnv("GRAPH_URL", "http://localhost:8186"),

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

		// Internal API Key for calling System module
		InternalAPIKey: getEnv("INTERNAL_API_KEY", "change-this-in-production"),

		// 模块发现配置
		ModuleRegistryEnabled: getEnvBool("MODULE_REGISTRY_ENABLED", true),
		ModuleRefreshInterval: getEnvDuration("MODULE_REGISTRY_REFRESH_INTERVAL", "30s"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
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
