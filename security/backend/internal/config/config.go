package config

import (
	"os"

	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/infra/database"
)

type Config struct {
	commonconfig.BaseConfig
	Port                string
	SystemURL           string
	MetaURL             string
	ServiceClientSecret string
}

func Load() *Config {
	commonconfig.LoadEnv()
	cfg := &Config{
		Port:                commonconfig.GetEnv("SECURITY_BACKEND_PORT", "8194"),
		SystemURL:           commonconfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		MetaURL:             commonconfig.GetEnv("META_URL", "http://localhost:8082"),
		ServiceClientSecret: os.Getenv("SECURITY_SERVICE_CLIENT_SECRET"),
	}
	commonconfig.LoadDeploymentConfig(&cfg.BaseConfig)
	cfg.BaseConfig.SystemServiceURL = cfg.SystemURL
	return cfg
}

func (c *Config) PlatformDatabase() database.Config {
	return database.Config{
		Host: c.DBHost, Port: c.DBPort,
		User: c.DBUser, Password: c.DBPassword,
		DBName: c.DBName, Schema: "security",
	}
}
