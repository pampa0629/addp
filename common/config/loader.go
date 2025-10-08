package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// SharedConfig 从 System 服务获取的共享配置
type SharedConfig struct {
	JWTSecret      string `json:"jwt_secret"`
	EncryptionKey  string `json:"encryption_key"`
	InternalAPIKey string `json:"internal_api_key"`
	Database       struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		Name     string `json:"name"`
	} `json:"database"`
}

// BaseConfig 所有模块共享的基础配置字段
type BaseConfig struct {
	// 从 System 获取的共享配置
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	JWTSecret  string

	// 通用配置
	SystemServiceURL  string
	EnableIntegration bool
	EncryptionKey     []byte
	InternalAPIKey    string
}

// LoadSharedConfig 从 System 服务获取共享配置
func LoadSharedConfig(systemURL string, target *BaseConfig) error {
	url := fmt.Sprintf("%s/internal/config", systemURL)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 可选：添加内部 API Key
	if apiKey := os.Getenv("INTERNAL_API_KEY"); apiKey != "" {
		req.Header.Set("X-Internal-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to System service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var shared SharedConfig
	if err := json.NewDecoder(resp.Body).Decode(&shared); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// 应用共享配置
	target.JWTSecret = shared.JWTSecret
	target.DBHost = shared.Database.Host
	target.DBPort = shared.Database.Port
	target.DBUser = shared.Database.User
	target.DBPassword = shared.Database.Password
	target.DBName = shared.Database.Name
	target.InternalAPIKey = shared.InternalAPIKey

	// 解析加密密钥
	if shared.EncryptionKey != "" {
		key, err := base64.StdEncoding.DecodeString(shared.EncryptionKey)
		if err == nil && len(key) == 32 {
			target.EncryptionKey = key
		}
	}

	// 如果没有从 System 获取到加密密钥，使用本地加载
	if target.EncryptionKey == nil {
		target.EncryptionKey = LoadEncryptionKey()
	}

	return nil
}

// LoadLocalConfig 从本地环境变量加载配置（降级方案）
func LoadLocalConfig(target *BaseConfig) {
	target.JWTSecret = GetEnv("JWT_SECRET", "")
	target.DBHost = GetEnv("DB_HOST", "localhost")
	target.DBPort = GetEnv("DB_PORT", "5432")
	target.DBUser = GetEnv("DB_USER", "addp")
	target.DBPassword = GetEnv("DB_PASSWORD", "addp_password")
	target.DBName = GetEnv("DB_NAME", "addp")
	target.EncryptionKey = LoadEncryptionKey()
	target.InternalAPIKey = GetEnv("INTERNAL_API_KEY", "")

	if target.JWTSecret == "" {
		log.Println("⚠️  WARNING: JWT_SECRET is not set! Authentication will fail!")
	}
}

// LoadEncryptionKey 加载加密密钥 (32字节 AES-256)
func LoadEncryptionKey() []byte {
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

// GetEnv 获取环境变量，如果不存在则返回默认值
func GetEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// GetEnvInt 获取整数类型的环境变量
func GetEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var intValue int
	if _, err := fmt.Sscanf(value, "%d", &intValue); err != nil {
		log.Printf("Invalid integer value for %s: %s, using default: %d", key, value, defaultValue)
		return defaultValue
	}
	return intValue
}

// GetEnvDuration 获取时间duration类型的环境变量
func GetEnvDuration(key string, defaultValue string) time.Duration {
	value := GetEnv(key, defaultValue)
	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("Invalid duration value for %s: %s, using default: %s", key, value, defaultValue)
		duration, _ = time.ParseDuration(defaultValue)
	}
	return duration
}

// GetEnvBool 获取布尔类型的环境变量
func GetEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}
