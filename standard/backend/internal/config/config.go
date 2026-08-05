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
	}
	minioCfg := commonConfig.LoadBuiltinMinIOConfig()
	cfg.MinioEndpoint = minioCfg.Endpoint
	cfg.MinioAccessKey = minioCfg.AccessKey
	cfg.MinioSecretKey = minioCfg.SecretKey
	cfg.MinioUseSSL = minioCfg.UseSSL

	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)

	// 设置 System 服务 URL
	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL

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
