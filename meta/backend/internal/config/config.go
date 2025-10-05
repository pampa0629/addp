package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type Config struct {
	ServerPort string

	// 从 System 获取的共享配置
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	JWTSecret  string

	// Meta 模块特有配置
	DBSchema          string
	SystemServiceURL  string
	EnableIntegration bool

	AutoSyncEnabled   bool
	AutoSyncSchedule  string // Cron expression
	AutoSyncLevel     string // database | table | field
	DeepScanTimeout   string
	DeepScanBatchSize int
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

func LoadConfig() *Config {
	systemURL := getEnv("SYSTEM_SERVICE_URL", "http://localhost:8080")

	cfg := &Config{
		ServerPort:        getEnv("SERVER_PORT", "8082"),
		DBSchema:          getEnv("DB_SCHEMA", "metadata"),
		SystemServiceURL:  systemURL,
		EnableIntegration: getEnv("ENABLE_SERVICE_INTEGRATION", "true") == "true",

		AutoSyncEnabled:   getEnv("AUTO_SYNC_ENABLED", "true") == "true",
		AutoSyncSchedule:  getEnv("AUTO_SYNC_SCHEDULE", "0 0 * * *"), // Every day at midnight
		AutoSyncLevel:     getEnv("AUTO_SYNC_LEVEL", "database"),
		DeepScanTimeout:   getEnv("DEEP_SCAN_TIMEOUT", "30m"),
		DeepScanBatchSize: getEnvInt("DEEP_SCAN_BATCH_SIZE", 10),
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
