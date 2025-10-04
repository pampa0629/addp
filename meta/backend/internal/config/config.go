package config

import (
	"fmt"
	"log"
	"os"
)

type Config struct {
	ServerPort        string
	DBHost            string
	DBPort            string
	DBName            string
	DBUser            string
	DBPassword        string
	DBSchema          string
	SystemServiceURL  string
	EnableIntegration bool

	// Auto sync configuration
	AutoSyncEnabled   bool
	AutoSyncSchedule  string // Cron expression
	AutoSyncLevel     string // database | table | field
	DeepScanTimeout   string
	DeepScanBatchSize int
}

func LoadConfig() *Config {
	return &Config{
		ServerPort:        getEnv("SERVER_PORT", "8082"),
		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnv("DB_PORT", "5432"),
		DBName:            getEnv("DB_NAME", "addp"),
		DBUser:            getEnv("DB_USER", "addp"),
		DBPassword:        getEnv("DB_PASSWORD", "addp_password"),
		DBSchema:          getEnv("DB_SCHEMA", "metadata"),
		SystemServiceURL:  getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080"),
		EnableIntegration: getEnv("ENABLE_SERVICE_INTEGRATION", "true") == "true",

		AutoSyncEnabled:   getEnv("AUTO_SYNC_ENABLED", "true") == "true",
		AutoSyncSchedule:  getEnv("AUTO_SYNC_SCHEDULE", "0 0 * * *"), // Every day at midnight
		AutoSyncLevel:     getEnv("AUTO_SYNC_LEVEL", "database"),
		DeepScanTimeout:   getEnv("DEEP_SCAN_TIMEOUT", "30m"),
		DeepScanBatchSize: getEnvInt("DEEP_SCAN_BATCH_SIZE", 10),
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var intValue int
	if _, err := fmt.Sscanf(value, "%d", &intValue); err != nil {
		log.Printf("Invalid integer value for %s: %s, using default: %d", key, value, defaultValue)
		return defaultValue
	}
	return intValue
}
