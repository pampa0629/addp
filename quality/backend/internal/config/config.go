package config

import (
	"fmt"
	"os"
	"time"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	Port              string
	DBSchema          string
	CheckTimeout      time.Duration
	WorkerConcurrency int
	WorkerLease       time.Duration
	WorkerPoll        time.Duration

	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	SystemURL           string
	StandardURL         string
	ModelURL            string
	ServiceClientSecret string
}

func LoadConfig() (*Config, error) {
	commonConfig.LoadEnv()

	cfg := &Config{
		Port:              commonConfig.GetEnv("QUALITY_BACKEND_PORT", "8182"),
		DBSchema:          "quality",
		CheckTimeout:      commonConfig.GetEnvDuration("QUALITY_CHECK_TIMEOUT", "30m"),
		WorkerConcurrency: commonConfig.GetEnvInt("QUALITY_WORKER_CONCURRENCY", 4),
		WorkerLease:       commonConfig.GetEnvDuration("QUALITY_WORKER_LEASE_DURATION", "30m"),
		WorkerPoll:        commonConfig.GetEnvDuration("QUALITY_WORKER_POLL_INTERVAL", "500ms"),

		RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       commonConfig.GetEnvInt("REDIS_DB", 0),

		SystemURL:           commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		StandardURL:         commonConfig.GetEnv("STANDARD_URL", "http://localhost:8110"),
		ModelURL:            commonConfig.GetEnv("MODEL_URL", "http://localhost:8181"),
		ServiceClientSecret: os.Getenv("QUALITY_SERVICE_CLIENT_SECRET"),
	}

	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)
	if cfg.CheckTimeout <= 0 {
		return nil, fmt.Errorf("QUALITY_CHECK_TIMEOUT must be positive")
	}
	if cfg.WorkerConcurrency <= 0 {
		return nil, fmt.Errorf("QUALITY_WORKER_CONCURRENCY must be positive")
	}
	if cfg.WorkerLease <= 0 || cfg.WorkerPoll <= 0 || cfg.WorkerPoll >= cfg.WorkerLease {
		return nil, fmt.Errorf("QUALITY worker lease and poll configuration is invalid")
	}

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
