package config

import (
	"log"
	"strconv"
	"strings"

	commonConfig "github.com/addp/common/config"
)

type VectorDBConfig = commonConfig.VectorDBConfig
type EmbeddingServiceConfig = commonConfig.EmbeddingServiceConfig

type Config struct {
	commonConfig.BaseConfig

	// Manager 模块特有配置
	Port                  string
	DBSchema              string
	PreviewPluginDir      string
	MetaServiceURL        string
	EnableMetaIntegration bool

	// Meilisearch 配置
	MeilisearchURL           string
	MeilisearchMasterKey     string
	MeilisearchDocumentIndex string
	MeilisearchAssetIndex    string

	// 向量数据库配置（PgVector）
	VectorDB VectorDBConfig

	// 在线向量化服务配置
	EmbeddingService EmbeddingServiceConfig

	// 向量检索阈值配置
	VectorSearchMaxDistance float64

	// Redis 配置（用于资源变更事件同步）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// MinIO 配置（用于 MVT 瓦片存储）
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool

	// MVT 预缓存配置
	PreCache PreCacheConfig
}

// PreCacheConfig MVT 预缓存相关配置
type PreCacheConfig struct {
	// 每瓦片目标记录数（用于计算 maxZoom）
	TargetRecordsPerTile int

	// 缓存策略阈值
	MinDurationForCacheMS int // 生成耗时阈值（毫秒）
	MinSizeForCacheKB     int // 瓦片大小阈值（KB）

	// 生成配置
	MaxZoom     int // 全局最大 zoom 层级
	Concurrency int // 并发协程数
}

func resolveMeilisearchURL() string {
	url := commonConfig.GetEnv("MEILISEARCH_URL", "")
	if url == "" {
		url = commonConfig.GetEnv("MEILISEARCH_URL_LOCAL", "")
	}
	return url
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_SERVICE_URL", "http://localhost:8080")
	metaURL := commonConfig.GetEnv("META_SERVICE_URL", "http://localhost:8082")

	rawPluginDir := commonConfig.GetEnv("PREVIEW_PLUGIN_DIR", "")
	builtinPluginDir := commonConfig.ResolveFromRoot("manager", "backend", "plugins")
	var pluginDirs []string
	if trimmed := strings.TrimSpace(rawPluginDir); trimmed != "" {
		pluginDirs = append(pluginDirs, trimmed)
	}
	pluginDirs = append(pluginDirs, builtinPluginDir)
	defaultPluginDir := strings.Join(pluginDirs, ",")

	cfg := &Config{
		Port:                  commonConfig.GetEnv("PORT", "8081"),
		DBSchema:              commonConfig.GetEnv("DB_SCHEMA", "manager"),
		PreviewPluginDir:      defaultPluginDir,
		MetaServiceURL:        metaURL,
		EnableMetaIntegration: commonConfig.GetEnvBool("ENABLE_META_INTEGRATION", true),
	}

	cfg.MeilisearchURL = resolveMeilisearchURL()
	cfg.MeilisearchMasterKey = commonConfig.GetEnv("MEILISEARCH_MASTER_KEY", "")
	cfg.MeilisearchDocumentIndex = commonConfig.GetEnv("MEILISEARCH_DOCUMENT_INDEX",
		commonConfig.GetEnv("META_DOCUMENT_INDEX", "asset-documents"))
	cfg.MeilisearchAssetIndex = commonConfig.GetEnv("MEILISEARCH_ASSET_INDEX", "metadata-assets")

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

	maxDistanceStr := strings.TrimSpace(commonConfig.GetEnv("VECTOR_SEARCH_MAX_DISTANCE", "0.35"))
	if maxDistanceStr == "" {
		maxDistanceStr = "0.35"
	}
	if val, err := strconv.ParseFloat(maxDistanceStr, 64); err != nil || val <= 0 {
		log.Printf("⚠️  Invalid VECTOR_SEARCH_MAX_DISTANCE=%q, fallback to 0.35", maxDistanceStr)
		cfg.VectorSearchMaxDistance = 0.35
	} else {
		cfg.VectorSearchMaxDistance = val
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

	// Redis 配置
	cfg.RedisHost = commonConfig.GetEnv("REDIS_HOST", "localhost")
	cfg.RedisPort = commonConfig.GetEnv("REDIS_PORT", "6379")
	cfg.RedisPassword = commonConfig.GetEnv("REDIS_PASSWORD", "")
	cfg.RedisDB = commonConfig.GetEnvInt("REDIS_DB", 0)

	// MinIO 配置（用于 MVT 瓦片存储）
	// 注意：Manager 模块使用系统 infra MinIO (9000)，不是业务 MinIO (9002)
	cfg.MinioEndpoint = commonConfig.GetEnv("MINIO_SYSTEM_ENDPOINT", commonConfig.GetEnv("MINIO_ENDPOINT", "localhost:9000"))
	cfg.MinioAccessKey = commonConfig.GetEnv("MINIO_SYSTEM_ACCESS_KEY", commonConfig.GetEnv("MINIO_ROOT_USER", commonConfig.GetEnv("MINIO_ACCESS_KEY", "minioadmin")))
	cfg.MinioSecretKey = commonConfig.GetEnv("MINIO_SYSTEM_SECRET_KEY", commonConfig.GetEnv("MINIO_ROOT_PASSWORD", commonConfig.GetEnv("MINIO_SECRET_KEY", "minioadmin")))
	cfg.MinioUseSSL = commonConfig.GetEnvBool("MINIO_USE_SSL", false)

	// MVT 预缓存配置
	cfg.PreCache = PreCacheConfig{
		TargetRecordsPerTile:  commonConfig.GetEnvInt("PRE_CACHE_TARGET_RECORDS", 5000),
		MinDurationForCacheMS: commonConfig.GetEnvInt("PRE_CACHE_MIN_DURATION_MS", 100),
		MinSizeForCacheKB:     commonConfig.GetEnvInt("PRE_CACHE_MIN_SIZE_KB", 50),
		MaxZoom:               commonConfig.GetEnvInt("PRE_CACHE_MAX_ZOOM", 18),
		Concurrency:           commonConfig.GetEnvInt("PRE_CACHE_CONCURRENCY", 10),
	}

	return cfg
}
