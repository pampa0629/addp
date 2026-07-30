package config

import (
	"fmt"
	"log"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	commonConfig "github.com/addp/common/config"
	"github.com/joho/godotenv"
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
	// 加载 .env 文件（从项目根目录）
	if err := godotenv.Load("../../.env"); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

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
	if value := os.Getenv("MONITOR_ALERT_EVALUATION_INTERVAL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid MONITOR_ALERT_EVALUATION_INTERVAL")
		}
		alertEvaluationInterval = parsed
	}
	webhookDispatchInterval, err := durationEnv("MONITOR_WEBHOOK_DISPATCH_INTERVAL", 2*time.Second)
	if err != nil {
		return nil, err
	}
	webhookHTTPTimeout, err := durationEnv("MONITOR_WEBHOOK_HTTP_TIMEOUT", 10*time.Second)
	if err != nil {
		return nil, err
	}
	webhookLeaseDuration, err := durationEnv("MONITOR_WEBHOOK_LEASE_DURATION", 30*time.Second)
	if err != nil {
		return nil, err
	}
	if webhookLeaseDuration <= webhookHTTPTimeout {
		return nil, fmt.Errorf("MONITOR_WEBHOOK_LEASE_DURATION must be greater than MONITOR_WEBHOOK_HTTP_TIMEOUT")
	}
	webhookRetryInitial, err := durationEnv("MONITOR_WEBHOOK_RETRY_INITIAL_BACKOFF", 5*time.Second)
	if err != nil {
		return nil, err
	}
	webhookRetryMax, err := durationEnv("MONITOR_WEBHOOK_RETRY_MAX_BACKOFF", 5*time.Minute)
	if err != nil {
		return nil, err
	}
	if webhookRetryInitial > webhookRetryMax {
		return nil, fmt.Errorf("MONITOR_WEBHOOK_RETRY_INITIAL_BACKOFF must not exceed MONITOR_WEBHOOK_RETRY_MAX_BACKOFF")
	}
	webhookMaxAttempts, err := positiveIntEnv("MONITOR_WEBHOOK_MAX_ATTEMPTS", 8)
	if err != nil {
		return nil, err
	}
	webhookAllowPrivate, err := boolEnv("MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS", false)
	if err != nil {
		return nil, err
	}
	consoleBaseURL := getEnvOrDefault("MONITOR_CONSOLE_BASE_URL", "http://localhost:5170")
	parsedConsoleURL, err := url.Parse(consoleBaseURL)
	if err != nil || parsedConsoleURL.Host == "" || (parsedConsoleURL.Scheme != "http" && parsedConsoleURL.Scheme != "https") {
		return nil, fmt.Errorf("invalid MONITOR_CONSOLE_BASE_URL")
	}
	emailDispatchInterval, err := durationEnv("MONITOR_EMAIL_DISPATCH_INTERVAL", 2*time.Second)
	if err != nil {
		return nil, err
	}
	emailSMTPTimeout, err := durationEnv("MONITOR_EMAIL_SMTP_TIMEOUT", 15*time.Second)
	if err != nil {
		return nil, err
	}
	emailLeaseDuration, err := durationEnv("MONITOR_EMAIL_LEASE_DURATION", 30*time.Second)
	if err != nil {
		return nil, err
	}
	if emailLeaseDuration <= emailSMTPTimeout {
		return nil, fmt.Errorf("MONITOR_EMAIL_LEASE_DURATION must be greater than MONITOR_EMAIL_SMTP_TIMEOUT")
	}
	emailRetryInitial, err := durationEnv("MONITOR_EMAIL_RETRY_INITIAL_BACKOFF", 5*time.Second)
	if err != nil {
		return nil, err
	}
	emailRetryMax, err := durationEnv("MONITOR_EMAIL_RETRY_MAX_BACKOFF", 5*time.Minute)
	if err != nil {
		return nil, err
	}
	if emailRetryInitial > emailRetryMax {
		return nil, fmt.Errorf("MONITOR_EMAIL_RETRY_INITIAL_BACKOFF must not exceed MONITOR_EMAIL_RETRY_MAX_BACKOFF")
	}
	emailMaxAttempts, err := positiveIntEnv("MONITOR_EMAIL_MAX_ATTEMPTS", 8)
	if err != nil {
		return nil, err
	}
	emailSMTPPort, err := positiveIntEnv("MONITOR_EMAIL_SMTP_PORT", 587)
	if err != nil || emailSMTPPort > 65535 {
		return nil, fmt.Errorf("invalid MONITOR_EMAIL_SMTP_PORT")
	}
	emailSMTPHost := strings.TrimSpace(os.Getenv("MONITOR_EMAIL_SMTP_HOST"))
	emailSMTPUsername := os.Getenv("MONITOR_EMAIL_SMTP_USERNAME")
	emailSMTPPassword := os.Getenv("MONITOR_EMAIL_SMTP_PASSWORD")
	if (emailSMTPUsername == "") != (emailSMTPPassword == "") {
		return nil, fmt.Errorf("MONITOR_EMAIL_SMTP_USERNAME and MONITOR_EMAIL_SMTP_PASSWORD must be configured together")
	}
	emailSMTPTLSMode := getEnvOrDefault("MONITOR_EMAIL_SMTP_TLS_MODE", "starttls")
	if emailSMTPTLSMode != "starttls" && emailSMTPTLSMode != "tls" {
		return nil, fmt.Errorf("MONITOR_EMAIL_SMTP_TLS_MODE must be starttls or tls")
	}
	emailFromAddress := strings.TrimSpace(os.Getenv("MONITOR_EMAIL_FROM_ADDRESS"))
	if emailSMTPHost != "" {
		address, parseErr := mail.ParseAddress(emailFromAddress)
		if parseErr != nil || address.Name != "" || address.Address != emailFromAddress {
			return nil, fmt.Errorf("invalid MONITOR_EMAIL_FROM_ADDRESS")
		}
	}

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
		EmailSMTPHost:           emailSMTPHost,
		EmailSMTPPort:           emailSMTPPort,
		EmailSMTPUsername:       emailSMTPUsername,
		EmailSMTPPassword:       emailSMTPPassword,
		EmailSMTPTLSMode:        emailSMTPTLSMode,
		EmailFromAddress:        emailFromAddress,
		EmailFromName:           getEnvOrDefault("MONITOR_EMAIL_FROM_NAME", "ADDP Monitor"),
	}

	return cfg, nil
}

func (c *Config) EmailSMTPConfigured() bool {
	return c.EmailSMTPHost != ""
}

func durationEnv(key string, defaultValue time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
}

func positiveIntEnv(key string, defaultValue int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
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
