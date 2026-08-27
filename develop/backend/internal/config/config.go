package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env        string
	ServerAddr string

	// PostgreSQL 配置
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	DBSchema         string // develop schema

	// System 服务配置（用于获取资源配置和认证）
	SystemServiceURL    string
	DevelopServiceURL   string
	EncryptionKey       []byte
	ServiceClientSecret string

	// 其他模块服务配置（用于算子发现）
	MetaServiceURL     string
	TransferServiceURL string
	ManagerServiceURL  string
	CopilotServiceURL  string

	// SQL 执行配置
	DefaultQueryTimeout int // 默认查询超时(秒)
	MaxQueryTimeout     int // 最大查询超时(秒)
	QueryResultLimit    int // execution 结果预览最大行数

	QueryWorkerConcurrency int
	QueryLeaseDuration     time.Duration
	QueryHeartbeatInterval time.Duration
	QueryClaimInterval     time.Duration

	// Redis 配置（资源回收 request/result）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int
}

func Load() *Config {
	env := getEnv("ENV", "development")

	// 加载加密密钥
	encryptionKey := loadEncryptionKey()

	return &Config{
		Env:        env,
		ServerAddr: ":" + getEnv("DEVELOP_BACKEND_PORT", "8185"),

		// PostgreSQL 配置
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:     getEnv("POSTGRES_USER", "addp"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "addp_password"),
		PostgresDB:       getEnv("POSTGRES_DB", "addp"),
		DBSchema:         "develop",

		// System 服务集成
		SystemServiceURL:    getEnv("SYSTEM_URL", "http://localhost:8180"),
		DevelopServiceURL:   getEnv("DEVELOP_URL", "http://localhost:8185"),
		EncryptionKey:       encryptionKey,
		ServiceClientSecret: getEnv("DEVELOP_SERVICE_CLIENT_SECRET", ""),

		// 其他模块服务配置
		MetaServiceURL:     getEnv("META_URL", "http://localhost:8082"),
		TransferServiceURL: getEnv("TRANSFER_URL", "http://localhost:8083"),
		ManagerServiceURL:  getEnv("MANAGER_URL", "http://localhost:8081"),
		CopilotServiceURL:  getEnv("COPILOT_URL", "http://localhost:8087"),

		// SQL 执行配置
		DefaultQueryTimeout:    30,
		MaxQueryTimeout:        300,
		QueryResultLimit:       getEnvAsInt("QUERY_RESULT_LIMIT", 500),
		QueryWorkerConcurrency: getEnvAsInt("DEVELOP_QUERY_WORKER_CONCURRENCY", 4),
		QueryLeaseDuration:     time.Duration(getEnvAsInt("DEVELOP_QUERY_LEASE_SECONDS", 120)) * time.Second,
		QueryHeartbeatInterval: time.Duration(getEnvAsInt("DEVELOP_QUERY_HEARTBEAT_SECONDS", 30)) * time.Second,
		QueryClaimInterval:     time.Duration(getEnvAsInt("DEVELOP_QUERY_CLAIM_INTERVAL_SECONDS", 1)) * time.Second,

		// Redis 配置
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "16379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvAsInt("REDIS_DB", 0),
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
