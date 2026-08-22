package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	commonRuntimeHealth "github.com/addp/common/runtimehealth"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/continuous"
	"github.com/addp/transfer/internal/deadletter"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func main() {
	commonConfig.LoadEnv()
	cfg := config.Load()
	logger.Init(logger.Options{Level: envOr("LOG_LEVEL", "info"), Format: "json", FilePath: filepath.Join("logs", "transfer-continuous-worker.log"), AddSource: true, RedirectStdLog: true})
	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("continuous worker 连接 Infra PostgreSQL 失败: %v", err)
	}
	if err := commonRuntimeHealth.EnsureStore(db); err != nil {
		log.Fatalf("初始化后台运行实例心跳失败: %v", err)
	}
	continuousPolicyService := service.NewContinuousPolicyService(repository.NewContinuousPolicyRepository(db))
	if err := continuousPolicyService.Apply(context.Background(), cfg); err != nil {
		log.Fatalf("continuous worker 加载持续同步策略失败: %v", err)
	}
	if err := cfg.ValidateContinuousRuntime(); err != nil {
		log.Fatalf("continuous worker 配置无效: %v", err)
	}
	if cfg.SystemServiceURL == "" {
		log.Fatal("continuous worker 需要 SYSTEM_URL 解析业务 Kafka/PostgreSQL Engine")
	}
	owner := cfg.ContinuousWorkerInstanceID
	if owner == "" {
		hostname, _ := os.Hostname()
		owner = fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString())
	}
	leaseRepo := repository.NewRuntimeLeaseRepository(db, repository.ContinuousRecoveryPolicy{
		InitialBackoff: cfg.ContinuousRecoveryInitialBackoff, MaxBackoff: cfg.ContinuousRecoveryMaxBackoff,
		MaxFailures: cfg.ContinuousRecoveryMaxFailures, CircuitOpenTime: cfg.ContinuousRecoveryCircuitOpenTime,
		StabilityWindow: cfg.ContinuousRecoveryStabilityWindow,
	})
	infraKafkaConnection, err := cfg.InfraKafkaTransferConnectionInfo()
	if err != nil {
		log.Fatalf("continuous worker Infra Kafka 配置无效: %v", err)
	}
	deadLetterWriter, err := deadletter.NewKafkaPayloadWriter(deadletter.KafkaWriterConfig{
		ConnectionInfo:    infraKafkaConnection,
		RetentionMillis:   int64(cfg.DeadLetterTopicRetention / time.Millisecond),
		RetentionBytes:    cfg.DeadLetterTopicRetentionBytes,
		ReplicationFactor: int16(cfg.DeadLetterTopicReplicationFactor),
	})
	if err != nil {
		log.Fatalf("continuous worker DLQ Kafka writer 配置无效: %v", err)
	}
	defer deadLetterWriter.Close()
	deadLetterRepo := repository.NewDeadLetterRepository(db)
	deadLetterRecorder, err := deadletter.NewRecorder(deadLetterWriter, deadLetterRepo)
	if err != nil {
		log.Fatalf("continuous worker DLQ recorder 配置无效: %v", err)
	}
	deadLetterProbe, err := deadletter.NewKafkaPayloadAvailabilityProbe(deadletter.KafkaPayloadProbeConfig{
		ConnectionInfo: infraKafkaConnection,
		FetchMaxBytes:  cfg.DeadLetterReconcileFetchMaxBytes,
	})
	if err != nil {
		log.Fatalf("continuous worker DLQ availability probe 配置无效: %v", err)
	}
	deadLetterReconciler, err := deadletter.NewPayloadAvailabilityReconciler(
		deadLetterRepo,
		deadLetterProbe,
		deadletter.PayloadAvailabilityReconcilerConfig{
			Interval: cfg.DeadLetterReconcileInterval, BatchSize: cfg.DeadLetterReconcileBatchSize,
			Timeout: cfg.DeadLetterReconcileTimeout,
		},
		logger.With("component", "dead_letter_availability_reconciler"),
	)
	if err != nil {
		log.Fatalf("continuous worker DLQ availability reconciler 配置无效: %v", err)
	}
	if cfg.MetaServiceURL == "" || cfg.ServiceClientSecret == "" {
		log.Fatal("continuous worker 需要 META_URL 和 TRANSFER_SERVICE_CLIENT_SECRET 提交目标元数据扫描")
	}
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-transfer", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("continuous worker Service Token Source 初始化失败: %v", err)
	}
	metaClient := commonClient.NewMetaClient(cfg.MetaServiceURL, tokenSource)
	systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, tokenSource)
	systemRuntimeClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, tokenSource, nil)
	metadataScanner := &continuous.TargetMetadataScanner{
		Store: leaseRepo, Client: metaClient, ClaimTTL: cfg.MetaScanClaimTTL,
		Logger: logger.With("component", "continuous_target_metadata_scan"),
	}
	runner := &continuous.DataSessionRunner{
		Resolver: planner.NewSystemEngineResolver(systemClient),
		States:   repository.NewSyncStateRepository(db), Progress: leaseRepo,
		Captures: repository.NewCaptureRepository(db), InfraKafkaConnection: infraKafkaConnection,
		DeadLetters: deadLetterRecorder,
		PollTimeout: cfg.ContinuousPollTimeout, MaxBytes: cfg.ContinuousFetchMaxBytes,
		DiagnosticsInterval:      cfg.ContinuousDiagnosticsInterval,
		RetentionDegradedHorizon: cfg.ContinuousRetentionDegradedHorizon,
		RetentionCriticalHorizon: cfg.ContinuousRetentionCriticalHorizon,
		CheckpointStaleAfter:     cfg.ContinuousCheckpointStaleAfter,
		MetadataScanner:          metadataScanner,
	}
	supervisor, err := continuous.NewSupervisor(
		leaseRepo,
		runner,
		continuous.Config{
			OwnerInstanceID:   owner,
			Capacity:          cfg.ContinuousWorkerCapacity,
			LeaseDuration:     cfg.ContinuousLeaseDuration,
			HeartbeatInterval: cfg.ContinuousHeartbeatInterval,
			ClaimInterval:     cfg.ContinuousClaimInterval,
		},
		logger.With("component", "continuous_supervisor"),
	)
	if err != nil {
		log.Fatalf("continuous worker 配置无效: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	systemRuntimeClient.RegisterAndHeartbeat(ctx, &commonClient.ModuleRegistrationRequest{
		ModuleName: commonExecution.ModuleTransfer, InstanceID: owner,
		Role: commonClient.ModuleRuntimeRoleWorker, RoutePrefix: "/transfer",
		Metadata: map[string]interface{}{
			"runtime_name": "continuous_sync",
			"capacity":     cfg.ContinuousWorkerCapacity,
		},
	})
	reporter, err := commonRuntimeHealth.NewReporter(commonRuntimeHealth.NewRepository(db), commonRuntimeHealth.ReporterConfig{
		InstanceID: owner, Module: commonExecution.ModuleTransfer, Role: commonRuntimeHealth.RoleContinuousWorker,
		RuntimeName: "continuous_sync", Capacity: cfg.ContinuousWorkerCapacity,
		Interval: commonRuntimeHealth.DefaultInterval, TTL: commonRuntimeHealth.DefaultTTL,
		ActiveCount: supervisor.ActiveCount, Logger: logger.With("component", "runtime_health"),
	})
	if err != nil {
		log.Fatalf("Transfer continuous worker 心跳配置无效: %v", err)
	}
	go reporter.Run(ctx)
	go func() {
		if err := deadLetterReconciler.Run(ctx); err != nil && err != context.Canceled {
			logger.L().Error("DLQ payload availability reconciler 已退出", "error", err)
		}
	}()
	logger.L().Info("transfer continuous worker starting", "owner_instance_id", owner, "capacity", cfg.ContinuousWorkerCapacity, "data_plane", "kafka_or_postgresql_cdc_to_postgresql")
	if err := supervisor.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("continuous supervisor 退出: %v", err)
	}
}

func connectDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := commonRepo.InitDatabase(commonRepo.DatabaseConfig{
		Host: cfg.DBHost, Port: cfg.DBPort, User: cfg.DBUser, Password: cfg.DBPassword,
		DBName: cfg.DBName, Schema: cfg.DBSchema, SSLMode: "disable",
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&models.ContinuousPolicy{}); err != nil {
		return nil, err
	}
	return db, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
