package config

import (
	"fmt"
	"os"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Standard 模块特有配置
	Port     string
	DBSchema string

	// Redis 配置
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// System 模块配置
	SystemURL      string
	InternalAPIKey string

	// MinIO 配置
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool
}

// LoadConfig 加载配置
func LoadConfig() (*Config, error) {
	// 使用 common/config 统一加载 .env 文件
	commonConfig.LoadEnv()

	cfg := &Config{
		Port:     commonConfig.GetEnv("STANDARD_BACKEND_PORT", "8110"),
		DBSchema: "standard",

		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       commonConfig.GetEnvInt("REDIS_DB", 0),

		SystemURL:      commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		InternalAPIKey: os.Getenv("INTERNAL_API_KEY"),

		MinioAccessKey: commonConfig.GetEnv("MINIO_SYSTEM_ACCESS_KEY", commonConfig.GetEnv("MINIO_ROOT_USER", commonConfig.GetEnv("MINIO_ACCESS_KEY", "minioadmin"))),
		MinioSecretKey: commonConfig.GetEnv("MINIO_SYSTEM_SECRET_KEY", commonConfig.GetEnv("MINIO_ROOT_PASSWORD", commonConfig.GetEnv("MINIO_SECRET_KEY", "minioadmin"))),
		MinioUseSSL:    commonConfig.GetEnvBool("MINIO_USE_SSL", false),
	}
	minioPort := commonConfig.GetEnv("MINIO_API_PORT", "9000")
	defaultEndpoint := fmt.Sprintf("localhost:%s", minioPort)
	cfg.MinioEndpoint = commonConfig.GetEnv("MINIO_SYSTEM_ENDPOINT", commonConfig.GetEnv("MINIO_ENDPOINT", defaultEndpoint))

	// 从 System 服务加载共享配置（带降级）
	enableIntegration := commonConfig.GetEnvBool("ENABLE_SERVICE_INTEGRATION", true)
	if err := commonConfig.LoadServiceConfiguration(commonConfig.ServiceConfigLoader{
		SystemServiceURL:      cfg.SystemURL,
		EnableIntegration:     enableIntegration,
		InternalAPIKey:        cfg.InternalAPIKey,
		BaseConfigDestination: &cfg.BaseConfig,
	}); err != nil {
		return nil, fmt.Errorf("failed to load service configuration: %w", err)
	}

	// 设置 System 服务 URL
	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL
	cfg.BaseConfig.EnableIntegration = enableIntegration

	return cfg, nil
}

// GetDatabaseDSN 获取数据库连接字符串
func (c *Config) GetDatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		c.DBHost,
		c.DBPort,
		c.DBUser,
		c.DBPassword,
		c.DBName,
		c.DBSchema,
	)
}
