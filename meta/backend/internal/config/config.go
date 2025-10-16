package config

import (
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
)

type Config struct {
	commonConfig.BaseConfig

	// Meta 模块特有配置
	ServerPort        string
	DBSchema          string
	InternalAPIKey    string // 服务间调用的 API Key
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
		InternalAPIKey:    commonConfig.GetEnv("INTERNAL_API_KEY", ""),
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
		logger.L().Info("尝试从 System 服务拉取共享配置")
		if err := commonConfig.LoadSharedConfig(systemURL, &cfg.BaseConfig); err != nil {
			logger.L().Warn("从 System 拉取共享配置失败，回退至本地环境变量", "error", err)
			commonConfig.LoadLocalConfig(&cfg.BaseConfig)
		} else {
			logger.L().Info("成功加载 System 共享配置")
		}
	} else {
		logger.L().Info("已禁用服务集成，使用本地配置")
		commonConfig.LoadLocalConfig(&cfg.BaseConfig)
	}

	if cfg.InternalAPIKey == "" {
		cfg.InternalAPIKey = cfg.BaseConfig.InternalAPIKey
	}

	return cfg
}
