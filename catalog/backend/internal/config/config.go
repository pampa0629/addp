package config

import (
	"fmt"
	"os"
	"time"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	Port                                 string
	DBSchema                             string
	SystemURL                            string
	MetaURL                              string
	ModelURL                             string
	StandardURL                          string
	ServiceURL                           string
	DevelopURL                           string
	WorkbenchURL                         string
	QualityURL                           string
	ServiceClientSecret                  string
	SourceSyncInterval                   time.Duration
	ProjectionInterval                   time.Duration
	ResponsibilityReconciliationInterval time.Duration
	MeilisearchURL                       string
	MeilisearchAPIKey                    string
	MeilisearchIndex                     string
}

func LoadConfig() (*Config, error) {
	commonConfig.LoadEnv()
	cfg := &Config{
		Port: commonConfig.GetEnv("CATALOG_BACKEND_PORT", "8192"), DBSchema: "catalog",
		SystemURL:                            commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		MetaURL:                              commonConfig.GetEnv("META_URL", "http://localhost:8082"),
		ModelURL:                             commonConfig.GetEnv("MODEL_URL", "http://localhost:8181"),
		StandardURL:                          commonConfig.GetEnv("STANDARD_URL", "http://localhost:8110"),
		ServiceURL:                           commonConfig.GetEnv("SERVICE_URL", "http://localhost:8086"),
		DevelopURL:                           commonConfig.GetEnv("DEVELOP_URL", "http://localhost:8185"),
		WorkbenchURL:                         commonConfig.GetEnv("WORKBENCH_URL", "http://localhost:8193"),
		QualityURL:                           commonConfig.GetEnv("QUALITY_URL", "http://localhost:8182"),
		ServiceClientSecret:                  os.Getenv("CATALOG_SERVICE_CLIENT_SECRET"),
		SourceSyncInterval:                   commonConfig.GetEnvDuration("CATALOG_SOURCE_SYNC_INTERVAL", "30s"),
		ProjectionInterval:                   commonConfig.GetEnvDuration("CATALOG_PROJECTION_INTERVAL", "2s"),
		ResponsibilityReconciliationInterval: commonConfig.GetEnvDuration("CATALOG_RESPONSIBILITY_RECONCILIATION_INTERVAL", "5m"),
		MeilisearchURL:                       commonConfig.GetEnv("MEILISEARCH_URL", "http://localhost:17700"),
		MeilisearchAPIKey:                    os.Getenv("MEILISEARCH_MASTER_KEY"),
		MeilisearchIndex:                     commonConfig.GetEnv("MEILISEARCH_CATALOG_INDEX", "catalog_entries"),
	}
	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)
	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL
	return cfg, nil
}

func (c *Config) GetDatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSchema,
	)
}
