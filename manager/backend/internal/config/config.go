package config

import (
	"encoding/base64"
	"log"
	"os"
)

type Config struct {
	Port              string
	DBHost            string
	DBPort            string
	DBName            string
	DBUser            string
	DBPassword        string
	DBSchema          string
	SystemServiceURL  string
	EnableIntegration bool
	EncryptionKey     []byte
}

func Load() *Config {
	// 加载加密密钥
	encryptionKey := loadEncryptionKey()

	return &Config{
		Port:              getEnv("PORT", "8081"),
		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnv("DB_PORT", "5432"),
		DBName:            getEnv("DB_NAME", "addp"),
		DBUser:            getEnv("DB_USER", "addp"),
		DBPassword:        getEnv("DB_PASSWORD", "addp_password"),
		DBSchema:          getEnv("DB_SCHEMA", "manager"),
		SystemServiceURL:  getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080"),
		EnableIntegration: getEnv("ENABLE_SERVICE_INTEGRATION", "true") == "true",
		EncryptionKey:     encryptionKey,
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// loadEncryptionKey 加载加密密钥 (32字节 AES-256)
func loadEncryptionKey() []byte {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if keyStr == "" {
		// 开发环境使用默认密钥 (生产环境必须设置!)
		log.Println("WARNING: ENCRYPTION_KEY not set, using default key (INSECURE for production!)")
		// 使用固定的32字节密钥作为开发默认值
		return []byte("dev-encryption-key-32-bytes!") // 正好32字节
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