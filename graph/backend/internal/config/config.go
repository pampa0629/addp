package config

import (
	"strconv"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/infra/database"
)

type Config struct {
	commonConfig.BaseConfig

	Port     string
	DBSchema string

	SystemServiceURL      string
	SecurityServiceURL    string
	ModelServiceURL       string
	CopilotServiceURL     string
	ServiceClientSecret   string
	BuildLeaseSeconds     int
	BuildHeartbeatSeconds int
	BuildPollSeconds      int

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
		Port:                  commonConfig.GetEnv("GRAPH_BACKEND_PORT", "8186"),
		DBSchema:              "graph",
		SystemServiceURL:      systemURL,
		SecurityServiceURL:    commonConfig.GetEnv("SECURITY_URL", "http://localhost:8194"),
		ModelServiceURL:       commonConfig.GetEnv("MODEL_URL", "http://localhost:8181"),
		CopilotServiceURL:     commonConfig.GetEnv("COPILOT_URL", "http://localhost:8087"),
		ServiceClientSecret:   commonConfig.GetEnv("GRAPH_SERVICE_CLIENT_SECRET", ""),
		BuildLeaseSeconds:     graphRuntimeInt("GRAPH_BUILD_LEASE_SECONDS", 120),
		BuildHeartbeatSeconds: graphRuntimeInt("GRAPH_BUILD_HEARTBEAT_SECONDS", 20),
		BuildPollSeconds:      graphRuntimeInt("GRAPH_BUILD_POLL_SECONDS", 1),
		RedisHost:             commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:             commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword:         commonConfig.GetEnv("REDIS_PASSWORD", ""),
		RedisDB:               commonConfig.GetEnvInt("REDIS_DB", 0),
		MinioEndpoint:         minioCfg.Endpoint,
		MinioAccessKey:        minioCfg.AccessKey,
		MinioSecretKey:        minioCfg.SecretKey,
	}

	cfg.BaseConfig.SystemServiceURL = systemURL

	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)

	return cfg
}

func graphRuntimeInt(key string, defaultValue int) int {
	value := commonConfig.GetEnv(key, strconv.Itoa(defaultValue))
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func (c *Config) PlatformDatabase() database.Config {
	return database.Config{
		Host: commonConfig.GetEnv("POSTGRES_HOST", "localhost"), Port: commonConfig.GetEnv("POSTGRES_PORT", "15432"), DBName: commonConfig.GetEnv("POSTGRES_DB", "addp"), Schema: c.DBSchema,
	}
}
