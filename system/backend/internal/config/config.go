package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"
)

type Config struct {
	Env                     string
	ServerAddr              string
	DatabaseURL             string
	JWTSecret               string
	EncryptionKey           []byte
	TokenExpireMinutes      int
	ProjectName             string
	AllowPublicRegistration bool

	// PostgreSQL 配置（用于其他模块）
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string

	// 内部 API Key（用于服务间调用）
	InternalAPIKey string

	// 地图服务配置
	AMapKey         string
	AMapSecurityKey string
	TDTKey          string

	// Redis 配置（用于事件通知）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// 内置引擎服务 URL（用于 extension_api_config.base_url）
	SystemServiceURL      string
	MetaServiceURL        string
	TransferServiceURL    string
	ManagerServiceURL     string
	OrchestratorServiceURL string
	DevelopServiceURL     string
	GeopandasEngineURL    string

	// 日志归档配置
	LogRetentionDays int    // 日志保留天数
	LogArchiveEnable bool   // 是否启用归档
	LogCleanupEnable bool   // 是否启用清理
	LogArchiveCron   string // 归档任务 cron 表达式

	// CORS 配置
	AllowedOrigins []string // CORS 白名单
}

func Load() *Config {
	// 加载环境配置
	env := getEnv("ENV", "development")

	// 加载 JWT Secret（CRITICAL: 必须设置且符合安全标准）
	jwtSecret := getEnv("JWT_SECRET", "your-secret-key-change-in-production")

	// P0-1: 验证 JWT_SECRET 强度
	if jwtSecret == "" {
		log.Fatal("FATAL: JWT_SECRET must be set!\n" +
			"Generate a secure random key with:\n" +
			"  Method 1: openssl rand -base64 64\n" +
			"  Method 2: python3 -c \"import secrets; print(secrets.token_urlsafe(64))\"")
	}

	// P0-1: 验证密钥长度（至少 32 字符 / 256 bits）
	if len(jwtSecret) < 32 {
		log.Fatalf("FATAL: JWT_SECRET must be at least 32 characters (256 bits), got %d characters", len(jwtSecret))
	}

	// P0-1: 生产环境禁止使用默认密钥
	if env == "production" && strings.Contains(jwtSecret, "change-in-production") {
		log.Fatal("FATAL: Default JWT_SECRET detected in production environment!\n" +
			"This is a critical security risk. Generate a new secret immediately.")
	}

	// 警告：开发环境也应使用强密钥
	if env == "development" && strings.Contains(jwtSecret, "change-in-production") {
		log.Println("WARNING: Using default JWT_SECRET in development environment.")
		log.Println("Consider generating a secure random key for better security.")
	}

	// 加载加密密钥
	encryptionKey := loadEncryptionKey()

	// 加载内部 API Key
	internalAPIKey := getEnv("INTERNAL_API_KEY", "")
	if internalAPIKey != "" {
		log.Printf("✅ INTERNAL_API_KEY loaded (length: %d)", len(internalAPIKey))
	} else {
		log.Println("⚠️  WARNING: INTERNAL_API_KEY is not set! Internal API endpoints will not be accessible.")
	}

	// 加载 CORS 白名单
	allowedOriginsStr := getEnv("ALLOWED_ORIGINS", "http://localhost:5170,http://localhost:5173")
	allowedOrigins := strings.Split(allowedOriginsStr, ",")
	// 去除空白字符
	for i, origin := range allowedOrigins {
		allowedOrigins[i] = strings.TrimSpace(origin)
	}
	log.Printf("✅ CORS AllowedOrigins: %v", allowedOrigins)

	return &Config{
		Env:                     env,
		ServerAddr:              getEnv("SERVER_ADDR", ":8180"),
		DatabaseURL:             "", // PostgreSQL 不使用此字段
		JWTSecret:               jwtSecret,
		EncryptionKey:           encryptionKey,
		TokenExpireMinutes:      getEnvAsInt("JWT_EXPIRE_MINUTES", 180),
		ProjectName:             getEnv("PROJECT_NAME", "全域数据平台"),
		AllowPublicRegistration: getEnvAsBool("ALLOW_PUBLIC_REGISTRATION", false),

		// PostgreSQL 配置
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:     getEnv("POSTGRES_USER", "addp"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "addp_password"),
		PostgresDB:       getEnv("POSTGRES_DB", "addp"),

		// 内部 API Key（可选，用于服务间调用安全）
		InternalAPIKey: internalAPIKey,

		// 地图服务配置（默认使用提供的高德开放平台 Key）
		AMapKey:         getEnv("AMAP_KEY", "7babce80a669a0fac7a8c4c951f7c952"),
		AMapSecurityKey: getEnv("AMAP_SECURITY_KEY", "5784bbf4bbcffc8815cb44db32439b7d"),
		TDTKey:          getEnv("TDT_KEY", "fa4585302823605b16464e5838dafdcd"),

		// Redis 配置
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvAsInt("REDIS_DB", 0),

		// 内置引擎服务 URL
		SystemServiceURL:      getEnv("SYSTEM_SERVICE_URL", "http://localhost:8180"),
		MetaServiceURL:        getEnv("META_SERVICE_URL", "http://localhost:8082"),
		TransferServiceURL:    getEnv("TRANSFER_SERVICE_URL", "http://localhost:8083"),
		ManagerServiceURL:     getEnv("MANAGER_SERVICE_URL", "http://localhost:8081"),
		OrchestratorServiceURL: getEnv("ORCHESTRATOR_SERVICE_URL", "http://localhost:8084"),
		DevelopServiceURL:     getEnv("DEVELOP_SERVICE_URL", "http://localhost:8085"),
		GeopandasEngineURL:    getEnv("GEOPANDAS_ENGINE_URL", "http://localhost:8090"),

		// 日志归档配置
		LogRetentionDays: getEnvAsInt("LOG_RETENTION_DAYS", 30),             // 默认保留30天
		LogArchiveEnable: getEnvAsBool("LOG_ARCHIVE_ENABLE", false),         // 默认不启用归档
		LogCleanupEnable: getEnvAsBool("LOG_CLEANUP_ENABLE", true),          // 默认启用清理
		LogArchiveCron:   getEnv("LOG_ARCHIVE_CRON", "0 2 * * *"),           // 每天凌晨2点执行

		// CORS 配置
		AllowedOrigins: allowedOrigins,
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
