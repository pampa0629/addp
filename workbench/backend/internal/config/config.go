package config

import (
	"os"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/infra/database"
)

type Config struct {
	commonConfig.BaseConfig

	Port                string
	SystemURL           string
	ServiceURL          string
	ServiceClientSecret string
}

func LoadConfig() (*Config, error) {
	commonConfig.LoadEnv()
	cfg := &Config{
		Port:                commonConfig.GetEnv("WORKBENCH_BACKEND_PORT", "8193"),
		SystemURL:           commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		ServiceURL:          commonConfig.GetEnv("SERVICE_URL", "http://localhost:8086"),
		ServiceClientSecret: os.Getenv("WORKBENCH_SERVICE_CLIENT_SECRET"),
	}
	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)
	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL
	return cfg, nil
}

func PlatformDatabase() database.Config {
	return database.Config{Host: commonConfig.GetEnv("POSTGRES_HOST", "localhost"), Port: commonConfig.GetEnv("POSTGRES_PORT", "15432"), DBName: commonConfig.GetEnv("POSTGRES_DB", "addp"), Schema: "workbench"}
}
