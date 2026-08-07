package config

import (
	"fmt"
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
	MetaServiceURL                     string // Meta 服务地址
	ServiceClientSecret                string
	RedisHost                          string
	RedisPort                          string
	RedisPassword                      string
	RetryDelay                         time.Duration
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
	InfraKafkaSASLMechanism            string
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
	KafkaConnectKafkaSASLMechanism     string
	KafkaConnectKafkaTLSCACertFile     string
	CaptureProvisioningTimeout         time.Duration
	CaptureStatusPollInterval          time.Duration
	CaptureMonitorInterval             time.Duration
	ContinuousRuntimeStopTimeout       time.Duration
	ContinuousRuntimeStopPollInterval  time.Duration
	MetaScanClaimTTL                   time.Duration

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
	if c.MetaScanClaimTTL <= time.Minute {
		return fmt.Errorf("Meta scan claim TTL must be greater than the 60-second Meta client timeout")
	}
	return nil
}

func Load() *Config {
	systemURL := commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180")
	metaURL := commonConfig.GetEnv("META_URL", "http://localhost:8082")

	cfg := &Config{
		Port:                               commonConfig.GetEnv("TRANSFER_BACKEND_PORT", "8083"),
		DBSchema:                           commonConfig.GetEnv("DB_SCHEMA", "transfer"),
		MetaServiceURL:                     metaURL,
		ServiceClientSecret:                commonConfig.GetEnv("TRANSFER_SERVICE_CLIENT_SECRET", ""),
		RedisHost:                          commonConfig.GetEnv("REDIS_HOST", "localhost"),
		RedisPort:                          commonConfig.GetEnv("REDIS_PORT", "6379"),
		RedisPassword:                      commonConfig.GetEnv("REDIS_PASSWORD", ""),
		RetryDelay:                         commonConfig.GetEnvDuration("TRANSFER_WORKER_RETRY_DELAY", "30s"),
		ConcurrentTasks:                    commonConfig.GetEnvInt("TRANSFER_WORKER_CONCURRENCY", 10),
		ContinuousWorkerInstanceID:         commonConfig.GetEnv("TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID", ""),
		ContinuousWorkerCapacity:           commonConfig.GetEnvInt("TRANSFER_CONTINUOUS_WORKER_CAPACITY", 4),
		ContinuousLeaseDuration:            commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_LEASE_DURATION", "30s"),
		ContinuousHeartbeatInterval:        commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_HEARTBEAT_INTERVAL", "10s"),
		ContinuousClaimInterval:            commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_CLAIM_INTERVAL", "2s"),
		ContinuousPollTimeout:              commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_POLL_TIMEOUT", "5s"),
		ContinuousFetchMaxBytes:            commonConfig.GetEnvInt("TRANSFER_CONTINUOUS_FETCH_MAX_BYTES", 52428800),
		ContinuousDiagnosticsInterval:      15 * time.Second,
		ContinuousRetentionDegradedHorizon: 6 * time.Hour,
		ContinuousRetentionCriticalHorizon: time.Hour,
		ContinuousCheckpointStaleAfter:     5 * time.Minute,
		ContinuousRecoveryInitialBackoff:   time.Second,
		ContinuousRecoveryMaxBackoff:       time.Minute,
		ContinuousRecoveryMaxFailures:      5,
		ContinuousRecoveryCircuitOpenTime:  5 * time.Minute,
		ContinuousRecoveryStabilityWindow:  5 * time.Minute,
		InfraKafkaBootstrapServers:         commonConfig.GetEnv("INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		InfraKafkaAdminUsername:            commonConfig.GetEnv("INFRA_KAFKA_ADMIN_USERNAME", "admin"),
		InfraKafkaAdminPassword:            commonConfig.GetEnv("INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		InfraKafkaTransferPassword:         commonConfig.GetEnv("INFRA_KAFKA_TRANSFER_PASSWORD", "addp_kafka_transfer"),
		InfraKafkaSecurityProtocol:         commonConfig.GetEnv("INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		InfraKafkaSASLMechanism:            commonConfig.GetEnv("INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
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
		KafkaConnectBootstrapServers:       commonConfig.GetEnv("KAFKA_CONNECT_BOOTSTRAP_SERVERS", "redpanda:29092"),
		KafkaConnectKafkaUsername:          commonConfig.GetEnv("KAFKA_CONNECT_KAFKA_USERNAME", "connect"),
		KafkaConnectKafkaPassword:          commonConfig.GetEnv("INFRA_KAFKA_CONNECT_PASSWORD", "addp_kafka_connect"),
		KafkaConnectKafkaSecurityProtocol:  commonConfig.GetEnv("KAFKA_CONNECT_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		KafkaConnectKafkaSASLMechanism:     commonConfig.GetEnv("INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
		KafkaConnectKafkaTLSCACertFile:     commonConfig.GetEnv("KAFKA_CONNECT_KAFKA_TLS_CA_CERT_FILE", commonConfig.GetEnv("INFRA_KAFKA_TLS_CA_CERT_FILE", "")),
		CaptureProvisioningTimeout:         commonConfig.GetEnvDuration("TRANSFER_CAPTURE_PROVISIONING_TIMEOUT", "60s"),
		CaptureStatusPollInterval:          commonConfig.GetEnvDuration("TRANSFER_CAPTURE_STATUS_POLL_INTERVAL", "1s"),
		CaptureMonitorInterval:             commonConfig.GetEnvDuration("TRANSFER_CAPTURE_MONITOR_INTERVAL", "15s"),
		ContinuousRuntimeStopTimeout:       commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RUNTIME_STOP_TIMEOUT", "45s"),
		ContinuousRuntimeStopPollInterval:  commonConfig.GetEnvDuration("TRANSFER_CONTINUOUS_RUNTIME_STOP_POLL_INTERVAL", "250ms"),
		MetaScanClaimTTL:                   commonConfig.GetEnvDuration("TRANSFER_META_SCAN_CLAIM_TTL", "2m"),
	}

	minioCfg := commonConfig.LoadBuiltinMinIOConfig()
	cfg.BuiltinMinioEndpoint = minioCfg.Endpoint
	cfg.BuiltinMinioAccessKey = minioCfg.AccessKey
	cfg.BuiltinMinioSecretKey = minioCfg.SecretKey
	cfg.BuiltinMinioUseSSL = minioCfg.UseSSL

	// 设置 BaseConfig 字段
	cfg.SystemServiceURL = systemURL

	commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)

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
		mechanism := strings.ToLower(strings.TrimSpace(c.InfraKafkaSASLMechanism))
		if mechanism == "" {
			mechanism = "scram-sha-256"
		}
		if mechanism != "scram-sha-256" {
			return nil, fmt.Errorf("Infra Kafka SASL mechanism must be scram-sha-256, got %q", mechanism)
		}
		info["sasl_mechanism"] = mechanism
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
