package config

import (
	"fmt"
	"os"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	Port     string
	DBSchema string

	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	SystemURL           string
	ServiceClientSecret string

	// 源模块 URL（用于资产自动发现）
	MetaURL     string
	ServiceURL  string
	StandardURL string
	DevelopURL  string

	// Meilisearch（用于资产全文搜索，可选）
	MeilisearchURL        string
	MeilisearchMasterKey  string
	MeilisearchAssetIndex string
}

func LoadConfig() (*Config, error) {
	commonConfig.LoadEnv()

	cfg := &Config{
		Port:     commonConfig.GetEnv("ASSET_BACKEND_PORT", "8183"),
		DBSchema: "asset",

		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       commonConfig.GetEnvInt("REDIS_DB", 0),

		SystemURL:           commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		ServiceClientSecret: commonConfig.GetEnv("ASSET_SERVICE_CLIENT_SECRET", ""),

		MetaURL:     commonConfig.GetEnv("META_URL", "http://localhost:8082"),
		ServiceURL:  commonConfig.GetEnv("SERVICE_URL", "http://localhost:8086"),
		StandardURL: commonConfig.GetEnv("STANDARD_URL", "http://localhost:8110"),
		DevelopURL:  commonConfig.GetEnv("DEVELOP_URL", "http://localhost:8185"),

		MeilisearchURL:        os.Getenv("MEILISEARCH_URL"),
		MeilisearchMasterKey:  os.Getenv("MEILISEARCH_MASTER_KEY"),
		MeilisearchAssetIndex: commonConfig.GetEnv("MEILISEARCH_ASSET_CATALOG_INDEX", "asset_catalog"),
	}

	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)
	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL

	return cfg, nil
}

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
