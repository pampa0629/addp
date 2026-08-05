package config

import (
	"time"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Meta 模块特有配置
	ServerPort          string
	DBSchema            string
	ServiceClientSecret string
	DeepScanTimeout     string
	DeepScanBatchSize   int

	// Meilisearch 配置
	MeilisearchURL        string
	MeilisearchMasterKey  string
	MeilisearchAssetIndex string

	// Redis 配置（用于资源变更事件同步和任务队列）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Worker 配置
	ConcurrentTasks int
	MaxRetries      int
	RetryDelay      time.Duration

	// 平台内置 MinIO（用于 no-persist inspect 等内部读取）
	BuiltinMinioEndpoint  string
	BuiltinMinioAccessKey string
	BuiltinMinioSecretKey string
	BuiltinMinioUseSSL    bool
}

func resolveMeilisearchURL() string {
	return commonConfig.GetEnv("MEILISEARCH_URL", "")
}

func LoadConfig() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180")

	cfg := &Config{
		ServerPort:          commonConfig.GetEnv("META_BACKEND_PORT", "8082"),
		DBSchema:            commonConfig.GetEnv("DB_SCHEMA", "meta"),
		ServiceClientSecret: commonConfig.GetEnv("META_SERVICE_CLIENT_SECRET", ""),
		DeepScanTimeout:     commonConfig.GetEnv("DEEP_SCAN_TIMEOUT", "30m"),
		DeepScanBatchSize:   commonConfig.GetEnvInt("DEEP_SCAN_BATCH_SIZE", 10),
	}

	// Meta 不再调用使用 Internal API Key 的共享配置接口。部署配置来自
	// 环境，System 业务事实只通过 Service Access Token 读取。
	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)
	cfg.SystemServiceURL = systemURL

	cfg.MeilisearchURL = resolveMeilisearchURL()
	cfg.MeilisearchMasterKey = commonConfig.GetEnv("MEILISEARCH_MASTER_KEY", "")
	cfg.MeilisearchAssetIndex = commonConfig.GetEnv("MEILISEARCH_ASSET_INDEX", "assets")

	// Redis 配置
	cfg.RedisHost = commonConfig.GetEnv("REDIS_HOST", "localhost")
	cfg.RedisPort = commonConfig.GetEnv("REDIS_PORT", "6379")
	cfg.RedisPassword = commonConfig.GetEnv("REDIS_PASSWORD", "")
	cfg.RedisDB = commonConfig.GetEnvInt("REDIS_DB", 0)

	// Worker 配置
	cfg.ConcurrentTasks = commonConfig.GetEnvInt("CONCURRENT_TASKS", 10)
	cfg.MaxRetries = commonConfig.GetEnvInt("MAX_RETRIES", 3)
	cfg.RetryDelay = commonConfig.GetEnvDuration("RETRY_DELAY", "30s")

	minioCfg := commonConfig.LoadBuiltinMinIOConfig()
	cfg.BuiltinMinioEndpoint = minioCfg.Endpoint
	cfg.BuiltinMinioAccessKey = minioCfg.AccessKey
	cfg.BuiltinMinioSecretKey = minioCfg.SecretKey
	cfg.BuiltinMinioUseSSL = minioCfg.UseSSL

	return cfg
}
