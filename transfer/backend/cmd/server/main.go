package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/common/utils"
	"github.com/addp/transfer/internal/api"
	"github.com/addp/transfer/internal/capture"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/continuous"
	"github.com/addp/transfer/internal/deadletter"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
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
	if cfg.EnableIntegration {
		if _, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-transfer", cfg.ServiceClientSecret, nil); err != nil {
			log.Fatalf("Service Token Source 配置无效: %v", err)
		}
	}

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
	if err := utils.CheckPortAvailable(cfg.Port); err != nil {
		logger.L().Error("端口检查失败", "error", err, "port", cfg.Port)
		os.Exit(1)
	}
	logger.L().Info("端口检查通过", "port", cfg.Port)

	// 连接数据库
	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 初始化 Repository 层
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db) // 统一执行记录仓库
	log.Printf("✅ Repository 层初始化完成（使用统一执行表）")

	// 创建任务队列（连接 Redis）
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	taskQueue := worker.NewTaskQueue(redisAddr, cfg.RedisPassword)
	defer taskQueue.Close()

	log.Printf("✅ Task queue connected to Redis: %s", redisAddr)

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

	// 初始化 System 客户端（用于审计日志、Engine 解析和服务间调用）
	var systemClient *commonClient.SystemClient
	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
		log.Printf("✅ SystemClient 已初始化: %s", cfg.SystemServiceURL)
	}

	// 初始化 Service 层。HTTP 进程需要 execution engine 解析 replay plan 和采集请求时 retention 快照，
	// 实际 bounded 数据处理仍只由 worker 执行。
	executionService := service.NewExecutionService(db, taskExecutionRepo) // 使用统一执行表
	executionService.SetTaskQueue(taskQueue)
	executionEngineService := service.NewExecutionEngineService(
		transferRepo.NewTaskRepository(db),
		transferRepo.NewSyncStateRepository(db),
		executionService,
		systemClient,
		nil,
	)
	executionEngineService.SetConfig(cfg)
	executionEngineService.SetReplayRuntime(continuous.NewReplayRuntime(continuous.BoundedReplayRunner{
		PollTimeout: cfg.ContinuousPollTimeout, MaxBytes: cfg.ContinuousFetchMaxBytes,
		AssertTargetAbsent: continuous.NewReplayTargetAbsenceValidator(nil),
	}))
	taskService := service.NewTaskService(db, executionEngineService, cfg, taskQueue)
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
				MonitorInterval: cfg.CaptureMonitorInterval,
			},
			logger.With("component", "capture_supervisor"),
		)
		if err != nil {
			log.Fatalf("初始化 Transfer capture supervisor 失败: %v", err)
		}
		taskService.SetCaptureControl(captureSupervisor)
		taskService.SetSchemaChangeInspector(capturePlanResolver)
		cleanupService.SetCaptureControl(captureSupervisor)
		captureCtx, cancelCapture := context.WithCancel(context.Background())
		defer cancelCapture()
		go func() {
			if err := captureSupervisor.Run(captureCtx); err != nil && err != context.Canceled {
				logger.L().Error("Transfer capture supervisor 已退出", "error", err)
			}
		}()
	}
	if err := cleanupService.Start(context.Background()); err != nil {
		logger.L().Warn("Transfer 资源回收服务启动失败", "error", err)
	}
	defer cleanupService.Stop()

	// 设置路由
	router := api.SetupRouter(taskService, executionService, cfg.SystemServiceURL, cfg.MetaServiceURL, redisClient, systemClient)

	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("transfer")
	serviceURL := utils.BuildServiceURL(serviceHost, port)

	// ========== 模块注册（注册到 System service_registry）==========
	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		registryClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
		registryClient.RegisterAndHeartbeatWithMetadata("transfer", serviceURL, "/transfer", map[string]interface{}{
			"module": "transfer",
			"capabilities": map[string]interface{}{
				"cleanup_executor": map[string]interface{}{
					"enabled": true,
					"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
				},
			},
		})
	}

	// ========== 任务提供者注册（启动时自动注册到 System task_providers）==========
	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		taskProviderRegistry := service.NewTaskProviderRegistryService(
			cfg.SystemServiceURL,
			cfg.InternalAPIKey,
			serviceURL,
		)

		// 后台异步注册（不阻塞启动，支持重试）
		go func() {
			time.Sleep(2 * time.Second) // 等待服务完全启动
			maxRetries := 5
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := taskProviderRegistry.Register(); err != nil {
					log.Printf("⚠️  任务提供者注册失败 (尝试 %d/%d): %v", attempt, maxRetries, err)
					time.Sleep(time.Duration(attempt*2) * time.Second) // 指数退避
					continue
				}
				log.Printf("✅ Transfer 模块已注册到 task_providers")
				return
			}
			log.Printf("❌ 任务提供者注册失败（已达最大重试次数：%d）", maxRetries)
		}()
	}

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🚀 Transfer service starting on %s", addr)
	log.Printf("📊 Database: %s@%s:%s/%s (schema: %s)",
		cfg.DBUser, cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBSchema)
	log.Printf("🔗 System Service: %s", cfg.SystemServiceURL)
	log.Printf("✅ Health check: http://localhost%s/health", addr)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
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
		&models.SchemaChangeRequest{},
		&models.DeadLetter{},
	}
}
