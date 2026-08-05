package config

import commonConfig "github.com/addp/common/config"

// Config Orchestrator 配置
type Config struct {
	ServerPort string

	// 数据库配置
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSchema   string

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

		DBHost:     commonConfig.GetEnv("POSTGRES_HOST", "localhost"),
		DBPort:     commonConfig.GetEnv("POSTGRES_PORT", "15432"),
		DBUser:     commonConfig.GetEnv("POSTGRES_USER", "addp"),
		DBPassword: commonConfig.GetEnv("POSTGRES_PASSWORD", "addp_password"),
		DBName:     commonConfig.GetEnv("POSTGRES_DB", "addp"),
		DBSchema:   "orchestrator",

		SystemServiceURL:    commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		ServiceClientSecret: commonConfig.GetEnv("ORCHESTRATOR_SERVICE_CLIENT_SECRET", ""),

		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: commonConfig.GetEnv("REDIS_PASSWORD", ""),
	}
}
