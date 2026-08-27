package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/meta/internal/api"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/service"
	"github.com/redis/go-redis/v9"

	// 导入 general 引擎插件以触发自动注册
	_ "github.com/addp/common/engine/plugins/builtin/general"

	// 导入 format 解析器以触发自动注册（图片、PDF、CSV 等）
	_ "github.com/addp/common/format/builtin"
)

// @title           ADDP Meta API
// @version         1.0
// @host      localhost:8082
// @BasePath  /api/v1/meta
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.LoadConfig()

	// 重新初始化日志（支持动态级别/格式，并写入日志文件）
	commonConfig.InitLogger("meta-backend.log", &commonConfig.LoggerOptions{
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		AddSource: &cfg.LogAddSource,
		File:      cfg.LogFile,
	})

	// 检查端口是否可用
	if err := commonConfig.CheckPortAvailable(cfg.ServerPort); err != nil {
		logger.L().Error("端口检查失败", "error", err, "port", cfg.ServerPort)
		os.Exit(1)
	}

	logger.L().Info("Meta 服务配置加载完成",
		"port", cfg.ServerPort,
		"db_host", cfg.DBHost,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
	)

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	logger.L().Info("数据库连接初始化完成")
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemServiceURL,
		"addp-meta",
		cfg.ServiceClientSecret,
		nil,
	)
	if err != nil {
		logger.L().Error("初始化 addp-meta Service Principal 凭据失败", "error", err)
		os.Exit(1)
	}
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, tokenSource, nil)
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	var registration *commonClient.ModuleRegistrationLifecycle

	// 初始化 Redis 客户端（可选，仅用于事件与扫描范围锁，不承担有界执行队列职责）
	var redisClient *redis.Client
	if cfg.RedisHost != "" && cfg.RedisPort != "" {
		redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		logger.L().Info("Redis 客户端已初始化", "addr", redisAddr)

	} else {
		logger.L().Warn("Redis 未配置，资源事件同步与扫描范围锁不可用")
	}

	// 初始化服务
	engineService := service.NewEngineService(db, systemClient)

	contentIndexClient := commonClient.NewManagerContentClient(cfg.ManagerServiceURL, tokenSource, nil)
	scanService := service.NewRuntimeScanService(db, engineService, cfg, contentIndexClient)

	// 初始化扫描事件发布器（如果 Redis 可用）
	if redisClient != nil {
		scanEventPublisher := events.NewScanEventPublisher(redisClient, logger.L())
		scanService.SetScanEventPublisher(scanEventPublisher)
		logger.L().Info("扫描事件发布器已初始化")
	}

	taskService := service.NewScanTaskService(db)
	executionService := service.NewScanExecutionService(db, scanService, engineService, redisClient)
	lineageService := service.NewLineageService(db, engineService)
	lineageContext, cancelLineage := context.WithCancel(runtimeContext)
	defer cancelLineage()
	go lineageService.RunCollector(lineageContext, time.Minute)

	scheduler := service.NewScanTaskScheduler(taskService, executionService)

	// ========== 启动 Meta 资源回收服务 ==========
	cleanupService := service.NewCleanupService(db, redisClient, systemClient, contentIndexClient, service.CleanupConfig{
		Enabled:         true,
		RetentionDays:   90,
		CleanupInterval: 24 * time.Hour,
	})
	if err := cleanupService.Start(runtimeContext); err != nil {
		logger.L().Error("Meta 资源回收服务启动失败", "error", err)
		os.Exit(1)
	}
	defer cleanupService.Stop(context.Background())
	logger.L().Info("Meta 资源回收服务已启动", "retention_days", 90)

	// 订阅资源回收事件（如果 Redis 可用）
	if redisClient != nil {
		if err := cleanupService.SubscribeCleanupEvents(runtimeContext); err != nil {
			logger.L().Warn("订阅资源回收事件失败", "error", err)
		} else {
			logger.L().Info("已订阅资源回收事件")
		}
	}
	// ===================================

	engineSyncService := service.NewEngineSyncService(redisClient, engineService)
	engineSyncService.Start()
	defer engineSyncService.Stop()

	// 设置路由
	lifecycleController := modulelifecycle.NewBusiness("meta", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(cfg, db, engineService, scanService, taskService, executionService, redisClient, systemClient, lifecycleController, lineageService)
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logger.L().Error("Meta HTTP 监听绑定失败", "error", err, "addr", addr)
		return
	}

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.ServerPort)

	// ========== 模块定义、运行实例与 TaskProvider 声明发布 ==========
	taskProvider, err := service.TaskProviderDeclaration()
	if err != nil {
		logger.L().Error("构建 TaskProvider 声明失败", "error", err)
		os.Exit(1)
	}
	registration = systemClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
		ModuleName: "meta", ModuleURL: serviceURL, RoutePrefix: "/meta", HealthCheckURL: serviceURL + "/health/ready",
		TaskProvider: taskProvider,
		Metadata: map[string]interface{}{
			"module": "meta",
			"capabilities": map[string]interface{}{
				"cleanup_executor": map[string]interface{}{
					"enabled": true,
					"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
				},
			},
		},
	})
	lifecycleController.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)
	scheduler.SetClaimGate(registration.IsRegistered)
	if err := scheduler.Start(runtimeContext); err != nil {
		logger.L().Error("扫描任务调度器启动失败", "error", err)
		os.Exit(1)
	}
	defer scheduler.Stop(context.Background())

	// 启动服务器
	logger.L().Info("Meta 服务启动", "addr", addr)

	go func() {
		if err := router.RunListener(listener); err != nil {
			logger.L().Error("HTTP 服务启动失败", "error", err)
			stopRuntime()
		}
	}()
	<-runtimeContext.Done()
	<-registration.Done()
}
