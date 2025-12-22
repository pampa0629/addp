package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"
)

type Config struct {
	Env        string
	ServerAddr string
	JWTSecret  string

	// PostgreSQL 配置
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	DBSchema         string // develop schema

	// System 服务配置（用于获取资源配置和认证）
	SystemServiceURL             string
	EnableServiceIntegration     bool
	EncryptionKey                []byte
	InternalAPIKey               string

	// GeoPandas Engine 配置
	GeoPandasEngineURL string

	// Spark Sedona Engine 配置
	SparkEngineURL string

	// 其他模块服务配置（用于算子发现）
	MetaServiceURL     string
	TransferServiceURL string
	ManagerServiceURL  string

	// SQL 执行配置
	DefaultQueryTimeout int // 默认查询超时(秒)
	MaxQueryTimeout     int // 最大查询超时(秒)
}

func Load() *Config {
	env := getEnv("ENV", "development")

	// JWT Secret（可从 System 服务获取，或使用本地配置）
	jwtSecret := getEnv("JWT_SECRET", "your-secret-key-change-in-production")

	// 开发环境警告
	if env == "development" && strings.Contains(jwtSecret, "change-in-production") {
		log.Println("WARNING: Using default JWT_SECRET in development environment.")
	}

	// 加载加密密钥
	encryptionKey := loadEncryptionKey()

	// 内部 API Key
	internalAPIKey := getEnv("INTERNAL_API_KEY", "")

	return &Config{
		Env:        env,
		ServerAddr: getEnv("PORT", "8085"),
		JWTSecret:  jwtSecret,

		// PostgreSQL 配置
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:     getEnv("POSTGRES_USER", "addp"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "addp_password"),
		PostgresDB:       getEnv("POSTGRES_DB", "addp"),
		DBSchema:         "develop",

		// System 服务集成
		SystemServiceURL:         getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080"),
		EnableServiceIntegration: getEnvAsBool("ENABLE_SERVICE_INTEGRATION", true),
		EncryptionKey:            encryptionKey,
		InternalAPIKey:           internalAPIKey,

		// GeoPandas Engine 配置
		GeoPandasEngineURL: getEnv("GEOPANDAS_ENGINE_URL", "http://localhost:8099"),

		// Spark Sedona Engine 配置
		SparkEngineURL: getEnv("SPARK_ENGINE_URL", "http://localhost:8098"),

		// 其他模块服务配置
		MetaServiceURL:     getEnv("META_SERVICE_URL", "http://localhost:8082"),
		TransferServiceURL: getEnv("TRANSFER_SERVICE_URL", "http://localhost:8083"),
		ManagerServiceURL:  getEnv("MANAGER_SERVICE_URL", "http://localhost:8081"),

		// SQL 执行配置
		DefaultQueryTimeout: getEnvAsInt("DEFAULT_QUERY_TIMEOUT", 30),  // 30秒
		MaxQueryTimeout:     getEnvAsInt("MAX_QUERY_TIMEOUT", 300),     // 5分钟
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "t", "yes", "y":
			return true
		case "0", "false", "f", "no", "n":
			return false
		}
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intVal int
		if _, err := fmt.Sscanf(value, "%d", &intVal); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// loadEncryptionKey 加载加密密钥 (32字节 AES-256)
func loadEncryptionKey() []byte {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if keyStr == "" {
		// 开发环境使用默认密钥
		log.Println("WARNING: ENCRYPTION_KEY not set, using default key (INSECURE for production!)")
		return []byte("addp-dev-encryption-key-2025!!!!")
	}

	// 从 Base64 解码密钥
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		log.Fatalf("Failed to decode ENCRYPTION_KEY: %v", err)
	}

	if len(key) != 32 {
		log.Fatalf("ENCRYPTION_KEY must be 32 bytes (256 bits), got %d bytes", len(key))
	}

	return key
}
