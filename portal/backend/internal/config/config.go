package config

import (
	"os"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	Port string

	SystemURL      string
	AssetURL       string
	InternalAPIKey string

	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int
}

func LoadConfig() *Config {
	commonConfig.LoadEnv()

	return &Config{
		Port:           commonConfig.GetEnv("PORTAL_BACKEND_PORT", "8184"),
		SystemURL:      commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		AssetURL:       commonConfig.GetEnv("ASSET_URL", "http://localhost:8183"),
		InternalAPIKey: os.Getenv("INTERNAL_API_KEY"),

		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       commonConfig.GetEnvInt("REDIS_DB", 0),
	}
}
