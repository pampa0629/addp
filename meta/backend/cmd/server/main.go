package main

import (
	"context"
	"fmt"
	"os"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
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
	runtimeContext, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()

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

	scanService, searchIndexer, err := service.NewRuntimeScanService(db, engineService, cfg)
	if err != nil {
		logger.L().Error("扫描运行时初始化失败", "error", err)
		os.Exit(1)
	}

	// 初始化扫描事件发布器（如果 Redis 可用）
	if redisClient != nil {
		scanEventPublisher := events.NewScanEventPublisher(redisClient, logger.L())
		scanService.SetScanEventPublisher(scanEventPublisher)
		logger.L().Info("扫描事件发布器已初始化")
	}

	taskService := service.NewScanTaskService(db)
	executionService := service.NewScanExecutionService(db, scanService, engineService, redisClient)
	lineageService := service.NewLineageService(db, engineService)
	lineageContext, cancelLineage := context.WithCancel(context.Background())
	defer cancelLineage()
	go lineageService.RunCollector(lineageContext, time.Minute)

	scheduler := service.NewScanTaskScheduler(taskService, executionService)
	if err := scheduler.Start(context.Background()); err != nil {
		logger.L().Error("扫描任务调度器启动失败", "error", err)
		os.Exit(1)
	}
	defer scheduler.Stop(context.Background())

	// ========== 启动 Meta 资源回收服务 ==========
	cleanupService := service.NewCleanupService(db, redisClient, systemClient, searchIndexer, service.CleanupConfig{
		Enabled:         true,
		RetentionDays:   90,
		CleanupInterval: 24 * time.Hour,
	})
	if err := cleanupService.Start(context.Background()); err != nil {
		logger.L().Error("Meta 资源回收服务启动失败", "error", err)
		os.Exit(1)
	}
	defer cleanupService.Stop(context.Background())
	logger.L().Info("Meta 资源回收服务已启动", "retention_days", 90)

	// 订阅资源回收事件（如果 Redis 可用）
	if redisClient != nil {
		if err := cleanupService.SubscribeCleanupEvents(context.Background()); err != nil {
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
	router := api.SetupRouter(cfg, db, engineService, scanService, taskService, executionService, redisClient, systemClient, lineageService)

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.ServerPort)

	// ========== 模块定义、运行实例与 TaskProvider 声明发布 ==========
	taskProvider, err := service.TaskProviderDeclaration()
	if err != nil {
		logger.L().Error("构建 TaskProvider 声明失败", "error", err)
		os.Exit(1)
	}
	systemClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
		ModuleName: "meta", ModuleURL: serviceURL, RoutePrefix: "/meta", HealthCheckURL: serviceURL + "/health",
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

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	logger.L().Info("Meta 服务启动", "addr", addr)

	if err := router.Run(addr); err != nil {
		logger.L().Error("HTTP 服务启动失败", "error", err)
		os.Exit(1)
	}
}
