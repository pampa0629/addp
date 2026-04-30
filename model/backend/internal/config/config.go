package config

import (
	"fmt"
	"os"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Model 模块特有配置
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

	// Standard 模块配置（用于代理）
	StandardURL string
}

// LoadConfig 加载配置
func LoadConfig() (*Config, error) {
	// 使用 common/config 统一加载 .env 文件
	commonConfig.LoadEnv()

	cfg := &Config{
		Port:     commonConfig.GetEnv("MODEL_BACKEND_PORT", "8181"),
		DBSchema: "model",

		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       commonConfig.GetEnvInt("REDIS_DB", 0),

		SystemURL:      commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		InternalAPIKey: os.Getenv("INTERNAL_API_KEY"),

		StandardURL: commonConfig.GetEnv("STANDARD_URL", "http://localhost:8110"),
	}

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
