package config

import (
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	commonConfig "github.com/addp/common/config"
	engineplugin "github.com/addp/common/engine/plugin"
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
	ContinuousCheckpointStaleAfter     time.Duration
	ContinuousRecoveryInitialBackoff   time.Duration
	ContinuousRecoveryMaxBackoff       time.Duration
	ContinuousRecoveryMaxFailures      int
	ContinuousRecoveryCircuitOpenTime  time.Duration
	ContinuousRecoveryStabilityWindow  time.Duration
	InfraKafkaBootstrapServers         string
	InfraKafkaAdminUsername            string
	InfraKafkaAdminPassword            string
	InfraKafkaTransferPassword         string
	InfraKafkaSecurityProtocol         string
	InfraKafkaTLSCACertFile            string
	InfraKafkaTLSInsecure              bool
	CaptureTopicRetention              time.Duration
	CaptureTopicRetentionBytes         int64
	CaptureTopicReplicationFactor      int
	DeadLetterTopicRetention           time.Duration
	DeadLetterTopicRetentionBytes      int64
	DeadLetterTopicReplicationFactor   int
	DeadLetterReconcileInterval        time.Duration
	DeadLetterReconcileBatchSize       int
	DeadLetterReconcileTimeout         time.Duration
	DeadLetterReconcileFetchMaxBytes   int
	KafkaConnectURL                    string
	KafkaConnectUsername               string
	KafkaConnectPassword               string
	KafkaConnectTimeout                time.Duration
	KafkaConnectLoopbackHost           string
	KafkaConnectBootstrapServers       string
	KafkaConnectKafkaUsername          string
	KafkaConnectKafkaPassword          string
	KafkaConnectKafkaSecurityProtocol  string
	KafkaConnectKafkaTLSCACertFile     string
	CaptureProvisioningTimeout         time.Duration
	CaptureStatusPollInterval          time.Duration
	CaptureMonitorInterval             time.Duration
	ContinuousRuntimeStopTimeout       time.Duration
	ContinuousRuntimeStopPollInterval  time.Duration

	BuiltinMinioEndpoint  string
	BuiltinMinioAccessKey string
	BuiltinMinioSecretKey string
	BuiltinMinioUseSSL    bool
}

