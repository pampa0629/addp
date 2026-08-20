package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/config"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-quality", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)
	executor := service.NewCheckExecutor(
		systemServiceClient,
		nil,
		repository.NewCheckTaskRepository(db),
		repository.NewIssueRepository(db),
		cfg.CheckTimeout,
		cfg.WorkerConcurrency,
	)
	if err := executor.ConfigureWorker(cfg.WorkerLease, cfg.WorkerPoll); err != nil {
		log.Fatalf("Quality worker 配置无效: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	executor.StartWorker(ctx)
	log.Printf("Quality worker started: concurrency=%d lease=%s", cfg.WorkerConcurrency, cfg.WorkerLease)
	<-ctx.Done()
	executor.StopWorker()
	log.Printf("Quality worker stopped")
}
