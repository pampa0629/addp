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
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/meta/internal/config"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/service"
	"github.com/addp/meta/internal/worker"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func main() {
	commonConfig.LoadEnv()
	cfg := config.LoadConfig()
	logger.Init(logger.Options{Level: cfg.LogLevel, Format: cfg.LogFormat, FilePath: filepath.Join("logs", "meta-worker.log"), AddSource: cfg.LogAddSource, RedirectStdLog: true})

	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-meta", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("初始化 addp-meta Service Principal 凭据失败: %v", err)
	}
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, tokenSource, nil)
	engineService := service.NewEngineService(db, systemClient)
	scanService, _, err := service.NewRuntimeScanService(db, engineService, cfg)
	if err != nil {
		log.Fatalf("扫描运行时初始化失败: %v", err)
	}

	var redisClient *redis.Client
	if cfg.RedisHost != "" && cfg.RedisPort != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr: fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort), Password: cfg.RedisPassword, DB: cfg.RedisDB,
		})
		defer redisClient.Close()
	}
	executionService := service.NewScanExecutionService(db, scanService, engineService, redisClient)

	hostname, _ := os.Hostname()
	runner, err := worker.NewBoundedRunner(executionService, worker.BoundedRunnerConfig{
		WorkerID:    fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString()),
		Concurrency: cfg.ConcurrentTasks, LeaseDuration: cfg.BoundedLeaseDuration,
		HeartbeatInterval: cfg.BoundedHeartbeatInterval, ClaimInterval: cfg.BoundedClaimInterval,
	}, logger.With("component", "meta_bounded_runner"))
	if err != nil {
		log.Fatalf("Meta bounded worker 配置无效: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.L().Info("meta bounded worker started", "concurrency", cfg.ConcurrentTasks, "lease", cfg.BoundedLeaseDuration)
	runner.Run(ctx)
	logger.L().Info("meta bounded worker stopped")
}

func connectDatabase(cfg *config.Config) (*gorm.DB, error) {
	if err := metaRepo.PrepareSchema(cfg); err != nil {
		return nil, err
	}
	db, err := commonRepo.InitDatabase(commonRepo.DatabaseConfig{
		Host: cfg.DBHost, Port: cfg.DBPort, User: cfg.DBUser, Password: cfg.DBPassword,
		DBName: cfg.DBName, Schema: cfg.DBSchema, SSLMode: "disable",
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	return db, nil
}
