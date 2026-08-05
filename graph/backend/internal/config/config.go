package config

import (
	"fmt"

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
	minioCfg := commonConfig.LoadBuiltinMinIOConfig()

	cfg := &Config{
		Port:              commonConfig.GetEnv("GRAPH_BACKEND_PORT", "8186"),
		DBSchema:          "graph",
		SystemServiceURL:  systemURL,
		InternalAPIKey:    commonConfig.GetEnv("INTERNAL_API_KEY", ""),
		ModelServiceURL:   commonConfig.GetEnv("MODEL_URL", "http://localhost:8181"),
		CopilotServiceURL: commonConfig.GetEnv("COPILOT_URL", "http://localhost:8087"),
		RedisHost:         commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:         commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword:     commonConfig.GetEnv("REDIS_PASSWORD", ""),
		RedisDB:           commonConfig.GetEnvInt("REDIS_DB", 0),
		MinioEndpoint:     minioCfg.Endpoint,
		MinioAccessKey:    minioCfg.AccessKey,
		MinioSecretKey:    minioCfg.SecretKey,
	}

	cfg.BaseConfig.SystemServiceURL = systemURL

	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)

	return cfg
}

func (c *Config) GetDatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSchema,
	)
}