func (c Config) ValidateContinuousRuntime() error {
	if c.ContinuousDiagnosticsInterval <= 0 || c.ContinuousRetentionDegradedHorizon <= 0 || c.ContinuousRetentionCriticalHorizon <= 0 || c.ContinuousCheckpointStaleAfter <= 0 {
		return fmt.Errorf("continuous diagnostics, retention, and checkpoint durations must be greater than zero")
	}
	if c.ContinuousRetentionCriticalHorizon >= c.ContinuousRetentionDegradedHorizon {
		return fmt.Errorf("continuous retention critical horizon must be less than degraded horizon")
	}
	if c.ContinuousCheckpointStaleAfter <= c.ContinuousDiagnosticsInterval {
		return fmt.Errorf("continuous checkpoint stale threshold must be greater than diagnostics interval")
	}
	if c.ContinuousRecoveryInitialBackoff <= 0 || c.ContinuousRecoveryMaxBackoff <= 0 || c.ContinuousRecoveryCircuitOpenTime <= 0 || c.ContinuousRecoveryStabilityWindow <= 0 {
		return fmt.Errorf("continuous recovery durations must be greater than zero")
	}
	if c.ContinuousRecoveryInitialBackoff > c.ContinuousRecoveryMaxBackoff {
		return fmt.Errorf("continuous recovery initial backoff must not exceed max backoff")
	}
	if c.ContinuousRecoveryMaxFailures <= 0 {
		return fmt.Errorf("continuous recovery max failures must be greater than zero")
	}
	if c.DeadLetterReconcileInterval <= 0 || c.DeadLetterReconcileTimeout <= 0 ||
		c.DeadLetterReconcileBatchSize <= 0 || c.DeadLetterReconcileBatchSize > 1000 ||
		c.DeadLetterReconcileFetchMaxBytes <= 0 || int64(c.DeadLetterReconcileFetchMaxBytes) > math.MaxInt32 {
		return fmt.Errorf("dead-letter reconcile interval, timeout, batch size, and fetch bytes must be valid")
	}
	if c.ContinuousRuntimeStopTimeout <= 0 || c.ContinuousRuntimeStopPollInterval <= 0 ||
		c.ContinuousRuntimeStopPollInterval >= c.ContinuousRuntimeStopTimeout {
		return fmt.Errorf("continuous runtime stop timeout and poll interval must be valid")
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
		ContinuousCheckpointStaleAfter:     commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_CHECKPOINT_STALE_AFTER", "5m"),
		ContinuousRecoveryInitialBackoff:   commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RECOVERY_INITIAL_BACKOFF", "1s"),
		ContinuousRecoveryMaxBackoff:       commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RECOVERY_MAX_BACKOFF", "1m"),
		ContinuousRecoveryMaxFailures:      commonConfig.GetEnvInt("TRANSFER_CONTINUOUS_RECOVERY_MAX_CONSECUTIVE_FAILURES", 5),
		ContinuousRecoveryCircuitOpenTime:  commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RECOVERY_CIRCUIT_OPEN_DURATION", "5m"),
		ContinuousRecoveryStabilityWindow:  commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RECOVERY_STABILITY_WINDOW", "5m"),
		InfraKafkaBootstrapServers:         commonConfig.GetEnv("INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		InfraKafkaAdminUsername:            commonConfig.GetEnv("INFRA_KAFKA_ADMIN_USERNAME", "admin"),
		InfraKafkaAdminPassword:            commonConfig.GetEnv("INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		InfraKafkaTransferPassword:         commonConfig.GetEnv("INFRA_KAFKA_TRANSFER_PASSWORD", "addp_kafka_transfer"),
		InfraKafkaSecurityProtocol:         commonConfig.GetEnv("INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		InfraKafkaTLSCACertFile:            commonConfig.GetEnv("INFRA_KAFKA_TLS_CA_CERT_FILE", ""),
		InfraKafkaTLSInsecure:              commonConfig.GetEnvBool("INFRA_KAFKA_TLS_INSECURE_SKIP_VERIFY", false),
		CaptureTopicRetention:              commonConfig.GetEnvDuration("INFRA_KAFKA_CDC_RETENTION", "168h"),
		CaptureTopicRetentionBytes:         getEnvInt64("INFRA_KAFKA_CDC_RETENTION_BYTES", 0),
		CaptureTopicReplicationFactor:      commonConfig.GetEnvInt("INFRA_KAFKA_CDC_REPLICATION_FACTOR", 1),
		DeadLetterTopicRetention:           commonConfig.GetEnvDuration("INFRA_KAFKA_DLQ_RETENTION", "168h"),
		DeadLetterTopicRetentionBytes:      getEnvInt64("INFRA_KAFKA_DLQ_RETENTION_BYTES", 0),
		DeadLetterTopicReplicationFactor:   commonConfig.GetEnvInt("INFRA_KAFKA_DLQ_REPLICATION_FACTOR", 1),
		DeadLetterReconcileInterval:        commonConfig.GetEnvDuration("TRANSFER_DLQ_RECONCILE_INTERVAL", "1m"),
		DeadLetterReconcileBatchSize:       commonConfig.GetEnvInt("TRANSFER_DLQ_RECONCILE_BATCH_SIZE", 100),
		DeadLetterReconcileTimeout:         commonConfig.GetEnvDuration("TRANSFER_DLQ_RECONCILE_TIMEOUT", "10s"),
		DeadLetterReconcileFetchMaxBytes:   commonConfig.GetEnvInt("TRANSFER_DLQ_RECONCILE_FETCH_MAX_BYTES", 10485760),
		KafkaConnectURL:                    commonConfig.GetEnv("KAFKA_CONNECT_URL", "http://localhost:18083"),
		KafkaConnectUsername:               commonConfig.GetEnv("KAFKA_CONNECT_USERNAME", ""),
		KafkaConnectPassword:               commonConfig.GetEnv("KAFKA_CONNECT_PASSWORD", ""),
		KafkaConnectTimeout:                commonConfig.GetEnvDuration("KAFKA_CONNECT_TIMEOUT", "15s"),
		KafkaConnectLoopbackHost:           commonConfig.GetEnv("KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
		KafkaConnectBootstrapServers:       commonConfig.GetEnv("KAFKA_CONNECT_BOOTSTRAP_SERVERS", "kafka:29092"),
		KafkaConnectKafkaUsername:          commonConfig.GetEnv("KAFKA_CONNECT_KAFKA_USERNAME", "connect"),
		KafkaConnectKafkaPassword:          commonConfig.GetEnv("INFRA_KAFKA_CONNECT_PASSWORD", "addp_kafka_connect"),
		KafkaConnectKafkaSecurityProtocol:  commonConfig.GetEnv("KAFKA_CONNECT_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		KafkaConnectKafkaTLSCACertFile:     commonConfig.GetEnv("KAFKA_CONNECT_KAFKA_TLS_CA_CERT_FILE", commonConfig.GetEnv("INFRA_KAFKA_TLS_CA_CERT_FILE", "")),
		CaptureProvisioningTimeout:         commonConfig.GetEnvDuration("TRANSFER_CAPTURE_PROVISIONING_TIMEOUT", "60s"),
		CaptureStatusPollInterval:          commonConfig.GetEnvDuration("TRANSFER_CAPTURE_STATUS_POLL_INTERVAL", "1s"),
		CaptureMonitorInterval:             commonConfig.GetEnvDuration("TRANSFER_CAPTURE_MONITOR_INTERVAL", "15s"),
		ContinuousRuntimeStopTimeout:       commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RUNTIME_STOP_TIMEOUT", "45s"),
		ContinuousRuntimeStopPollInterval:  commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RUNTIME_STOP_POLL_INTERVAL", "250ms"),
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

func (c Config) InfraKafkaTransferConnectionInfo() (engineplugin.ConnectionInfo, error) {
	return c.infraKafkaConnectionInfo("transfer", c.InfraKafkaTransferPassword, "addp-transfer-continuous-worker")
}

func (c Config) InfraKafkaAdminConnectionInfo() (engineplugin.ConnectionInfo, error) {
	return c.infraKafkaConnectionInfo(c.InfraKafkaAdminUsername, c.InfraKafkaAdminPassword, "addp-transfer-dlq-cleanup")
}

func (c Config) infraKafkaConnectionInfo(username, password, clientID string) (engineplugin.ConnectionInfo, error) {
	if strings.TrimSpace(c.InfraKafkaBootstrapServers) == "" {
		return nil, fmt.Errorf("Infra Kafka bootstrap servers are required")
	}
	protocol := strings.ToLower(strings.TrimSpace(c.InfraKafkaSecurityProtocol))
	if protocol == "" {
		protocol = "sasl_plaintext"
	}
	if protocol != "plaintext" && protocol != "ssl" && protocol != "sasl_plaintext" && protocol != "sasl_ssl" {
		return nil, fmt.Errorf("unsupported Infra Kafka security protocol %q", protocol)
	}
	info := engineplugin.ConnectionInfo{
		"bootstrap_servers": c.InfraKafkaBootstrapServers,
		"security_protocol": protocol,
		"client_id":         clientID,
	}
	if protocol == "sasl_plaintext" || protocol == "sasl_ssl" {
		if strings.TrimSpace(username) == "" || password == "" {
			return nil, fmt.Errorf("Infra Kafka SASL username and password are required")
		}
		info["username"] = strings.TrimSpace(username)
		info["password"] = password
		info["sasl_mechanism"] = "plain"
	}
	if protocol == "ssl" || protocol == "sasl_ssl" {
		if strings.TrimSpace(c.InfraKafkaTLSCACertFile) != "" {
			pem, err := os.ReadFile(c.InfraKafkaTLSCACertFile)
			if err != nil {
				return nil, fmt.Errorf("read Infra Kafka CA certificate: %w", err)
			}
			info["tls_ca_cert"] = string(pem)
		}
		info["tls_insecure_skip_verify"] = c.InfraKafkaTLSInsecure
	}
	return info, nil
}

func getEnvInt64(key string, fallback int64) int64 {
	value := commonConfig.GetEnv(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
