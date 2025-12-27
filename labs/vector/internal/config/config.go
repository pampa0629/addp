package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

// Config 应用配置
type Config struct {
	// 向量化服务类型：clip 或 dashscope
	EmbeddingServiceType string

	// CLIP 服务配置
	CLIPServiceURL string

	// DashScope 配置（可选）
	DashScopeBaseURL string
	DashScopeAPIKey  string

	// PostgreSQL 配置
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSchema   string
	DBTable    string
	DBSSLMode  string

	// 向量维度（0 表示自动检测）
	VectorDimension int

	// 日志配置
	LogLevel string
}

// Load 加载配置
func Load() *Config {
	// 加载 .env 文件
	envPath := findEnvFile()
	if envPath != "" {
		if err := godotenv.Load(envPath); err != nil {
			log.Printf("⚠️  警告: 无法加载 .env 文件 (%s): %v", envPath, err)
		} else {
			log.Printf("✅ 已加载配置: %s", envPath)
		}
	}

	cfg := &Config{
		// 向量化服务类型
		EmbeddingServiceType: getEnv("EMBEDDING_SERVICE_TYPE", "clip"),

		// CLIP 服务
		CLIPServiceURL: getEnv("CLIP_SERVICE_URL", "http://localhost:8888"),

		// DashScope（可选）
		DashScopeBaseURL: getEnv("EMBEDDING_SERVICE_BASE_URL", ""),
		DashScopeAPIKey:  getEnv("EMBEDDING_SERVICE_API_KEY", ""),

		// Database
		DBHost:     getEnv("VECTOR_DB_HOST", "localhost"),
		DBPort:     getEnv("VECTOR_DB_PORT", "5436"),
		DBUser:     getEnv("VECTOR_DB_USER", "vector_user"),
		DBPassword: getEnv("VECTOR_DB_PASSWORD", "vector_pass_2025"),
		DBName:     getEnv("VECTOR_DB_NAME", "vector_db"),
		DBSchema:   getEnv("VECTOR_DB_SCHEMA", "vector_store"),
		DBTable:    getEnv("VECTOR_DB_TABLE", "embeddings"),
		DBSSLMode:  getEnv("VECTOR_DB_SSL_MODE", "disable"),

		// Vector
		VectorDimension: getEnvInt("VECTOR_DIMENSION", 0),

		// Log
		LogLevel: getEnv("LOG_LEVEL", "info"),
	}

	// 验证必需配置（根据选择的服务类型）
	switch cfg.EmbeddingServiceType {
	case "dashscope":
		if cfg.DashScopeAPIKey == "" {
			log.Fatal("❌ 错误: EMBEDDING_SERVICE_API_KEY 未设置（DashScope 服务需要）")
		}
		if cfg.DashScopeBaseURL == "" {
			log.Fatal("❌ 错误: EMBEDDING_SERVICE_BASE_URL 未设置（DashScope 服务需要）")
		}
	case "clip":
		if cfg.CLIPServiceURL == "" {
			log.Fatal("❌ 错误: CLIP_SERVICE_URL 未设置")
		}
	default:
		log.Fatalf("❌ 错误: 不支持的向量化服务类型: %s（支持: clip, dashscope）", cfg.EmbeddingServiceType)
	}

	return cfg
}

// GetDSN 返回 PostgreSQL 连接字符串
func (c *Config) GetDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode)
}

// findEnvFile 查找 .env 文件（向上查找）
func findEnvFile() string {
	cwd, _ := os.Getwd()
	for dir := cwd; dir != "/"; dir = filepath.Dir(dir) {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}
	return ""
}

// getEnv 获取环境变量
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt 获取整数环境变量
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
