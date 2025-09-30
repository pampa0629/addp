package config

import "os"

type Config struct {
	Port               string
	Env                string
	SystemServiceURL   string
	ManagerServiceURL  string
	MetaServiceURL     string
	TransferServiceURL string
}

func Load() *Config {
	return &Config{
		Port:               getEnv("PORT", ":8000"),
		Env:                getEnv("ENV", "development"),
		SystemServiceURL:   getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080"),
		ManagerServiceURL:  getEnv("MANAGER_SERVICE_URL", "http://localhost:8081"),
		MetaServiceURL:     getEnv("META_SERVICE_URL", "http://localhost:8082"),
		TransferServiceURL: getEnv("TRANSFER_SERVICE_URL", "http://localhost:8083"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}