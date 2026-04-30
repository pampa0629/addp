package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

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
	SystemServiceURL string
	InternalAPIKey   string

	// 模块服务 URL（向后兼容）
	TransferServiceURL string
	MetaServiceURL     string
	ManagerServiceURL  string

	// Redis 配置
	RedisHost     string
	RedisPort     string
	RedisPassword string
}

// LoadConfig 加载配置
func LoadConfig() *Config {
	// 加载 .env 文件 (从项目根目录加载)
	if err := godotenv.Load("../../../../.env"); err != nil {
		log.Println("未找到 .env 文件，使用环境变量")
	}

	return &Config{
		ServerPort: getEnv("ORCHESTRATOR_BACKEND_PORT", "8084"),

		DBHost:     getEnv("POSTGRES_HOST", "localhost"),
		DBPort:     getEnv("POSTGRES_PORT", "5432"),
		DBUser:     getEnv("POSTGRES_USER", "addp"),
		DBPassword: getEnv("POSTGRES_PASSWORD", "addp_password"),
		DBName:     getEnv("POSTGRES_DB", "addp"),
		DBSchema:   "orchestrator",

		SystemServiceURL: getEnv("SYSTEM_URL", "http://localhost:8180"),
		InternalAPIKey:   getEnv("INTERNAL_API_KEY", ""),

		TransferServiceURL: getEnv("TRANSFER_URL", "http://localhost:8083"),
		MetaServiceURL:     getEnv("META_URL", "http://localhost:8082"),
		ManagerServiceURL:  getEnv("MANAGER_URL", "http://localhost:8081"),

		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
