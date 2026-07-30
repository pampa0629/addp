package config

import (
	"fmt"
	"os"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	Port     string
	DBSchema string

	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	SystemURL           string
	StandardURL         string
	MetaURL             string
	InternalAPIKey      string
	ServiceClientSecret string
}

func LoadConfig() (*Config, error) {
	commonConfig.LoadEnv()

	cfg := &Config{
		Port:     commonConfig.GetEnv("QUALITY_BACKEND_PORT", "8182"),
		DBSchema: "quality",

		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       commonConfig.GetEnvInt("REDIS_DB", 0),

		SystemURL:           commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		StandardURL:         commonConfig.GetEnv("STANDARD_URL", "http://localhost:8110"),
		MetaURL:             commonConfig.GetEnv("META_URL", "http://localhost:8082"),
		InternalAPIKey:      os.Getenv("INTERNAL_API_KEY"),
		ServiceClientSecret: os.Getenv("QUALITY_SERVICE_CLIENT_SECRET"),
	}

	enableIntegration := commonConfig.GetEnvBool("ENABLE_SERVICE_INTEGRATION", true)
	if err := commonConfig.LoadServiceConfiguration(commonConfig.ServiceConfigLoader{
		SystemServiceURL:      cfg.SystemURL,
		EnableIntegration:     enableIntegration,
		InternalAPIKey:        cfg.InternalAPIKey,
		BaseConfigDestination: &cfg.BaseConfig,
	}); err != nil {
		return nil, fmt.Errorf("failed to load service configuration: %w", err)
	}

	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL
	cfg.BaseConfig.EnableIntegration = enableIntegration

	return cfg, nil
}

func (c *Config) GetDatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		c.DBHost,
		c.DBPort,
		c.DBUser,
		c.DBPassword,
		c.DBName,
		c.DBSchema,
	)
}
