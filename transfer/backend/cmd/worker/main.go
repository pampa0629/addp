package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	commonExecution "github.com/addp/common/execution"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/continuous"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/worker"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func main() {
	commonConfig.LoadEnv()
	cfg := config.Load()
	logger.Init(logger.Options{Level: envOr("LOG_LEVEL", "info"), Format: "json", FilePath: filepath.Join("logs", "transfer-bounded-worker.log"), AddSource: true, RedirectStdLog: true})

	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	taskRepo := repository.NewTaskRepository(db)
	executionRepo := commonExecution.NewTaskExecutionRepository(db)
	executionService := service.NewExecutionService(db, executionRepo)

	if cfg.MetaServiceURL == "" || cfg.ServiceClientSecret == "" {
		log.Fatal("META_URL 和 TRANSFER_SERVICE_CLIENT_SECRET 必须配置")
	}
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-transfer", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	metaClient := commonClient.NewMetaClient(cfg.MetaServiceURL, tokenSource)
	systemRuntimeClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, tokenSource, nil)
	systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, tokenSource)
	executionEngineService := service.NewExecutionEngineService(taskRepo, repository.NewSyncStateRepository(db), executionService, systemClient, systemRuntimeClient, metaClient)
	executionEngineService.SetConfig(cfg)
	executionEngineService.SetReplayRuntime(continuous.NewReplayRuntime(continuous.BoundedReplayRunner{
		PollTimeout: cfg.ContinuousPollTimeout, MaxBytes: cfg.ContinuousFetchMaxBytes,
		AssertTargetAbsent: continuous.NewReplayTargetAbsenceValidator(nil),
	}))
	taskService := service.NewTaskService(db, executionEngineService, cfg)

	hostname, _ := os.Hostname()
	runner, err := worker.NewBoundedRunner(taskRepo, taskService, executionService, worker.BoundedRunnerConfig{
		WorkerID:    fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString()),
		Concurrency: cfg.BoundedWorkerConcurrency, LeaseDuration: cfg.BoundedLeaseDuration,
		HeartbeatInterval: cfg.BoundedHeartbeatInterval, ClaimInterval: cfg.BoundedClaimInterval,
	}, logger.With("component", "transfer_bounded_runner"))
	if err != nil {
		log.Fatalf("Transfer bounded worker 配置无效: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.L().Info("transfer bounded worker started", "concurrency", cfg.BoundedWorkerConcurrency, "lease", cfg.BoundedLeaseDuration)
	runner.Run(ctx)
	logger.L().Info("transfer bounded worker stopped")
}

func connectDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := commonRepo.InitDatabase(commonRepo.DatabaseConfig{
		Host: cfg.DBHost, Port: cfg.DBPort, User: cfg.DBUser, Password: cfg.DBPassword,
		DBName: cfg.DBName, Schema: cfg.DBSchema, SSLMode: "disable",
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	return db, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
