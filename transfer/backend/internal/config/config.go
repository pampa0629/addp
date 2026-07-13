package config

import (
	"fmt"
	"log"
	"time"

	commonConfig "github.com/addp/common/config"
)

type Config struct {
	commonConfig.BaseConfig

	// Transfer 模块特有配置
	Port                               string
	DBSchema                           string
	InternalAPIKey                     string // 服务间调用的 API Key
	MetaServiceURL                     string // Meta 服务地址
	RedisHost                          string
	RedisPort                          string
	RedisPassword                      string
	WorkerCount                        int
	MaxRetries                         int
	RetryDelay                         time.Duration
	TaskQueueName                      string
	ConcurrentTasks                    int
	ContinuousWorkerInstanceID         string
	ContinuousWorkerCapacity           int
	ContinuousLeaseDuration            time.Duration
	ContinuousHeartbeatInterval        time.Duration
	ContinuousClaimInterval            time.Duration
	ContinuousPollTimeout              time.Duration
	ContinuousFetchMaxBytes            int
	ContinuousDiagnosticsInterval      time.Duration
	ContinuousRetentionDegradedHorizon time.Duration
	ContinuousRetentionCriticalHorizon time.Duration

	BuiltinMinioEndpoint  string
	BuiltinMinioAccessKey string
	BuiltinMinioSecretKey string
	BuiltinMinioUseSSL    bool
}

func (c Config) ValidateContinuousRuntimeObservability() error {
	if c.ContinuousDiagnosticsInterval <= 0 || c.ContinuousRetentionDegradedHorizon <= 0 || c.ContinuousRetentionCriticalHorizon <= 0 {
		return fmt.Errorf("continuous diagnostics interval and retention horizons must be greater than zero")
	}
	if c.ContinuousRetentionCriticalHorizon >= c.ContinuousRetentionDegradedHorizon {
		return fmt.Errorf("continuous retention critical horizon must be less than degraded horizon")
	}
	return nil
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180")
	metaURL := commonConfig.GetEnv("META_URL", "http://localhost:8082")

	cfg := &Config{
		Port:                               commonConfig.GetEnv("TRANSFER_BACKEND_PORT", "8083"),
		DBSchema:                           commonConfig.GetEnv("DB_SCHEMA", "transfer"),
		InternalAPIKey:                     commonConfig.GetEnv("INTERNAL_API_KEY", ""),
		MetaServiceURL:                     metaURL,
		RedisHost:                          commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:                          commonConfig.GetEnv("REDIS_PORT", "6379"),
		RedisPassword:                      commonConfig.GetEnv("REDIS_PASSWORD", ""),
		WorkerCount:                        commonConfig.GetEnvInt("WORKER_COUNT", 5),
		MaxRetries:                         commonConfig.GetEnvInt("MAX_RETRIES", 3),
		RetryDelay:                         commonConfig.GetEnvDuration("RETRY_DELAY", "30s"),
		TaskQueueName:                      commonConfig.GetEnv("TASK_QUEUE_NAME", "transfer:tasks"),
		ConcurrentTasks:                    commonConfig.GetEnvInt("CONCURRENT_TASKS", 10),
		ContinuousWorkerInstanceID:         commonConfig.GetEnv("TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID", ""),
		ContinuousWorkerCapacity:           commonConfig.GetEnvInt("TRANSFER_CONTINUOUS_WORKER_CAPACITY", 4),
		ContinuousLeaseDuration:            commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_LEASE_DURATION", "30s"),
		ContinuousHeartbeatInterval:        commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_HEARTBEAT_INTERVAL", "10s"),
		ContinuousClaimInterval:            commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_CLAIM_INTERVAL", "2s"),
		ContinuousPollTimeout:              commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_POLL_TIMEOUT", "5s"),
		ContinuousFetchMaxBytes:            commonConfig.GetEnvInt("TRANSFER_CONTINUOUS_FETCH_MAX_BYTES", 52428800),
		ContinuousDiagnosticsInterval:      commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_DIAGNOSTICS_INTERVAL", "15s"),
		ContinuousRetentionDegradedHorizon: commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RETENTION_DEGRADED_HORIZON", "6h"),
		ContinuousRetentionCriticalHorizon: commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RETENTION_CRITICAL_HORIZON", "1h"),
	}

	minioCfg := commonConfig.LoadBuiltinMinIOConfig()
	cfg.BuiltinMinioEndpoint = minioCfg.Endpoint
	cfg.BuiltinMinioAccessKey = minioCfg.AccessKey
	cfg.BuiltinMinioSecretKey = minioCfg.SecretKey
	cfg.BuiltinMinioUseSSL = minioCfg.UseSSL

	// 设置 BaseConfig 字段
	cfg.SystemServiceURL = systemURL
	cfg.EnableIntegration = commonConfig.GetEnvBool("ENABLE_SERVICE_INTEGRATION", true)

	// 从 System 获取共享配置
	if cfg.EnableIntegration {
		log.Println("🔄 Attempting to load shared config from System service...")
		if err := commonConfig.LoadSharedConfig(systemURL, &cfg.BaseConfig); err != nil {
			log.Printf("⚠️  Warning: Failed to load shared config from System: %v", err)
			log.Printf("⚠️  Falling back to local environment variables...")
			commonConfig.LoadLocalConfig(&cfg.BaseConfig)
		} else {
			log.Println("✅ Successfully loaded shared config from System service")
		}
	} else {
		log.Println("ℹ️  Service integration disabled, using local config")
		commonConfig.LoadLocalConfig(&cfg.BaseConfig)
	}

	// 如果本地未配置 InternalAPIKey，尝试从 BaseConfig 获取
	if cfg.InternalAPIKey == "" {
		cfg.InternalAPIKey = cfg.BaseConfig.InternalAPIKey
	}

	return cfg
}
