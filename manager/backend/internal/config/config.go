package config

import (
	"log"
	"strings"
	"time"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Manager 模块特有配置
	Port                string
	DBSchema            string
	PreviewPluginDir    string
	MetaServiceURL      string
	SecurityServiceURL  string
	ServiceClientSecret string

	// Meilisearch 配置
	MeilisearchURL                 string
	MeilisearchMasterKey           string
	MeilisearchManagerContentIndex string

	VectorConfig struct {
		Dimension int // 固定向量维度，仅供数据库结构初始化
	}

	// Redis 配置（用于资源变更事件同步）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// MinIO 配置（用于 MVT 瓦片存储和数据导入中转，读取平台内置 MinIO）
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool

	// Transfer 服务配置（用于导入 / 导出时创建并触发 sync）
	TransferServiceURL string

	// 导出暂存清理配置。当前作为 Manager 内部默认值，不暴露为运维环境变量。
	ExportCleanup ExportCleanupConfig

	// 瓦片缓存生成配置
	TileCache TileCacheConfig

	// Raster mosaic 在线瓦片运行时
	RasterMosaicRuntime        RasterMosaicRuntimeConfig
	RasterMosaicGeneration     RasterMosaicGenerationConfig
	DocumentWorkflowGeneration DocumentWorkflowGenerationConfig
	PPTXPDFWorker              PPTXPDFWorkerConfig
}

type ExportCleanupConfig struct {
	SuccessRetention time.Duration
	FailedRetention  time.Duration
	MaxRunningAge    time.Duration
	Interval         time.Duration
}

// TileCacheConfig 瓦片缓存生成相关配置
type TileCacheConfig struct {
	Concurrency int // 并发协程数
	MaxDBConns  int // 数据库最大连接数（默认等于 Concurrency）

}

type RasterMosaicRuntimeConfig struct {
	BaseURL  string
	Timeout  time.Duration
	TileSize int
}

type RasterMosaicGenerationConfig struct {
	Timeout time.Duration
}

type DocumentWorkflowGenerationConfig struct {
	Timeout time.Duration
}

type PPTXPDFWorkerConfig struct {
	Concurrency       int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ClaimInterval     time.Duration
}

