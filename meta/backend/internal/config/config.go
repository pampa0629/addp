package config

import (
	"log"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Meta 模块特有配置
	ServerPort        string
	DBSchema          string
	AutoSyncEnabled   bool
	AutoSyncSchedule  string // Cron expression
	AutoSyncLevel     string // database | table | field
	DeepScanTimeout   string
	DeepScanBatchSize int
}

func LoadConfig() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_SERVICE_URL", "http://localhost:8080")

	cfg := &Config{
		ServerPort:        commonConfig.GetEnv("SERVER_PORT", "8082"),
		DBSchema:          commonConfig.GetEnv("DB_SCHEMA", "metadata"),
		AutoSyncEnabled:   commonConfig.GetEnvBool("AUTO_SYNC_ENABLED", true),
		AutoSyncSchedule:  commonConfig.GetEnv("AUTO_SYNC_SCHEDULE", "0 0 * * *"), // Every day at midnight
		AutoSyncLevel:     commonConfig.GetEnv("AUTO_SYNC_LEVEL", "database"),
		DeepScanTimeout:   commonConfig.GetEnv("DEEP_SCAN_TIMEOUT", "30m"),
		DeepScanBatchSize: commonConfig.GetEnvInt("DEEP_SCAN_BATCH_SIZE", 10),
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

	return cfg
}
