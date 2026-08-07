package config

import commonConfig "github.com/addp/common/config"

type Config struct {
	commonConfig.BaseConfig

	// Service 模块特有配置
	Port     string
	DBSchema string

	// Gateway URL (用于生成对外服务端点)
	GatewayURL string

	// 模块集成配置
	ManagerServiceURL   string
	MetaServiceURL      string
	ServiceClientSecret string

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
	systemURL := commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180")
	managerURL := commonConfig.GetEnv("MANAGER_URL", "http://localhost:8081")
	metaURL := commonConfig.GetEnv("META_URL", "http://localhost:8082")

	cfg := &Config{
		Port:                commonConfig.GetEnv("SERVICE_BACKEND_PORT", "8086"),
		DBSchema:            commonConfig.GetEnv("DB_SCHEMA", "service"),
		GatewayURL:          commonConfig.GetEnv("GATEWAY_URL", "http://localhost:8000"),
		ManagerServiceURL:   managerURL,
		MetaServiceURL:      metaURL,
		ServiceClientSecret: commonConfig.GetEnv("SERVICE_SERVICE_CLIENT_SECRET", ""),
	}

	// 设置 BaseConfig 字段
	cfg.SystemServiceURL = systemURL

	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)

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

	// 定时任务配置
	cfg.HealthCheckCron = "0 * * * *"
	cfg.MetadataRefreshCron = "0 2 * * *"

	return cfg
}
