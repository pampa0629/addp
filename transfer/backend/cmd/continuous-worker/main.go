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
	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/continuous"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
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
	if cfg.SystemServiceURL == "" || cfg.InternalAPIKey == "" {
		log.Fatal("continuous worker 需要 SYSTEM_URL 和 INTERNAL_API_KEY 解析业务 Kafka/PostgreSQL Engine")
	}
	owner := cfg.ContinuousWorkerInstanceID
	if owner == "" {
		hostname, _ := os.Hostname()
		owner = fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString())
	}
	leaseRepo := repository.NewRuntimeLeaseRepository(db)
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	runner := &continuous.DataSessionRunner{
		Resolver: planner.NewSystemEngineResolver(systemClient),
		States:   repository.NewSyncStateRepository(db), Progress: leaseRepo,
		PollTimeout: cfg.ContinuousPollTimeout, MaxBytes: cfg.ContinuousFetchMaxBytes,
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
	logger.L().Info("transfer continuous worker starting", "owner_instance_id", owner, "capacity", cfg.ContinuousWorkerCapacity, "data_plane", "kafka_to_postgresql")
	if err := supervisor.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("continuous supervisor 退出: %v", err)
	}
}

func connectDatabase(cfg *config.Config) (*gorm.DB, error) {
	return commonRepo.InitDatabase(commonRepo.DatabaseConfig{
		Host: cfg.DBHost, Port: cfg.DBPort, User: cfg.DBUser, Password: cfg.DBPassword,
		DBName: cfg.DBName, Schema: cfg.DBSchema, SSLMode: "disable",
	})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
