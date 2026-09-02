package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	commonconfiguration "github.com/addp/common/configuration"
	"github.com/addp/common/dataprotection/projectionstore"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/api"
	transferauthorization "github.com/addp/transfer/internal/authorization"
	"github.com/addp/transfer/internal/capture"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/continuous"
	"github.com/addp/transfer/internal/deadletter"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	transferprotection "github.com/addp/transfer/internal/protection"
	transferRepo "github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/worker"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// @title           ADDP Transfer API
// @version         1.0
// @host      localhost:8083
// @BasePath  /api/v1/transfer
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	// 设置本地时区为 Asia/Shanghai (CST)
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		log.Printf("⚠️  无法加载时区，使用系统默认时区: %v", err)
	} else {
		time.Local = loc
		log.Printf("✅ 时区已设置为: %s", loc.String())
	}

	// 加载 .env 文件（从项目根目录，使用无参数版本）
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.Load()
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-transfer", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Service Token Source 配置无效: %v", err)
	}
	systemRuntimeClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, serviceTokenSource, nil)
	systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, serviceTokenSource)

	// 初始化结构化日志
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	logFile := filepath.Join("logs", "transfer-backend.log")
	logger.Init(logger.Options{
		Level:          logLevel,
		Format:         "json",
		FilePath:       logFile,
		AddSource:      true,
		RedirectStdLog: true,
	})
	logger.L().Info("transfer backend starting",
		"version", "0.0.20",
		"log_level", logLevel,
		"log_file", logFile,
	)

	// 检查端口是否可用
	if err := commonConfig.CheckPortAvailable(cfg.Port); err != nil {
		logger.L().Error("端口检查失败", "error", err, "port", cfg.Port)
		os.Exit(1)
	}
	logger.L().Info("端口检查通过", "port", cfg.Port)

	// 连接数据库
	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	continuousPolicyService := service.NewContinuousPolicyService(transferRepo.NewContinuousPolicyRepository(db))
	if err := continuousPolicyService.Apply(context.Background(), cfg); err != nil {
		log.Fatalf("Failed to load continuous policy: %v", err)
	}
	protectionStore, err := projectionstore.New(db, cfg.DBSchema, "transfer", nil)
	if err != nil {
		log.Fatalf("初始化 Transfer 保护投影存储失败: %v", err)
	}
	protectionGate := transferprotection.NewGate(protectionStore, systemClient)
	securityClient := commonClient.NewSecurityClient(cfg.SecurityServiceURL, serviceTokenSource, nil)
	projectionstore.NewRunner(
		protectionStore, securityClient, systemRuntimeClient, 30*time.Second,
		transferprotection.NewExecutionBarrier(db, protectionGate),
	).Start(runtimeContext)

	// 初始化 Repository 层
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db) // 统一执行记录仓库
	log.Printf("✅ Repository 层初始化完成（使用统一执行表）")

	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)

	// 初始化 Redis 客户端（用于资源变更事件同步）
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: cfg.RedisPassword,
		DB:       0, // Transfer 使用 DB 0（与任务队列可以共享）
	})
	log.Printf("✅ Redis 客户端已初始化用于资源事件同步: %s", redisAddr)

	// 初始化 Pipeline 组件（简化版 - 暂时不启动）
	// registry := pipeline.NewConnectorRegistry()
	// if err := connector.RegisterAllConnectors(registry); err != nil {
	// 	log.Fatalf("Failed to register connectors: %v", err)
	// }

	// 初始化 Service 层。HTTP 进程需要 execution engine 解析 replay plan 和采集请求时 retention 快照，
	// 实际 bounded 数据处理仍只由 worker 执行。
	executionService := service.NewExecutionService(db, taskExecutionRepo) // 使用统一执行表
	executionEngineService := service.NewExecutionEngineService(
		transferRepo.NewTaskRepository(db),
		transferRepo.NewSyncStateRepository(db),
		executionService,
		systemClient,
		systemRuntimeClient,
		nil,
	)
	executionEngineService.SetConfig(cfg)
	executionEngineService.SetProtectionGate(protectionGate)
	executionEngineService.SetReplayRuntime(continuous.NewReplayRuntime(continuous.BoundedReplayRunner{
		PollTimeout: cfg.ContinuousPollTimeout, MaxBytes: cfg.ContinuousFetchMaxBytes,
		AssertTargetAbsent: continuous.NewReplayTargetAbsenceValidator(nil),
	}))
	taskService := service.NewTaskService(db, executionEngineService, cfg)
	taskService.SetEngineResolver(planner.NewSystemEngineResolver(systemClient))
	taskService.SetExecutionService(executionService) // 注入执行服务（避免循环依赖）
	cleanupService := service.NewTransferCleanupService(db, redisClient, taskExecutionRepo, service.TaskOwnedCleanupConfig{
		RuntimeStopTimeout: cfg.ContinuousRuntimeStopTimeout, RuntimeStopPollInterval: cfg.ContinuousRuntimeStopPollInterval,
	})
	infraKafkaAdminConnection, err := cfg.InfraKafkaAdminConnectionInfo()
	if err != nil {
		log.Fatalf("初始化 Infra Kafka DLQ cleanup 配置失败: %v", err)
	}
	deadLetterTopicCleaner, err := deadletter.NewKafkaTopicCleaner(deadletter.KafkaTopicCleanerConfig{ConnectionInfo: infraKafkaAdminConnection})
	if err != nil {
		log.Fatalf("初始化 Infra Kafka DLQ topic cleaner 失败: %v", err)
	}
	defer deadLetterTopicCleaner.Close()
	cleanupService.SetDeadLetterTopicCleaner(deadLetterTopicCleaner)
	taskService.SetTaskOwnedResourceCleanup(cleanupService)
	var registration *commonClient.ModuleRegistrationLifecycle
	boundedScheduler := worker.NewScheduler(transferRepo.NewTaskRepository(db))
	boundedScheduler.SetExecutionService(executionService)

	// 数据库 CDC capture control plane；数据面由独立 continuous worker 消费登记的 Infra Kafka generation。
	if systemClient != nil {
		topicAdmin, err := capture.NewKafkaTopicAdmin(capture.KafkaAdminConfig{
			BootstrapServers: cfg.InfraKafkaBootstrapServers,
			Username:         cfg.InfraKafkaAdminUsername, Password: cfg.InfraKafkaAdminPassword,
			SecurityProtocol: cfg.InfraKafkaSecurityProtocol,
			SASLMechanism:    cfg.InfraKafkaSASLMechanism,
			TLSCACertFile:    cfg.InfraKafkaTLSCACertFile, TLSInsecure: cfg.InfraKafkaTLSInsecure,
		})
		if err != nil {
			log.Fatalf("初始化 Infra Kafka capture admin 失败: %v", err)
		}
		defer topicAdmin.Close()
		connectClient, err := capture.NewConnectClient(cfg.KafkaConnectURL, cfg.KafkaConnectUsername, cfg.KafkaConnectPassword, cfg.KafkaConnectTimeout)
		if err != nil {
			log.Fatalf("初始化 Kafka Connect client 失败: %v", err)
		}
		capturePlanResolver := capture.NewDatabasePlanResolver(planner.NewSystemEngineResolver(systemClient))
		captureSupervisor, err := capture.NewSupervisor(
			transferRepo.NewCaptureRepository(db),
			capturePlanResolver,
			connectClient, topicAdmin, capture.DatabaseSourceResources{},
			capture.SupervisorConfig{
				TopicRetention: cfg.CaptureTopicRetention, TopicRetentionBytes: cfg.CaptureTopicRetentionBytes,
				TopicReplication: int16(cfg.CaptureTopicReplicationFactor), ConnectLoopbackHost: cfg.KafkaConnectLoopbackHost,
				ConnectBootstrapServers: cfg.KafkaConnectBootstrapServers,
				ConnectKafkaUsername:    cfg.KafkaConnectKafkaUsername, ConnectKafkaPassword: cfg.KafkaConnectKafkaPassword,
				ConnectKafkaSecurityProtocol: cfg.KafkaConnectKafkaSecurityProtocol,
				ConnectKafkaSASLMechanism:    cfg.KafkaConnectKafkaSASLMechanism,
				ConnectKafkaTLSCACertFile:    cfg.KafkaConnectKafkaTLSCACertFile,
				ProvisioningTimeout:          cfg.CaptureProvisioningTimeout, StatusPollInterval: cfg.CaptureStatusPollInterval,
				MonitorInterval: cfg.CaptureMonitorInterval, SourceProbeTimeout: cfg.KafkaConnectTimeout,
			},
			logger.With("component", "capture_supervisor"),
		)
		if err != nil {
			log.Fatalf("初始化 Transfer capture supervisor 失败: %v", err)
		}
		taskService.SetCaptureControl(captureSupervisor)
		taskService.SetSchemaChangeInspector(capturePlanResolver)
		cleanupService.SetCaptureControl(captureSupervisor)
		go func() {
			if err := captureSupervisor.Run(runtimeContext); err != nil && err != context.Canceled {
				logger.L().Error("Transfer capture supervisor 已退出", "error", err)
			}
		}()
	}
	if err := cleanupService.Start(runtimeContext); err != nil {
		logger.L().Warn("Transfer 资源回收服务启动失败", "error", err)
	}
	defer cleanupService.Stop()

	// 设置路由
	lifecycleController := modulelifecycle.NewBusiness("transfer", commonClient.ModuleRuntimeRoleBackend)
	fieldDefinitionRecommendationService := service.NewFieldDefinitionRecommendationService(systemClient, protectionGate)
	router := api.SetupRouter(taskService, executionService, continuousPolicyService, fieldDefinitionRecommendationService, cfg.SystemServiceURL, cfg.MetaServiceURL, redisClient, systemClient, systemRuntimeClient, lifecycleController)
	addr := fmt.Sprintf(":%s", cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Transfer 监听绑定失败: %v", err)
	}

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	// ========== 模块定义、运行实例与 TaskProvider 声明发布 ==========
	if systemRuntimeClient != nil {
		taskProvider, err := service.TransferTaskProviderDeclaration()
		if err != nil {
			log.Fatalf("构建 Transfer TaskProvider 声明失败: %v", err)
		}
		registration = systemRuntimeClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
			ModuleName: "transfer", ModuleURL: serviceURL, RoutePrefix: "/transfer", HealthCheckURL: serviceURL + "/health/ready",
			TaskProvider: taskProvider,
			Metadata: map[string]interface{}{
				"module": "transfer",
				"capabilities": map[string]interface{}{
					"cleanup_executor": map[string]interface{}{
						"enabled": true,
						"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
					},
				},
			},
			ConfigurationManagement: &commonconfiguration.ManagementDeclaration{SchemaVersion: commonconfiguration.ManagementSchemaVersion, Entries: []commonconfiguration.ManagementEntry{{
				ID: "transfer.configuration", OwnerModule: "transfer", ScopeTypes: []string{commonconfiguration.ScopePlatformOnly}, FrontendRoute: "/configuration/transfer",
				ReadPermission: transferauthorization.PermissionTransferConfigurationRead, UpdatePermission: transferauthorization.PermissionTransferConfigurationUpdate,
			}}},
		})
		lifecycleController.AttachRegistration(registration)
		modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)
	}
	boundedScheduler.SetClaimGate(func() bool { return registration != nil && registration.IsRegistered() })
	if err := boundedScheduler.Start(runtimeContext); err != nil {
		log.Fatalf("Transfer owner scheduler 启动失败: %v", err)
	}
	defer boundedScheduler.Stop()

	// 启动服务器
	log.Printf("🚀 Transfer service starting on %s", addr)
	log.Printf("📊 Database: %s@%s:%s/%s (schema: %s)",
		cfg.DBUser, cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBSchema)
	log.Printf("🔗 System Service: %s", cfg.SystemServiceURL)
	log.Printf("✅ Readiness check: http://localhost%s/health/ready", addr)

	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Failed to start server: %v", err)
			stopRuntime()
		}
	}()
	<-runtimeContext.Done()
	if registration != nil {
		<-registration.Done()
	}
}

// connectDatabase 连接数据库
func connectDatabase(cfg *config.Config) (*gorm.DB, error) {
	// Use common repository InitDatabase
	dbConfig := commonRepo.DatabaseConfig{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		Schema:   cfg.DBSchema,
		SSLMode:  "disable",
	}

	// Connect first so the one-way capture schema split can run before AutoMigrate
	// attempts to enforce the new non-null source_type column.
	db, err := commonRepo.InitDatabase(dbConfig)
	if err != nil {
		return nil, err
	}
	if err := transferRepo.MigrateCaptureProviderResources(db); err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(transferSchemaModels()...); err != nil {
		return nil, fmt.Errorf("auto-migrate transfer models: %w", err)
	}

	return db, nil
}

func transferSchemaModels() []interface{} {
	return []interface{}{
		&models.TransferTask{},
		&models.SyncState{},
		&models.RuntimeLease{},
		&models.CaptureResource{},
		&models.PostgreSQLCaptureResource{},
		&models.MySQLCaptureResource{},
		&models.OracleCaptureResource{},
		&models.SchemaChangeRequest{},
		&models.DeadLetter{},
		&models.ContinuousPolicy{},
	}
}
