package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Env                         string
	ServerAddr                  string
	DatabaseURL                 string
	EncryptionKey               []byte
	OAuthUserCodePepper         []byte
	OAuthPreviousUserCodePepper []byte
	IAMMFAEncryptionKey         []byte
	PublicAPIURL                string
	ConsoleURL                  string
	ProjectName                 string
	ServiceClientSecrets        map[string]string

	// PostgreSQL 配置（用于其他模块）
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string

	// Redis 配置（用于事件通知）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Infra MinIO 配置（系统文件存储，不是业务引擎）
	InfraMinIOEndpoint  string
	InfraMinIOAccessKey string
	InfraMinIOSecretKey string
	InfraMinIOBucket    string
	InfraMinIOUseSSL    bool

	// 内置模块服务 URL。
	SystemServiceURL       string
	MetaServiceURL         string
	TransferServiceURL     string
	ManagerServiceURL      string
	OrchestratorServiceURL string
	DevelopServiceURL      string

	// CORS 配置
	AllowedOrigins []string // CORS 白名单
	TrustedProxies []string // 允许提供客户端 IP 转发头的反向代理 IP/CIDR
}

func (c *Config) PostgreSQLDSN() string {
	endpoint := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.PostgresUser, c.PostgresPassword),
		Host:   net.JoinHostPort(c.PostgresHost, c.PostgresPort),
		Path:   "/" + c.PostgresDB,
	}
	query := endpoint.Query()
	query.Set("sslmode", "disable")
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

func (c *Config) ValidateTrustedProxies() error {
	for _, proxy := range c.TrustedProxies {
		switch strings.TrimSpace(proxy) {
		case "*", "0.0.0.0/0", "::/0":
			return errors.New("TRUSTED_PROXIES 不得信任全网段")
		}
	}
	return nil
}

