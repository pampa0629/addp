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
	"github.com/addp/common/dbbridge"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	commonRuntimeHealth "github.com/addp/common/runtimehealth"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/addp/develop/backend/internal/service"
	"github.com/addp/develop/backend/internal/worker"
	"github.com/google/uuid"
)

func main() {
	commonConfig.LoadEnv()
	cfg := config.Load()
	logger.Init(logger.Options{
		Level: envOr("LOG_LEVEL", "info"), Format: "json",
		FilePath: filepath.Join("logs", "develop-query-worker.log"), AddSource: true, RedirectStdLog: true,
	})
	if cfg.SystemServiceURL == "" || cfg.MetaServiceURL == "" || cfg.ServiceClientSecret == "" {
		log.Fatal("SYSTEM_URL、META_URL 和 DEVELOP_SERVICE_CLIENT_SECRET 必须配置")
	}
	db, err := repository.ConnectDatabase(cfg)
	if err != nil {
		log.Fatalf("Develop Query Worker 数据库连接失败: %v", err)
	}
	defer dbbridge.CloseAllPools()

	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemServiceURL, "addp-develop", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Develop Service Token Source 初始化失败: %v", err)
	}
	systemService := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, tokenSource, nil)
	executionAuthorizationClient := commonClient.NewSystemExecutionAuthorizationClient(cfg.SystemServiceURL, nil)
	metaClient := commonClient.NewMetaClient(cfg.MetaServiceURL, tokenSource)
	queryPolicyService := service.NewQueryPolicyService(repository.NewQueryPolicyRepository(db))
	sqlEngine := service.NewSQLEngineService(cfg, systemService, executionAuthorizationClient, queryPolicyService)
	federatedQuery := service.NewFederatedQueryService(systemService, metaClient)
	executionRepo := commonExecution.NewTaskExecutionRepository(db)
	devExecutor := service.NewDevExecutor(
		repository.NewDevTaskRepository(db), executionRepo, nil, nil, metaClient,
		sqlEngine, federatedQuery, nil, cfg.QueryResultLimit,
	)
	queryRepo := repository.NewQueryExecutionRepository(db)
	queryService, err := service.NewQueryWorkerService(devExecutor, queryRepo)
	if err != nil {
		log.Fatalf("Develop Query Worker 服务初始化失败: %v", err)
	}

	hostname, _ := os.Hostname()
	instanceID := fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString())
	runner, err := worker.NewQueryRunner(queryRepo, queryService, worker.QueryRunnerConfig{
		WorkerID: instanceID, Concurrency: cfg.QueryWorkerConcurrency,
		LeaseDuration: cfg.QueryLeaseDuration, HeartbeatInterval: cfg.QueryHeartbeatInterval,
		ClaimInterval: cfg.QueryClaimInterval,
	}, logger.With("component", "develop_query_runner"))
	if err != nil {
		log.Fatalf("Develop Query Worker 配置无效: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	registration := systemService.RegisterAndHeartbeat(ctx, &commonClient.ModuleRegistrationRequest{
		ModuleName: commonExecution.ModuleDevelop, InstanceID: instanceID,
		Role: commonClient.ModuleRuntimeRoleWorker, RoutePrefix: "/develop",
		Metadata: map[string]interface{}{
			"runtime_name": commonExecution.TaskTypeQuery,
			"source":       commonExecution.ModuleOrchestrator,
			"capacity":     cfg.QueryWorkerConcurrency,
		},
	})
	modulelifecycle.CancelRuntimeOnFatal(registration, stop)
	reporter, err := commonRuntimeHealth.NewReporter(
		commonRuntimeHealth.NewRepository(db),
		commonRuntimeHealth.ReporterConfig{
			InstanceID: instanceID, Module: commonExecution.ModuleDevelop,
			Role: commonRuntimeHealth.RoleExecutionWorker, RuntimeName: commonExecution.TaskTypeQuery,
			Capacity: cfg.QueryWorkerConcurrency, Interval: commonRuntimeHealth.DefaultInterval,
			TTL: commonRuntimeHealth.DefaultTTL, ActiveCount: runner.ActiveCount,
			Logger: logger.With("component", "runtime_health"),
		},
	)
	if err != nil {
		log.Fatalf("Develop Query Worker 运行心跳配置无效: %v", err)
	}
	go reporter.Run(ctx)
	logger.L().Info("develop query worker started", "concurrency", cfg.QueryWorkerConcurrency, "lease", cfg.QueryLeaseDuration)
	runner.Run(ctx, registration.IsRegistered)
	<-registration.Done()
	logger.L().Info("develop query worker stopped")
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
