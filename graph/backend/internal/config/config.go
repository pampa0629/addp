package config

import (
	"fmt"
	"log"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	Port     string
	DBSchema string

	SystemServiceURL  string
	InternalAPIKey    string
	ModelServiceURL   string
	CopilotServiceURL string

	// Redis 配置（资源回收 request/result）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// MinIO 配置（用于构建材料文件存储）
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180")

	cfg := &Config{
		Port:              commonConfig.GetEnv("GRAPH_BACKEND_PORT", "8186"),
		DBSchema:          "graph",
		SystemServiceURL:  systemURL,
		InternalAPIKey:    commonConfig.GetEnv("INTERNAL_API_KEY", ""),
		ModelServiceURL:   commonConfig.GetEnv("MODEL_URL", "http://localhost:8181"),
		CopilotServiceURL: commonConfig.GetEnv("COPILOT_URL", "http://localhost:8085"),
		RedisHost:         commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:         commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword:     commonConfig.GetEnv("REDIS_PASSWORD", ""),
		RedisDB:           commonConfig.GetEnvInt("REDIS_DB", 0),
		MinioEndpoint:     commonConfig.GetEnv("MINIO_SYSTEM_ENDPOINT", commonConfig.GetEnv("MINIO_ENDPOINT", "http://localhost:"+commonConfig.GetEnv("MINIO_API_PORT", "19000"))),
		MinioAccessKey:    commonConfig.GetEnv("MINIO_SYSTEM_ACCESS_KEY", commonConfig.GetEnv("MINIO_ROOT_USER", commonConfig.GetEnv("MINIO_ACCESS_KEY", "minioadmin"))),
		MinioSecretKey:    commonConfig.GetEnv("MINIO_SYSTEM_SECRET_KEY", commonConfig.GetEnv("MINIO_ROOT_PASSWORD", commonConfig.GetEnv("MINIO_SECRET_KEY", "minioadmin"))),
	}

	cfg.BaseConfig.SystemServiceURL = systemURL
	cfg.EnableIntegration = commonConfig.GetEnvBool("ENABLE_SERVICE_INTEGRATION", true)

	if cfg.EnableIntegration {
		log.Println("🔄 Attempting to load shared config from System service...")
		if err := commonConfig.LoadSharedConfig(systemURL, &cfg.BaseConfig); err != nil {
			log.Printf("⚠️  Warning: Failed to load shared config from System: %v", err)
			log.Printf("⚠️  Falling back to local environment variables...")
			commonConfig.LoadLocalConfig(&cfg.BaseConfig)
		} else {
			log.Println("✅ Successfully loaded shared config from System service")
		}
	} else {
		log.Println("ℹ️  Service integration disabled, using local config")
		commonConfig.LoadLocalConfig(&cfg.BaseConfig)
	}

	return cfg
}

func (c *Config) GetDatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSchema,
	)
}
