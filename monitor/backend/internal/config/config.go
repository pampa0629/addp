package config

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// 服务配置
	ServerPort string

	// 数据库配置
	DatabaseHost     string
	DatabasePort     string
	DatabaseUser     string
	DatabasePassword string
	DatabaseName     string
	DatabaseSSLMode  string

	// Redis 配置
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// System 模块配置（用于健康检查）
	SystemURL string
	InternalAPIKey string
}

// LoadConfig 加载配置
func LoadConfig() (*Config, error) {
	// 加载 .env 文件（从项目根目录）
	if err := godotenv.Load("../../.env"); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

	// Redis DB 转换
	redisDB := 0
	if redisDBStr := os.Getenv("REDIS_DB"); redisDBStr != "" {
		var err error
		redisDB, err = strconv.Atoi(redisDBStr)
		if err != nil {
			return nil, fmt.Errorf("invalid REDIS_DB: %w", err)
		}
	}

	cfg := &Config{
		ServerPort: getEnvOrDefault("MONITOR_BACKEND_PORT", "8100"),

		DatabaseHost:     getEnvOrDefault("POSTGRES_HOST", "localhost"),
		DatabasePort:     getEnvOrDefault("POSTGRES_PORT", "15432"),
		DatabaseUser:     getEnvOrDefault("POSTGRES_USER", "addp"),
		DatabasePassword: getEnvOrDefault("POSTGRES_PASSWORD", "addp123"),
		DatabaseName:     getEnvOrDefault("POSTGRES_DB", "addp"),
		DatabaseSSLMode:  getEnvOrDefault("POSTGRES_SSLMODE", "disable"),

		RedisHost:     getEnvOrDefault("REDIS_HOST", "localhost"),
		RedisPort:     getEnvOrDefault("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       redisDB,

		SystemURL: getEnvOrDefault("SYSTEM_URL", "http://localhost:8180"),
		InternalAPIKey: os.Getenv("INTERNAL_API_KEY"),
	}

	return cfg, nil
}

// GetDatabaseDSN 获取数据库连接字符串
func (c *Config) GetDatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DatabaseHost,
		c.DatabasePort,
		c.DatabaseUser,
		c.DatabasePassword,
		c.DatabaseName,
		c.DatabaseSSLMode,
	)
}

// getEnvOrDefault 获取环境变量或默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
