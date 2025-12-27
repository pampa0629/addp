package config

import (
	"log"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Service 模块特有配置
	Port     string
	DBSchema string

	// 模块集成配置
	ManagerServiceURL string
	MetaServiceURL    string

	// Redis 配置
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// 定时任务配置
	HealthCheckCron     string // 健康检查 Cron 表达式
	MetadataRefreshCron string // 元数据刷新 Cron 表达式
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_SERVICE_URL", "http://localhost:8080")
	managerURL := commonConfig.GetEnv("MANAGER_SERVICE_URL", "http://localhost:8081")
	metaURL := commonConfig.GetEnv("META_SERVICE_URL", "http://localhost:8082")

	cfg := &Config{
		Port:              commonConfig.GetEnv("PORT", "8086"),
		DBSchema:          commonConfig.GetEnv("DB_SCHEMA", "service"),
		ManagerServiceURL: managerURL,
		MetaServiceURL:    metaURL,
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

	// Redis 配置
	cfg.RedisHost = commonConfig.GetEnv("REDIS_HOST", "localhost")
	cfg.RedisPort = commonConfig.GetEnv("REDIS_PORT", "6379")
	cfg.RedisPassword = commonConfig.GetEnv("REDIS_PASSWORD", "")
	cfg.RedisDB = commonConfig.GetEnvInt("REDIS_DB", 0)

	// 定时任务配置
	cfg.HealthCheckCron = commonConfig.GetEnv("SERVICE_HEALTH_CHECK_CRON", "0 * * * *")       // 默认每小时
	cfg.MetadataRefreshCron = commonConfig.GetEnv("SERVICE_METADATA_REFRESH_CRON", "0 2 * * *") // 默认每天凌晨2点

	return cfg
}
