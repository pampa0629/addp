package config

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	commonConfig "github.com/addp/common/config"
)

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

	// Manager 向量化配置（按需触发）
	EmbeddingService struct {
		BaseURL string
		APIKey  string
		Timeout time.Duration
		Models  map[string]string // text, image, video
	}

	VectorConfig struct {
		Dimension        int     // 向量维度
		MaxDistance      float64 // 搜索相似度阈值（余弦距离）
		MaxFileSizeMB    int     // 向量化最大文件大小
		BatchConcurrency int     // 批量向量化并发数
	}

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
	cfg.MeilisearchDocumentIndex = commonConfig.GetEnv("MEILISEARCH_DOCUMENT_INDEX", "meta-documents")
	cfg.MeilisearchAssetIndex = commonConfig.GetEnv("MEILISEARCH_ASSET_INDEX", "meta-assets")

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

	// 加载 Manager 向量化配置（按需触发）
	cfg.EmbeddingService.BaseURL = commonConfig.GetEnv("MANAGER_EMBEDDING_SERVICE_BASE_URL", "")
	cfg.EmbeddingService.APIKey = commonConfig.GetEnv("MANAGER_EMBEDDING_SERVICE_API_KEY", "")
	cfg.EmbeddingService.Timeout = commonConfig.GetEnvDuration("MANAGER_EMBEDDING_SERVICE_TIMEOUT", "15s")
	cfg.EmbeddingService.Models = map[string]string{
		"text":  commonConfig.GetEnv("MANAGER_EMBEDDING_MODEL_TEXT", "qwen2.5-vl-embedding"),
		"image": commonConfig.GetEnv("MANAGER_EMBEDDING_MODEL_IMAGE", "qwen2.5-vl-embedding"),
		"video": commonConfig.GetEnv("MANAGER_EMBEDDING_MODEL_VIDEO", "qwen2.5-vl-embedding"),
	}

	cfg.VectorConfig.Dimension = commonConfig.GetEnvInt("MANAGER_VECTOR_DIMENSION", 1024)

	// 解析 MaxDistance（浮点数）
	maxDistanceStr := commonConfig.GetEnv("MANAGER_VECTOR_SEARCH_MAX_DISTANCE", "0.35")
	if maxDistance, err := strconv.ParseFloat(maxDistanceStr, 64); err == nil && maxDistance > 0 {
		cfg.VectorConfig.MaxDistance = maxDistance
	} else {
		cfg.VectorConfig.MaxDistance = 0.35
	}

	cfg.VectorConfig.MaxFileSizeMB = commonConfig.GetEnvInt("MANAGER_VECTOR_MAX_FILE_SIZE_MB", 10)
	cfg.VectorConfig.BatchConcurrency = commonConfig.GetEnvInt("MANAGER_VECTOR_BATCH_CONCURRENCY", 5)

	// Redis 配置
	cfg.RedisHost = commonConfig.GetEnv("REDIS_HOST", "localhost")
	cfg.RedisPort = commonConfig.GetEnv("REDIS_PORT", "6379")
	cfg.RedisPassword = commonConfig.GetEnv("REDIS_PASSWORD", "")
	cfg.RedisDB = commonConfig.GetEnvInt("REDIS_DB", 0)

	// MinIO 配置（用于 MVT 瓦片存储）
	// 注意：Manager 模块使用系统 infra MinIO，端口从 MINIO_API_PORT 读取
	minioPort := commonConfig.GetEnv("MINIO_API_PORT", "9000")
	defaultEndpoint := fmt.Sprintf("localhost:%s", minioPort)
	cfg.MinioEndpoint = commonConfig.GetEnv("MINIO_SYSTEM_ENDPOINT", commonConfig.GetEnv("MINIO_ENDPOINT", defaultEndpoint))
	cfg.MinioAccessKey = commonConfig.GetEnv("MINIO_SYSTEM_ACCESS_KEY", commonConfig.GetEnv("MINIO_ROOT_USER", commonConfig.GetEnv("MINIO_ACCESS_KEY", "minioadmin")))
	cfg.MinioSecretKey = commonConfig.GetEnv("MINIO_SYSTEM_SECRET_KEY", commonConfig.GetEnv("MINIO_ROOT_PASSWORD", commonConfig.GetEnv("MINIO_SECRET_KEY", "minioadmin")))
	cfg.MinioUseSSL = commonConfig.GetEnvBool("MINIO_USE_SSL", false)

	// MVT 预缓存配置
	cfg.PreCache = PreCacheConfig{
		TargetRecordsPerTile:  commonConfig.GetEnvInt("PRE_CACHE_TARGET_RECORDS", 3000),
		MinDurationForCacheMS: commonConfig.GetEnvInt("PRE_CACHE_MIN_DURATION_MS", 100),
		MinSizeForCacheKB:     commonConfig.GetEnvInt("PRE_CACHE_MIN_SIZE_KB", 50),
		MaxZoom:               commonConfig.GetEnvInt("PRE_CACHE_MAX_ZOOM", 18),
		Concurrency:           commonConfig.GetEnvInt("PRE_CACHE_CONCURRENCY", 10),
	}

	return cfg
}
