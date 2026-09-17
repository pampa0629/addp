package main

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/addp/common/schema"
	"github.com/addp/quality/internal/migration"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	commonRuntimeHealth "github.com/addp/common/runtimehealth"
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
	if err := schema.Require(db, "quality", migration.SchemaVersion); err != nil {
		log.Fatalf("Worker schema is not ready: %v", err)
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-quality", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)
	planRepo := repository.NewPlanRepository(db)
	executor := service.NewCheckExecutor(systemServiceClient, planRepo, cfg.WorkerConcurrency)
	if err := executor.ConfigureWorker(cfg.WorkerLease, cfg.WorkerPoll); err != nil {
		log.Fatalf("Quality worker 配置无效: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	registrationDone := systemServiceClient.RegisterAndHeartbeat(ctx, &commonClient.ModuleRegistrationRequest{
		ModuleName: commonExecution.ModuleQuality, InstanceID: executor.WorkerID(),
		Role: commonClient.ModuleRuntimeRoleWorker, RoutePrefix: "/quality",
		Metadata: map[string]interface{}{
			"runtime_name": "quality-bounded",
			"capacity":     cfg.WorkerConcurrency,
		},
	})
	modulelifecycle.CancelRuntimeOnFatal(registrationDone, stop)
	reporter, err := commonRuntimeHealth.NewReporter(commonRuntimeHealth.NewRepository(db), commonRuntimeHealth.ReporterConfig{
		InstanceID: executor.WorkerID(), Module: commonExecution.ModuleQuality, Role: commonRuntimeHealth.RoleExecutionWorker,
		RuntimeName: "quality-bounded", Capacity: cfg.WorkerConcurrency,
		Interval: commonRuntimeHealth.DefaultInterval, TTL: commonRuntimeHealth.DefaultTTL,
		ActiveCount: executor.ActiveCount, Logger: slog.Default(),
	})
	if err != nil {
		log.Fatalf("Quality worker heartbeat config is invalid: %v", err)
	}
	go reporter.Run(ctx)
	executor.StartWorker(ctx, registrationDone.IsRegistered)
	log.Printf("Quality worker started: concurrency=%d lease=%s", cfg.WorkerConcurrency, cfg.WorkerLease)
	<-ctx.Done()
	executor.StopWorker()
	<-registrationDone.Done()
	log.Printf("Quality worker stopped")
}
