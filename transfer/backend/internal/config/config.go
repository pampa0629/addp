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

type Config struct {
	Port string

	// 从 System 获取的共享配置
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	JWTSecret  string

	// Transfer 模块特有配置
	DBSchema          string
	SystemServiceURL  string
	EnableIntegration bool
	EncryptionKey     []byte

	// Transfer 特有配置
	RedisHost       string
	RedisPort       string
	RedisPassword   string
	WorkerCount     int
	MaxRetries      int
	RetryDelay      time.Duration
	TaskQueueName   string
	ConcurrentTasks int
}

// SharedConfig 从 System 获取的共享配置
type SharedConfig struct {
	JWTSecret     string `json:"jwt_secret"`
	EncryptionKey string `json:"encryption_key"`
	Database      struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		Name     string `json:"name"`
	} `json:"database"`
}

func Load() *Config {
	systemURL := getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080")

	cfg := &Config{
		Port:              getEnv("PORT", "8083"),
		DBSchema:          getEnv("DB_SCHEMA", "transfer"),
		SystemServiceURL:  systemURL,
		EnableIntegration: getEnv("ENABLE_SERVICE_INTEGRATION", "true") == "true",

		// Transfer 特有配置
		RedisHost:       getEnv("REDIS_HOST", "localhost"),
		RedisPort:       getEnv("REDIS_PORT", "6379"),
		RedisPassword:   getEnv("REDIS_PASSWORD", ""),
		WorkerCount:     getEnvInt("WORKER_COUNT", 5),
		MaxRetries:      getEnvInt("MAX_RETRIES", 3),
		RetryDelay:      getEnvDuration("RETRY_DELAY", "30s"),
		TaskQueueName:   getEnv("TASK_QUEUE_NAME", "transfer:tasks"),
		ConcurrentTasks: getEnvInt("CONCURRENT_TASKS", 10),
	}

	// 从 System 获取共享配置
	if cfg.EnableIntegration {
		log.Println("🔄 Attempting to load shared config from System service...")
		if err := cfg.loadSharedConfig(systemURL); err != nil {
			log.Printf("⚠️  Warning: Failed to load shared config from System: %v", err)
			log.Printf("⚠️  Falling back to local environment variables...")
			cfg.loadLocalConfig()
		} else {
			log.Println("✅ Successfully loaded shared config from System service")
		}
	} else {
		log.Println("ℹ️  Service integration disabled, using local config")
		cfg.loadLocalConfig()
	}

	return cfg
}

// loadSharedConfig 从 System 服务获取共享配置
func (c *Config) loadSharedConfig(systemURL string) error {
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
	c.JWTSecret = shared.JWTSecret
	c.DBHost = shared.Database.Host
	c.DBPort = shared.Database.Port
	c.DBUser = shared.Database.User
	c.DBPassword = shared.Database.Password
	c.DBName = shared.Database.Name

	// 解析加密密钥
	if shared.EncryptionKey != "" {
		key, err := base64.StdEncoding.DecodeString(shared.EncryptionKey)
		if err == nil && len(key) == 32 {
			c.EncryptionKey = key
		}
	}

	// 如果没有从 System 获取到加密密钥，使用本地加载
	if c.EncryptionKey == nil {
		c.EncryptionKey = loadEncryptionKey()
	}

	return nil
}

// loadLocalConfig 从本地环境变量加载配置（降级方案）
func (c *Config) loadLocalConfig() {
	c.JWTSecret = getEnv("JWT_SECRET", "")
	c.DBHost = getEnv("DB_HOST", "localhost")
	c.DBPort = getEnv("DB_PORT", "5432")
	c.DBUser = getEnv("DB_USER", "addp")
	c.DBPassword = getEnv("DB_PASSWORD", "addp_password")
	c.DBName = getEnv("DB_NAME", "addp")
	c.EncryptionKey = loadEncryptionKey()

	if c.JWTSecret == "" {
		log.Println("⚠️  WARNING: JWT_SECRET is not set! Authentication will fail!")
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
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

func getEnvDuration(key string, defaultValue string) time.Duration {
	value := getEnv(key, defaultValue)
	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("Invalid duration value for %s: %s, using default: %s", key, value, defaultValue)
		duration, _ = time.ParseDuration(defaultValue)
	}
	return duration
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