func Load() *Config {
	// 加载环境配置
	env := getEnv("ENV", "development")

	// 加载加密密钥
	encryptionKey := loadEncryptionKey()

	// 加载 CORS 白名单
	allowedOriginsStr := getEnv("ALLOWED_ORIGINS", "http://localhost:5170,http://localhost:5173")
	allowedOrigins := strings.Split(allowedOriginsStr, ",")
	// 去除空白字符
	for i, origin := range allowedOrigins {
		allowedOrigins[i] = strings.TrimSpace(origin)
	}
	log.Printf("✅ CORS AllowedOrigins: %v", allowedOrigins)
	trustedProxies := splitAndTrim(getEnv("TRUSTED_PROXIES", "127.0.0.1,::1"))

	// 读取端口配置（统一使用 {MODULE}_BACKEND_PORT 格式）
	port := getEnv("SYSTEM_BACKEND_PORT", "8180")
	serverAddr := ":" + port

	return &Config{
		Env:                         env,
		ServerAddr:                  serverAddr,
		DatabaseURL:                 "", // PostgreSQL 不使用此字段
		EncryptionKey:               encryptionKey,
		OAuthUserCodePepper:         loadOAuthUserCodePepper(env, "OAUTH_USER_CODE_PEPPER", true),
		OAuthPreviousUserCodePepper: loadOAuthUserCodePepper(env, "OAUTH_PREVIOUS_USER_CODE_PEPPER", false),
		IAMMFAEncryptionKey:         loadIAMMFAEncryptionKey(env),
		PublicAPIURL:                strings.TrimSuffix(getEnv("PUBLIC_API_URL", "http://localhost:8000"), "/"),
		ConsoleURL:                  strings.TrimSuffix(getEnv("CONSOLE_URL", "http://localhost:5170"), "/"),
		ProjectName:                 getEnv("PROJECT_NAME", "全域数据平台"),
		ServiceClientSecrets: map[string]string{
			"addp-agent":        getEnv("AGENT_SERVICE_CLIENT_SECRET", ""),
			"addp-asset":        getEnv("ASSET_SERVICE_CLIENT_SECRET", ""),
			"addp-catalog":      getEnv("CATALOG_SERVICE_CLIENT_SECRET", ""),
			"addp-workbench":    getEnv("WORKBENCH_SERVICE_CLIENT_SECRET", ""),
			"addp-copilot":      getEnv("COPILOT_SERVICE_CLIENT_SECRET", ""),
			"addp-develop":      getEnv("DEVELOP_SERVICE_CLIENT_SECRET", ""),
			"addp-duckdb":       getEnv("DUCKDB_SERVICE_CLIENT_SECRET", ""),
			"addp-gateway":      getEnv("GATEWAY_SERVICE_CLIENT_SECRET", ""),
			"addp-graph":        getEnv("GRAPH_SERVICE_CLIENT_SECRET", ""),
			"addp-geopython":    getEnv("GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET", ""),
			"addp-manager":      getEnv("MANAGER_SERVICE_CLIENT_SECRET", ""),
			"addp-inference":    getEnv("INFERENCE_SERVICE_CLIENT_SECRET", ""),
			"addp-meta":         getEnv("META_SERVICE_CLIENT_SECRET", ""),
			"addp-model":        getEnv("MODEL_SERVICE_CLIENT_SECRET", ""),
			"addp-model3d":      getEnv("MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET", ""),
			"addp-monitor":      getEnv("MONITOR_SERVICE_CLIENT_SECRET", ""),
			"addp-orchestrator": getEnv("ORCHESTRATOR_SERVICE_CLIENT_SECRET", ""),
			"addp-portal":       getEnv("PORTAL_SERVICE_CLIENT_SECRET", ""),
			"addp-quality":      getEnv("QUALITY_SERVICE_CLIENT_SECRET", ""),
			"addp-security":     getEnv("SECURITY_SERVICE_CLIENT_SECRET", ""),
			"addp-pointcloud":   getEnv("POINTCLOUD_WORKFLOW_SERVICE_CLIENT_SECRET", ""),
			"addp-service":      getEnv("SERVICE_SERVICE_CLIENT_SECRET", ""),
			"addp-standard":     getEnv("STANDARD_SERVICE_CLIENT_SECRET", ""),
			"addp-spark":        getEnv("SPARK_WORKFLOW_SERVICE_CLIENT_SECRET", ""),
			"addp-transfer":     getEnv("TRANSFER_SERVICE_CLIENT_SECRET", ""),
		},

		// PostgreSQL 配置
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:     getEnv("POSTGRES_USER", "addp"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "addp_password"),
		PostgresDB:       getEnv("POSTGRES_DB", "addp"),

		// Redis 配置
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvAsInt("REDIS_DB", 0),

		// Infra MinIO 配置（系统文件存储，不是业务引擎）
		InfraMinIOEndpoint:  getInfraMinIOEndpoint(),
		InfraMinIOAccessKey: getEnv("MINIO_ROOT_USER", "minioadmin"),
		InfraMinIOSecretKey: getEnv("MINIO_ROOT_PASSWORD", "minioadmin"),
		InfraMinIOBucket:    getEnv("MINIO_BUCKET", "system"),
		InfraMinIOUseSSL:    getEnvAsBool("MINIO_USE_SSL", false),

		// 内置引擎服务 URL
		SystemServiceURL:       getEnv("SYSTEM_URL", "http://localhost:8180"),
		MetaServiceURL:         getEnv("META_URL", "http://localhost:8082"),
		TransferServiceURL:     getEnv("TRANSFER_URL", "http://localhost:8083"),
		ManagerServiceURL:      getEnv("MANAGER_URL", "http://localhost:8081"),
		OrchestratorServiceURL: getEnv("ORCHESTRATOR_URL", "http://localhost:8084"),
		DevelopServiceURL:      getEnv("DEVELOP_URL", "http://localhost:8185"),

		// CORS 配置
		AllowedOrigins: allowedOrigins,
		TrustedProxies: trustedProxies,
	}
}

func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
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

func getInfraMinIOEndpoint() string {
	if endpoint := getEnv("MINIO_ENDPOINT", ""); endpoint != "" {
		return endpoint
	}
	port := getEnv("MINIO_API_PORT", "19000")
	return "localhost:" + port
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

func loadOAuthUserCodePepper(env, name string, required bool) []byte {
	encoded := strings.TrimSpace(os.Getenv(name))
	if encoded == "" {
		if !required {
			return nil
		}
		if env == "production" {
			log.Fatalf("%s must be set in production", name)
		}
		log.Printf("WARNING: %s not set, using an isolated development-only pepper", name)
		return []byte("0123456789abcdef0123456789abcdef")
	}
	pepper, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Fatalf("Failed to decode %s: %v", name, err)
	}
	if len(pepper) != 32 {
		log.Fatalf("%s must be 32 bytes, got %d bytes", name, len(pepper))
	}
	return pepper
}

func loadIAMMFAEncryptionKey(env string) []byte {
	encoded := strings.TrimSpace(os.Getenv("IAM_MFA_ENCRYPTION_KEY"))
	if encoded == "" {
		if env == "production" {
			log.Fatal("IAM_MFA_ENCRYPTION_KEY must be set in production")
		}
		log.Print("WARNING: IAM_MFA_ENCRYPTION_KEY not set, using an isolated development-only key")
		return []byte("mfa-dev-0123456789abcdef01234567")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Fatalf("Failed to decode IAM_MFA_ENCRYPTION_KEY: %v", err)
	}
	if len(key) != 32 {
		log.Fatalf("IAM_MFA_ENCRYPTION_KEY must be 32 bytes, got %d bytes", len(key))
	}
	return key
}
