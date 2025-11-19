package config

import (
	"strings"
	"time"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
)

type VectorDBConfig = commonConfig.VectorDBConfig
type EmbeddingServiceConfig = commonConfig.EmbeddingServiceConfig

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

	// Elasticsearch 配置
	ElasticsearchURL              string
	ElasticsearchUsername         string
	ElasticsearchPassword         string
	ElasticsearchAPIKey           string
	ElasticsearchAssetIndex       string
	ElasticsearchDocumentIndex    string
	ElasticsearchDisableTLSVerify bool

	// 向量数据库配置（PgVector）
	VectorDB VectorDBConfig

	// 在线向量化服务配置
	EmbeddingService EmbeddingServiceConfig

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

func resolveElasticsearchURL() string {
	url := commonConfig.GetEnv("ELASTICSEARCH_URL", "")
	if url == "" {
		url = commonConfig.GetEnv("ELASTICSEARCH_URL_LOCAL", "")
	}
	return url
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

	cfg.ElasticsearchURL = resolveElasticsearchURL()
	cfg.ElasticsearchUsername = commonConfig.GetEnv("ELASTICSEARCH_USERNAME", "")
	cfg.ElasticsearchPassword = commonConfig.GetEnv("ELASTICSEARCH_PASSWORD", "")
	cfg.ElasticsearchAPIKey = commonConfig.GetEnv("ELASTICSEARCH_API_KEY", "")
	cfg.ElasticsearchAssetIndex = commonConfig.GetEnv("ELASTICSEARCH_INDEX", "metadata-assets")
	cfg.ElasticsearchDocumentIndex = commonConfig.GetEnv("META_DOCUMENT_INDEX", commonConfig.GetEnv("ASSET_DOCUMENT_INDEX", "asset-documents"))
	cfg.ElasticsearchDisableTLSVerify = commonConfig.GetEnvBool("ELASTICSEARCH_SKIP_VERIFY", false)

	// Redis 配置
	cfg.RedisHost = commonConfig.GetEnv("REDIS_HOST", "localhost")
	cfg.RedisPort = commonConfig.GetEnv("REDIS_PORT", "6379")
	cfg.RedisPassword = commonConfig.GetEnv("REDIS_PASSWORD", "")
	cfg.RedisDB = commonConfig.GetEnvInt("REDIS_DB", 0)

	// Worker 配置
	cfg.ConcurrentTasks = commonConfig.GetEnvInt("CONCURRENT_TASKS", 10)
	cfg.MaxRetries = commonConfig.GetEnvInt("MAX_RETRIES", 3)
	cfg.RetryDelay = commonConfig.GetEnvDuration("RETRY_DELAY", "30s")

	defaultHost := cfg.DBHost
	if defaultHost == "" {
		defaultHost = "localhost"
	}
	defaultPort := cfg.DBPort
	if defaultPort == "" {
		defaultPort = "5432"
	}
	defaultUser := cfg.DBUser
	if defaultUser == "" {
		defaultUser = "addp"
	}
	defaultPassword := cfg.DBPassword
	if defaultPassword == "" {
		defaultPassword = "addp_password"
	}

	cfg.VectorDB = VectorDBConfig{
		Host:      commonConfig.GetEnv("VECTOR_DB_HOST", defaultHost),
		Port:      commonConfig.GetEnv("VECTOR_DB_PORT", defaultPort),
		Name:      commonConfig.GetEnv("VECTOR_DB_NAME", "addp_vectors"),
		User:      commonConfig.GetEnv("VECTOR_DB_USER", defaultUser),
		Password:  commonConfig.GetEnv("VECTOR_DB_PASSWORD", defaultPassword),
		Schema:    commonConfig.GetEnv("VECTOR_DB_SCHEMA", "vector_store"),
		Table:     commonConfig.GetEnv("VECTOR_DB_TABLE", "document_embeddings"),
		Dimension: commonConfig.GetEnvInt("VECTOR_DB_DIMENSION", 1024),
		SSLMode:   commonConfig.GetEnv("VECTOR_DB_SSL_MODE", "disable"),
	}

	cfg.EmbeddingService = EmbeddingServiceConfig{
		BaseURL:    strings.TrimSpace(commonConfig.GetEnv("EMBEDDING_SERVICE_BASE_URL", "")),
		APIKey:     commonConfig.GetEnv("EMBEDDING_SERVICE_API_KEY", ""),
		TextModel:  commonConfig.GetEnv("EMBEDDING_MODEL_TEXT", "bge-large-zh"),
		ImageModel: commonConfig.GetEnv("EMBEDDING_MODEL_IMAGE", "clip-ViT-B-32"),
		AudioModel: commonConfig.GetEnv("EMBEDDING_MODEL_AUDIO", "whisper-small"),
		VideoModel: commonConfig.GetEnv("EMBEDDING_MODEL_VIDEO", "internvideo2-6b"),
		Timeout:    commonConfig.GetEnvDuration("EMBEDDING_SERVICE_TIMEOUT", "15s"),
	}

	return cfg
}
