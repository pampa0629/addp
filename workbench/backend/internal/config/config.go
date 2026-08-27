package config

import (
	"fmt"
	"os"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	Port                string
	DBSchema            string
	SystemURL           string
	ServiceURL          string
	ServiceClientSecret string
}

func LoadConfig() (*Config, error) {
	commonConfig.LoadEnv()
	cfg := &Config{
		Port:                commonConfig.GetEnv("WORKBENCH_BACKEND_PORT", "8193"),
		DBSchema:            "workbench",
		SystemURL:           commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		ServiceURL:          commonConfig.GetEnv("SERVICE_URL", "http://localhost:8086"),
		ServiceClientSecret: os.Getenv("WORKBENCH_SERVICE_CLIENT_SECRET"),
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
