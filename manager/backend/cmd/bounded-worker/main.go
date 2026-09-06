package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	commonRuntimeHealth "github.com/addp/common/runtimehealth"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/addp/manager/internal/worker"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	commonConfig.LoadEnv()
	cfg := config.Load()
	commonConfig.InitLogger("manager-bounded-worker.log", &commonConfig.LoggerOptions{
		Level: cfg.LogLevel, Format: cfg.LogFormat, AddSource: &cfg.LogAddSource, File: cfg.LogFile,
	})
	if cfg.SystemServiceURL == "" || cfg.ServiceClientSecret == "" {
		log.Fatal("SYSTEM_URL 和 MANAGER_SERVICE_CLIENT_SECRET 必须配置")
	}

	db, err := repository.ConnectDatabase(cfg)
	if err != nil {
		log.Fatalf("Manager Bounded Worker 数据库连接失败: %v", err)
	}
	if err := commonRuntimeHealth.EnsureStore(db); err != nil {
		log.Fatalf("初始化后台运行实例心跳失败: %v", err)
	}

	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemServiceURL, "addp-manager", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Manager Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, tokenSource, nil)
	systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, tokenSource)
	workflowRuntimeLister := service.NewWorkflowRuntimeEngineLister(systemServiceClient)
	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds: credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""), Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		log.Fatalf("Manager Bounded Worker MinIO 客户端初始化失败: %v", err)
	}

	const minioBucket = "manager"
	executionService := service.NewPPTXPDFTaskService(repository.NewPPTXPDFRepository(db))
	executionService.SetBucket(minioBucket)
	executionService.SetExecutor(service.NewManagerPPTXPDFExecutor(
		systemClient,
		workflowRuntimeLister,
		minioClient,
		cfg.MinioEndpoint,
		cfg.MinioAccessKey,
		cfg.MinioSecretKey,
		cfg.MinioUseSSL,
		minioBucket,
		cfg.DocumentWorkflowGeneration.Timeout,
	))

	hostname, _ := os.Hostname()
	instanceID := fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString())
	runner, err := worker.NewPPTXPDFRunner(executionService, worker.PPTXPDFRunnerConfig{
		WorkerID: instanceID, Concurrency: cfg.PPTXPDFWorker.Concurrency,
		LeaseDuration: cfg.PPTXPDFWorker.LeaseDuration, HeartbeatInterval: cfg.PPTXPDFWorker.HeartbeatInterval,
		ClaimInterval: cfg.PPTXPDFWorker.ClaimInterval,
	}, logger.With("component", "manager_pptx_pdf_runner"))
	if err != nil {
		log.Fatalf("Manager Bounded Worker 配置无效: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	registration := systemServiceClient.RegisterAndHeartbeat(ctx, &commonClient.ModuleRegistrationRequest{
		ModuleName: commonExecution.ModuleManager, InstanceID: instanceID,
		Role: commonClient.ModuleRuntimeRoleWorker, RoutePrefix: "/manager",
		Metadata: map[string]interface{}{
			"runtime_name": commonExecution.TaskTypePPTXPDFGeneration,
			"capacity":     cfg.PPTXPDFWorker.Concurrency,
		},
	})
	modulelifecycle.CancelRuntimeOnFatal(registration, stop)
	reporter, err := commonRuntimeHealth.NewReporter(
		commonRuntimeHealth.NewRepository(db),
		commonRuntimeHealth.ReporterConfig{
			InstanceID: instanceID, Module: commonExecution.ModuleManager,
			Role: commonRuntimeHealth.RoleExecutionWorker, RuntimeName: commonExecution.TaskTypePPTXPDFGeneration,
			Capacity: cfg.PPTXPDFWorker.Concurrency, Interval: commonRuntimeHealth.DefaultInterval,
			TTL: commonRuntimeHealth.DefaultTTL, ActiveCount: runner.ActiveCount,
			Logger: logger.With("component", "runtime_health"),
		},
	)
	if err != nil {
		log.Fatalf("Manager Bounded Worker 运行心跳配置无效: %v", err)
	}
	go reporter.Run(ctx)
	logger.L().Info("manager bounded worker started", "concurrency", cfg.PPTXPDFWorker.Concurrency, "lease", cfg.PPTXPDFWorker.LeaseDuration)
	runner.Run(ctx, registration.IsRegistered)
	<-registration.Done()
	logger.L().Info("manager bounded worker stopped")
}
