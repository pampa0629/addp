package config

import (
	"log"
	"time"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Transfer 模块特有配置
	Port            string
	DBSchema        string
	InternalAPIKey  string        // 服务间调用的 API Key
	MetaServiceURL  string        // Meta 服务地址
	RedisHost       string
	RedisPort       string
	RedisPassword   string
	WorkerCount     int
	MaxRetries      int
	RetryDelay      time.Duration
	TaskQueueName   string
	ConcurrentTasks int
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_SERVICE_URL", "http://localhost:8180")
	metaURL := commonConfig.GetEnv("META_SERVICE_URL", "http://localhost:8082")

	cfg := &Config{
		Port:            commonConfig.GetEnv("PORT", "8083"),
		DBSchema:        commonConfig.GetEnv("DB_SCHEMA", "transfer"),
		InternalAPIKey:  commonConfig.GetEnv("INTERNAL_API_KEY", ""),
		MetaServiceURL:  metaURL,
		RedisHost:       commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:       commonConfig.GetEnv("REDIS_PORT", "6379"),
		RedisPassword:   commonConfig.GetEnv("REDIS_PASSWORD", ""),
		WorkerCount:     commonConfig.GetEnvInt("WORKER_COUNT", 5),
		MaxRetries:      commonConfig.GetEnvInt("MAX_RETRIES", 3),
		RetryDelay:      commonConfig.GetEnvDuration("RETRY_DELAY", "30s"),
		TaskQueueName:   commonConfig.GetEnv("TASK_QUEUE_NAME", "transfer:tasks"),
		ConcurrentTasks: commonConfig.GetEnvInt("CONCURRENT_TASKS", 10),
	}

	// 设置 BaseConfig 字段
	cfg.SystemServiceURL = systemURL
	cfg.EnableIntegration = commonConfig.GetEnvBool("ENABLE_SERVICE_INTEGRATION", true)

	// 从 System 获取共享配置
	if cfg.EnableIntegration {
		log.Println("🔄 Attempting to load shared config from System service...")
		if err := commonConfig.LoadSharedConfig(systemURL, &cfg.BaseConfig); err != nil {
			log.Printf("⚠️  Warning: Failed to load shared config from System: %v", err)
			log.Printf("⚠️  Falling back to local environment variables...")
			commonConfig.LoadLocalConfig(&cfg.BaseConfig)
		} else {
			log.Println("✅ Successfully loaded shared config from System service")
		}
	} else {
		log.Println("ℹ️  Service integration disabled, using local config")
		commonConfig.LoadLocalConfig(&cfg.BaseConfig)
	}

	// 如果本地未配置 InternalAPIKey，尝试从 BaseConfig 获取
	if cfg.InternalAPIKey == "" {
		cfg.InternalAPIKey = cfg.BaseConfig.InternalAPIKey
	}

	return cfg
}
