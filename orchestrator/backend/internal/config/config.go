package config

import (
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/infra/database"
)

// Config Orchestrator 配置
type Config struct {
	ServerPort string

	// System 服务配置（用于能力注册中心）
	SystemServiceURL    string
	ServiceClientSecret string

	// Redis 配置
	RedisHost     string
	RedisPort     string
	RedisPassword string
}

// LoadConfig 加载配置
func LoadConfig() *Config {
	return &Config{
		ServerPort: commonConfig.GetEnv("ORCHESTRATOR_BACKEND_PORT", "8084"),

		SystemServiceURL:    commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		ServiceClientSecret: commonConfig.GetEnv("ORCHESTRATOR_SERVICE_CLIENT_SECRET", ""),

		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: commonConfig.GetEnv("REDIS_PASSWORD", ""),
	}
}

func PlatformDatabase() database.Config {
	return database.Config{Host: commonConfig.GetEnv("POSTGRES_HOST", "localhost"), Port: commonConfig.GetEnv("POSTGRES_PORT", "15432"), DBName: commonConfig.GetEnv("POSTGRES_DB", "addp"), Schema: "orchestrator"}
}
