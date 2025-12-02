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

	// 模块服务 URL
	TransferServiceURL string
	MetaServiceURL     string
	ManagerServiceURL  string
}

// LoadConfig 加载配置
func LoadConfig() *Config {
	// 加载 .env 文件 (从项目根目录加载)
	if err := godotenv.Load("../../../../.env"); err != nil {
		log.Println("未找到 .env 文件，使用环境变量")
	}

	return &Config{
		ServerPort: getEnv("ORCHESTRATOR_PORT", "8084"),

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "addp"),
		DBPassword: getEnv("DB_PASSWORD", "addp_password"),
		DBName:     getEnv("DB_NAME", "addp"),
		DBSchema:   "orchestrator",

		TransferServiceURL: getEnv("TRANSFER_SERVICE_URL", "http://localhost:8083"),
		MetaServiceURL:     getEnv("META_SERVICE_URL", "http://localhost:8082"),
		ManagerServiceURL:  getEnv("MANAGER_SERVICE_URL", "http://localhost:8081"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
