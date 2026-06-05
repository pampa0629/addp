package config

import (
	"time"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
)

type Config struct {
	commonConfig.BaseConfig

	// Meta 模块特有配置
	ServerPort        string
	DBSchema          string
	InternalAPIKey    string // 服务间调用的 API Key
	DeepScanTimeout   string
	DeepScanBatchSize int

	// Meilisearch 配置
	MeilisearchURL        string
	MeilisearchMasterKey  string
	MeilisearchAssetIndex string

	// MinIO 配置（Infra MinIO，用于清理）
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool

	// Redis 配置（用于资源变更事件同步和任务队列）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Worker 配置
	ConcurrentTasks int
	MaxRetries      int
	RetryDelay      time.Duration
}

func resolveMeilisearchURL() string {
	return commonConfig.GetEnv("MEILISEARCH_URL", "")
}

func LoadConfig() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180")

	cfg := &Config{
		ServerPort:        commonConfig.GetEnv("META_BACKEND_PORT", "8082"),
		DBSchema:          commonConfig.GetEnv("DB_SCHEMA", "meta"),
		InternalAPIKey:    commonConfig.GetEnv("INTERNAL_API_KEY", ""),
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

	cfg.MeilisearchURL = resolveMeilisearchURL()
	cfg.MeilisearchMasterKey = commonConfig.GetEnv("MEILISEARCH_MASTER_KEY", "")
	cfg.MeilisearchAssetIndex = commonConfig.GetEnv("MEILISEARCH_ASSET_INDEX", "assets")

	// MinIO 配置（Infra MinIO）
	cfg.MinioEndpoint = commonConfig.GetEnv("META_MINIO_ENDPOINT", "localhost:19000")
	cfg.MinioAccessKey = commonConfig.GetEnv("META_MINIO_ACCESS_KEY", "minioadmin")
	cfg.MinioSecretKey = commonConfig.GetEnv("META_MINIO_SECRET_KEY", "minioadmin")
	cfg.MinioUseSSL = commonConfig.GetEnvBool("META_MINIO_USE_SSL", false)

	// Redis 配置
	cfg.RedisHost = commonConfig.GetEnv("REDIS_HOST", "localhost")
	cfg.RedisPort = commonConfig.GetEnv("REDIS_PORT", "6379")
	cfg.RedisPassword = commonConfig.GetEnv("REDIS_PASSWORD", "")
	cfg.RedisDB = commonConfig.GetEnvInt("REDIS_DB", 0)

	// Worker 配置
	cfg.ConcurrentTasks = commonConfig.GetEnvInt("CONCURRENT_TASKS", 10)
	cfg.MaxRetries = commonConfig.GetEnvInt("MAX_RETRIES", 3)
	cfg.RetryDelay = commonConfig.GetEnvDuration("RETRY_DELAY", "30s")

	return cfg
}