func resolveMeilisearchURL() string {
	return commonConfig.GetEnv("MEILISEARCH_URL", "")
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180")
	metaURL := commonConfig.GetEnv("META_URL", "http://localhost:8082")

	rawPluginDir := commonConfig.GetEnv("PREVIEW_PLUGIN_DIR", "")
	builtinPluginDir := commonConfig.ResolveFromRoot("manager", "backend", "plugins")
	var pluginDirs []string
	if trimmed := strings.TrimSpace(rawPluginDir); trimmed != "" {
		pluginDirs = append(pluginDirs, trimmed)
	}
	pluginDirs = append(pluginDirs, builtinPluginDir)
	defaultPluginDir := strings.Join(pluginDirs, ",")

	cfg := &Config{
		Port:                commonConfig.GetEnv("MANAGER_BACKEND_PORT", "8081"),
		DBSchema:            commonConfig.GetEnv("DB_SCHEMA", "manager"),
		PreviewPluginDir:    defaultPluginDir,
		MetaServiceURL:      metaURL,
		SecurityServiceURL:  commonConfig.GetEnv("SECURITY_URL", "http://localhost:8194"),
		ServiceClientSecret: commonConfig.GetEnv("MANAGER_SERVICE_CLIENT_SECRET", ""),
	}

	cfg.MeilisearchURL = resolveMeilisearchURL()
	cfg.MeilisearchMasterKey = commonConfig.GetEnv("MEILISEARCH_MASTER_KEY", "")
	cfg.MeilisearchManagerContentIndex = commonConfig.GetEnv("MEILISEARCH_MANAGER_CONTENT_INDEX", "manager_content_documents")

	// 设置 BaseConfig 字段
	cfg.SystemServiceURL = systemURL

	// Bootstrap 部署配置只从根 env / 进程环境读取。
	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)

	cfg.VectorConfig.Dimension = 2560

	// Redis 配置
	cfg.RedisHost = commonConfig.GetEnv("REDIS_HOST", "localhost")
	cfg.RedisPort = commonConfig.GetEnv("REDIS_PORT", "6379")
	cfg.RedisPassword = commonConfig.GetEnv("REDIS_PASSWORD", "")
	cfg.RedisDB = commonConfig.GetEnvInt("REDIS_DB", 0)

	minioCfg := commonConfig.LoadBuiltinMinIOConfig()
	cfg.MinioEndpoint = minioCfg.Endpoint
	cfg.MinioAccessKey = minioCfg.AccessKey
	cfg.MinioSecretKey = minioCfg.SecretKey
	cfg.MinioUseSSL = minioCfg.UseSSL

	// Transfer 服务配置
	cfg.TransferServiceURL = commonConfig.GetEnv("TRANSFER_URL", "http://localhost:8083")

	cfg.ExportCleanup = ExportCleanupConfig{
		SuccessRetention: 24 * time.Hour,
		FailedRetention:  6 * time.Hour,
		MaxRunningAge:    6 * time.Hour,
		Interval:         30 * time.Minute,
	}

	// 瓦片缓存生成配置
	concurrency := commonConfig.GetEnvInt("TILE_CACHE_CONCURRENCY", 10)
	maxDBConns := commonConfig.GetEnvInt("TILE_CACHE_MAX_DB_CONNS", 0)
	// 如果没有指定 MaxDBConns，默认等于 Concurrency
	if maxDBConns == 0 {
		maxDBConns = concurrency
	}

	cfg.TileCache = TileCacheConfig{
		Concurrency: concurrency,
		MaxDBConns:  maxDBConns,
	}
	cfg.RasterMosaicRuntime = RasterMosaicRuntimeConfig{
		BaseURL:  commonConfig.GetEnv("RASTER_MOSAIC_RUNTIME_URL", "http://127.0.0.1:8291"),
		Timeout:  commonConfig.GetEnvDuration("RASTER_MOSAIC_RUNTIME_TIMEOUT", "15s"),
		TileSize: commonConfig.GetEnvInt("RASTER_MOSAIC_TILE_SIZE", 256),
	}
	cfg.RasterMosaicGeneration = RasterMosaicGenerationConfig{
		Timeout: 2 * time.Hour,
	}
	cfg.DocumentWorkflowGeneration = DocumentWorkflowGenerationConfig{
		Timeout: 30 * time.Minute,
	}
	cfg.PPTXPDFWorker = PPTXPDFWorkerConfig{
		Concurrency:       commonConfig.GetEnvInt("MANAGER_PPTX_PDF_WORKER_CONCURRENCY", 1),
		LeaseDuration:     commonConfig.GetEnvDuration("MANAGER_PPTX_PDF_LEASE_DURATION", "2m"),
		HeartbeatInterval: commonConfig.GetEnvDuration("MANAGER_PPTX_PDF_HEARTBEAT_INTERVAL", "30s"),
		ClaimInterval:     commonConfig.GetEnvDuration("MANAGER_PPTX_PDF_CLAIM_INTERVAL", "1s"),
	}
	if cfg.RasterMosaicRuntime.TileSize != 512 {
		cfg.RasterMosaicRuntime.TileSize = 256
	}
	// 记录瓦片缓存生成配置（特别关注并发数和连接池）
	log.Printf("📋 Manager Config: 瓦片缓存生成配置")
	log.Printf("   TILE_CACHE_CONCURRENCY (并发数): %d", cfg.TileCache.Concurrency)
	log.Printf("   TILE_CACHE_MAX_DB_CONNS (数据库连接池): %d", cfg.TileCache.MaxDBConns)

	return cfg
}
