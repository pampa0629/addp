package config

import "os"

type Config struct {
	Port               string
	Env                string
	SystemServiceURL   string
	ManagerServiceURL  string
	MetaServiceURL     string
	TransferServiceURL string
	DevelopServiceURL  string
	ServiceServiceURL  string

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
}

func Load() *Config {
	port := getEnv("PORT", ":8000")
	if len(port) > 0 && port[0] != ':' {
		port = ":" + port
	}

	return &Config{
		Port:               port,
		Env:                getEnv("ENV", "development"),
		SystemServiceURL:   getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080"),
		ManagerServiceURL:  getEnv("MANAGER_SERVICE_URL", "http://localhost:8081"),
		MetaServiceURL:     getEnv("META_SERVICE_URL", "http://localhost:8082"),
		TransferServiceURL: getEnv("TRANSFER_SERVICE_URL", "http://localhost:8083"),
		DevelopServiceURL:  getEnv("DEVELOP_SERVICE_URL", "http://localhost:8084"),
		ServiceServiceURL:  getEnv("SERVICE_SERVICE_URL", "http://localhost:8086"),

		// Database (defaults to system PostgreSQL)
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "addp"),
		DBPassword: getEnv("DB_PASSWORD", "addp_password"),
		DBName:     getEnv("DB_NAME", "addp"),
		DBSchema:   getEnv("DB_SCHEMA", "gateway"),

		// Redis (defaults to system Redis)
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", "addp_redis"),
		RedisDB:       0, // Default DB

		// Internal API Key for calling System module
		InternalAPIKey: getEnv("INTERNAL_API_KEY", "change-this-in-production"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
