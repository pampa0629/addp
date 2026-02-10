package config

import (
	"fmt"
	"log"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Service 模块特有配置
	Port     string
	DBSchema string

	// Gateway URL (用于生成对外服务端点)
	GatewayURL string

	// 模块集成配置
	ManagerServiceURL string
	MetaServiceURL    string

	// Redis 配置（用于资源变更事件同步）
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// MinIO 配置（用于瓦片存储）
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool

	// 定时任务配置
	HealthCheckCron     string // 健康检查 Cron 表达式
	MetadataRefreshCron string // 元数据刷新 Cron 表达式
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_SERVICE_URL", "http://localhost:8180")
	managerURL := commonConfig.GetEnv("MANAGER_SERVICE_URL", "http://localhost:8081")
	metaURL := commonConfig.GetEnv("META_SERVICE_URL", "http://localhost:8082")

	cfg := &Config{
		Port:              commonConfig.GetEnv("SERVICE_BACKEND_PORT", "8086"),
		DBSchema:          commonConfig.GetEnv("DB_SCHEMA", "service"),
		GatewayURL:        commonConfig.GetEnv("GATEWAY_URL", "http://localhost:8000"),
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

	// MinIO 配置（用于瓦片存储）
	// 注意：Service 模块使用系统 infra MinIO，端口从 MINIO_API_PORT 读取
	minioPort := commonConfig.GetEnv("MINIO_API_PORT", "9000")
	defaultEndpoint := fmt.Sprintf("localhost:%s", minioPort)
	cfg.MinioEndpoint = commonConfig.GetEnv("MINIO_SYSTEM_ENDPOINT", commonConfig.GetEnv("MINIO_ENDPOINT", defaultEndpoint))
	cfg.MinioAccessKey = commonConfig.GetEnv("MINIO_SYSTEM_ACCESS_KEY", commonConfig.GetEnv("MINIO_ROOT_USER", commonConfig.GetEnv("MINIO_ACCESS_KEY", "minioadmin")))
	cfg.MinioSecretKey = commonConfig.GetEnv("MINIO_SYSTEM_SECRET_KEY", commonConfig.GetEnv("MINIO_ROOT_PASSWORD", commonConfig.GetEnv("MINIO_SECRET_KEY", "minioadmin")))
	cfg.MinioUseSSL = commonConfig.GetEnvBool("MINIO_USE_SSL", false)

	// 定时任务配置
	cfg.HealthCheckCron = commonConfig.GetEnv("SERVICE_HEALTH_CHECK_CRON", "0 * * * *")       // 默认每小时
	cfg.MetadataRefreshCron = commonConfig.GetEnv("SERVICE_METADATA_REFRESH_CRON", "0 2 * * *") // 默认每天凌晨2点

	return cfg
}
