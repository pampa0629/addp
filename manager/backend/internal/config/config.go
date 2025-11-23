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
	Port             string
	DBSchema         string
	PreviewPluginDir string
	MetaServiceURL   string

	// Elasticsearch 配置
	ElasticsearchURL              string
	ElasticsearchUsername         string
	ElasticsearchPassword         string
	ElasticsearchAPIKey           string
	ElasticsearchDocumentIndex    string
	ElasticsearchAssetIndex       string
	ElasticsearchDisableTLSVerify bool

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
}

func resolveElasticsearchURL() string {
	url := commonConfig.GetEnv("ELASTICSEARCH_URL", "")
	if url == "" {
		url = commonConfig.GetEnv("ELASTICSEARCH_URL_LOCAL", "")
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
		Port:             commonConfig.GetEnv("PORT", "8081"),
		DBSchema:         commonConfig.GetEnv("DB_SCHEMA", "manager"),
		PreviewPluginDir: defaultPluginDir,
		MetaServiceURL:   metaURL,
	}

	cfg.ElasticsearchURL = resolveElasticsearchURL()
	cfg.ElasticsearchUsername = commonConfig.GetEnv("ELASTICSEARCH_USERNAME", "")
	cfg.ElasticsearchPassword = commonConfig.GetEnv("ELASTICSEARCH_PASSWORD", "")
	cfg.ElasticsearchAPIKey = commonConfig.GetEnv("ELASTICSEARCH_API_KEY", "")
	cfg.ElasticsearchDocumentIndex = commonConfig.GetEnv("META_DOCUMENT_INDEX", commonConfig.GetEnv("ASSET_DOCUMENT_INDEX", "asset-documents"))
	cfg.ElasticsearchAssetIndex = commonConfig.GetEnv("ELASTICSEARCH_INDEX", "metadata-assets")
	cfg.ElasticsearchDisableTLSVerify = commonConfig.GetEnvBool("ELASTICSEARCH_SKIP_VERIFY", false)

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

	return cfg
}
