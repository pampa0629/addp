package config

import "os"

type Config struct {
	Port              string
	DBHost            string
	DBPort            string
	DBName            string
	DBUser            string
	DBPassword        string
	DBSchema          string
	SystemServiceURL  string
	EnableIntegration bool
}

func Load() *Config {
	return &Config{
		Port:              getEnv("PORT", "8081"),
		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnv("DB_PORT", "5432"),
		DBName:            getEnv("DB_NAME", "addp"),
		DBUser:            getEnv("DB_USER", "addp"),
		DBPassword:        getEnv("DB_PASSWORD", "addp_password"),
		DBSchema:          getEnv("DB_SCHEMA", "manager"),
		SystemServiceURL:  getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080"),
		EnableIntegration: getEnv("ENABLE_SERVICE_INTEGRATION", "true") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}