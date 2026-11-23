package config

import (
	"fmt"
	"os"
	"strings"

	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/infra/database"
)

type Config struct {
	commonconfig.BaseConfig
	Port                string
	SystemURL           string
	ServiceClientSecret string
}

func Load() (*Config, error) {
	commonconfig.LoadEnv()
	if strings.TrimSpace(os.Getenv("ENCRYPTION_KEY")) == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY is required for inference credentials")
	}
	cfg := &Config{
		Port:                commonconfig.GetEnv("INFERENCE_BACKEND_PORT", "8191"),
		SystemURL:           commonconfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		ServiceClientSecret: commonconfig.GetEnv("INFERENCE_SERVICE_CLIENT_SECRET", ""),
	}
	commonconfig.LoadDeploymentConfig(&cfg.BaseConfig)
	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL
	if len(cfg.EncryptionKey) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must decode to 32 bytes")
	}
	return cfg, nil
}

func PlatformDatabase() database.Config {
	return database.Config{Host: commonconfig.GetEnv("POSTGRES_HOST", "localhost"), Port: commonconfig.GetEnv("POSTGRES_PORT", "15432"), DBName: commonconfig.GetEnv("POSTGRES_DB", "addp"), Schema: "inference"}
}
