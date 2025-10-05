package config

import (
	"encoding/base64"
	"log"
	"os"
)

type Config struct {
	Env                string
	ServerAddr         string
	DatabaseURL        string
	JWTSecret          string
	EncryptionKey      []byte
	TokenExpireMinutes int
	ProjectName        string

	// PostgreSQL 配置（用于其他模块）
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string

	// 内部 API Key（用于服务间调用）
	InternalAPIKey string
}

func Load() *Config {
	// 加载加密密钥
	encryptionKey := loadEncryptionKey()

	return &Config{
		Env:                getEnv("ENV", "development"),
		ServerAddr:         getEnv("SERVER_ADDR", ":8080"),
		DatabaseURL:        "",  // PostgreSQL 不使用此字段
		JWTSecret:          getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		EncryptionKey:      encryptionKey,
		TokenExpireMinutes: 30,
		ProjectName:        getEnv("PROJECT_NAME", "全域数据平台"),

		// PostgreSQL 配置
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:     getEnv("POSTGRES_USER", "addp"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "addp_password"),
		PostgresDB:       getEnv("POSTGRES_DB", "addp"),

		// 内部 API Key（可选，用于服务间调用安全）
		InternalAPIKey: getEnv("INTERNAL_API_KEY", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// loadEncryptionKey 加载加密密钥 (32字节 AES-256)
func loadEncryptionKey() []byte {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if keyStr == "" {
		// 开发环境使用默认密钥 (生产环境必须设置!)
		log.Println("WARNING: ENCRYPTION_KEY not set, using default key (INSECURE for production!)")
		// 使用固定的32字节密钥作为开发默认值 (256 bits = 32 bytes)
		return []byte("addp-dev-encryption-key-2025!!!!") // 正好32字节
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