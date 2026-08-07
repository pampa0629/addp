package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	// 服务配置
	ServerPort string

	// 数据库配置
	DatabaseHost     string
	DatabasePort     string
	DatabaseUser     string
	DatabasePassword string
	DatabaseName     string
	DatabaseSSLMode  string

	// Redis 配置
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// System 模块配置（用于健康检查）
	SystemURL               string
	InternalAPIKey          string
	ServiceClientSecret     string
	AlertEvaluationInterval time.Duration
	EncryptionKey           []byte
	WebhookDispatchInterval time.Duration
	WebhookHTTPTimeout      time.Duration
	WebhookLeaseDuration    time.Duration
	WebhookMaxAttempts      int
	WebhookRetryInitial     time.Duration
	WebhookRetryMax         time.Duration
	WebhookAllowPrivate     bool
	ConsoleBaseURL          string
	EmailDispatchInterval   time.Duration
	EmailSMTPTimeout        time.Duration
	EmailLeaseDuration      time.Duration
	EmailMaxAttempts        int
	EmailRetryInitial       time.Duration
	EmailRetryMax           time.Duration
	EmailSMTPHost           string
	EmailSMTPPort           int
	EmailSMTPUsername       string
	EmailSMTPPassword       string
	EmailSMTPTLSMode        string
	EmailFromAddress        string
	EmailFromName           string
}

// LoadConfig 加载配置
func LoadConfig() (*Config, error) {
	// Redis DB 转换
	redisDB := 0
	if redisDBStr := os.Getenv("REDIS_DB"); redisDBStr != "" {
		var err error
		redisDB, err = strconv.Atoi(redisDBStr)
		if err != nil {
			return nil, fmt.Errorf("invalid REDIS_DB: %w", err)
		}
	}
	alertEvaluationInterval := 15 * time.Second
	webhookDispatchInterval := 2 * time.Second
	webhookHTTPTimeout := 10 * time.Second
	webhookLeaseDuration := 30 * time.Second
	webhookRetryInitial := 5 * time.Second
	webhookRetryMax := 5 * time.Minute
	webhookMaxAttempts := 8
	webhookAllowPrivate, err := boolEnv("MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS", false)
	if err != nil {
		return nil, err
	}
	consoleBaseURL := getEnvOrDefault("MONITOR_CONSOLE_BASE_URL", "http://localhost:5170")
	parsedConsoleURL, err := url.Parse(consoleBaseURL)
	if err != nil || parsedConsoleURL.Host == "" || (parsedConsoleURL.Scheme != "http" && parsedConsoleURL.Scheme != "https") {
		return nil, fmt.Errorf("invalid MONITOR_CONSOLE_BASE_URL")
	}
	emailDispatchInterval := 2 * time.Second
	emailSMTPTimeout := 15 * time.Second
	emailLeaseDuration := 30 * time.Second
	emailRetryInitial := 5 * time.Second
	emailRetryMax := 5 * time.Minute
	emailMaxAttempts := 8

	cfg := &Config{
		ServerPort: getEnvOrDefault("MONITOR_BACKEND_PORT", "8100"),

		DatabaseHost:     getEnvOrDefault("POSTGRES_HOST", "localhost"),
		DatabasePort:     getEnvOrDefault("POSTGRES_PORT", "15432"),
		DatabaseUser:     getEnvOrDefault("POSTGRES_USER", "addp"),
		DatabasePassword: getEnvOrDefault("POSTGRES_PASSWORD", "addp123"),
		DatabaseName:     getEnvOrDefault("POSTGRES_DB", "addp"),
		DatabaseSSLMode:  getEnvOrDefault("POSTGRES_SSLMODE", "disable"),

		RedisHost:     getEnvOrDefault("REDIS_HOST", "localhost"),
		RedisPort:     getEnvOrDefault("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       redisDB,

		SystemURL:               getEnvOrDefault("SYSTEM_URL", "http://localhost:8180"),
		InternalAPIKey:          os.Getenv("INTERNAL_API_KEY"),
		ServiceClientSecret:     os.Getenv("MONITOR_SERVICE_CLIENT_SECRET"),
		AlertEvaluationInterval: alertEvaluationInterval,
		EncryptionKey:           commonConfig.LoadEncryptionKey(),
		WebhookDispatchInterval: webhookDispatchInterval,
		WebhookHTTPTimeout:      webhookHTTPTimeout,
		WebhookLeaseDuration:    webhookLeaseDuration,
		WebhookMaxAttempts:      webhookMaxAttempts,
		WebhookRetryInitial:     webhookRetryInitial,
		WebhookRetryMax:         webhookRetryMax,
		WebhookAllowPrivate:     webhookAllowPrivate,
		ConsoleBaseURL:          parsedConsoleURL.String(),
		EmailDispatchInterval:   emailDispatchInterval,
		EmailSMTPTimeout:        emailSMTPTimeout,
		EmailLeaseDuration:      emailLeaseDuration,
		EmailMaxAttempts:        emailMaxAttempts,
		EmailRetryInitial:       emailRetryInitial,
		EmailRetryMax:           emailRetryMax,
		EmailSMTPHost:           "",
		EmailSMTPPort:           587,
		EmailSMTPTLSMode:        "starttls",
		EmailFromName:           "ADDP Monitor",
	}

	return cfg, nil
}

func (c *Config) EmailSMTPConfigured() bool {
	return c.EmailSMTPHost != ""
}

func boolEnv(key string, defaultValue bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
}

// GetDatabaseDSN 获取数据库连接字符串
func (c *Config) GetDatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DatabaseHost,
		c.DatabasePort,
		c.DatabaseUser,
		c.DatabasePassword,
		c.DatabaseName,
		c.DatabaseSSLMode,
	)
}

// getEnvOrDefault 获取环境变量或默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
