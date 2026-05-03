package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/logger"
	"github.com/addp/common/utils"
	"github.com/addp/system/internal/api"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// @title           ADDP System API
// @version         1.0

// @host      localhost:8180
// @BasePath  /api/v1/system

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.
func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()
	commonConfig.InitLogger("system-backend.log", nil)

	// 加载配置
	cfg := config.Load()

	// 检查端口是否可用
	if err := utils.CheckPortAvailable(cfg.ServerAddr); err != nil {
		logger.L().Error("端口检查失败", "error", err, "addr", cfg.ServerAddr)
		os.Exit(1)
	}
	logger.L().Info("端口检查通过", "addr", cfg.ServerAddr)

	// 临时调试：输出配置值
	logger.L().Info("[DEBUG] PostgreSQL 配置",
		"host", cfg.PostgresHost,
		"port", cfg.PostgresPort,
		"user", cfg.PostgresUser,
		"db", cfg.PostgresDB)

	// 初始化数据库
	db, err := repository.InitDB(cfg.DatabaseURL)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	// 自动迁移
	if err := repository.AutoMigrate(db); err != nil {
		logger.L().Error("数据库迁移失败", "error", err)
		os.Exit(1)
	}

	// 迁移 task_providers 表（将 create_task_url/edit_task_url 合入 capabilities）
	if err := repository.MigrateTaskProviders(db); err != nil {
		logger.L().Error("task_providers 迁移失败", "error", err)
		os.Exit(1)
	}

	// 创建模块注册表索引
	if err := repository.CreateModuleRegistryIndexes(db); err != nil {
		logger.L().Error("模块注册表索引创建失败", "error", err)
		os.Exit(1)
	}

	// 注释：MigrateExistingEnginesDisplayName 已删除（display_name 字段已移除）

	// 统一刷新引擎能力声明。旧 capabilities 结构不再保留，Meta/Develop 等消费端只读取新结构。
	{
		userRepo := repository.NewUserRepository(db)
		engineRepo := repository.NewEngineRepository(db)
		engineService := service.NewEngineService(engineRepo, userRepo, cfg.EncryptionKey, nil)
		if err := engineService.RefreshAllEngineCapabilities(); err != nil {
			logger.L().Error("刷新引擎能力声明失败", "error", err)
			os.Exit(1)
		}
	}

	// 初始化超级管理员用户
	if err := repository.InitSuperAdmin(db); err != nil {
		logger.L().Error("超级管理员用户初始化失败", "error", err)
		os.Exit(1)
	}

	// 初始化默认租户和租户管理员 (仅开发环境且显式启用时)
	if err := repository.InitDefaultTenant(db); err != nil {
		logger.L().Error("默认租户初始化失败", "error", err)
		os.Exit(1)
	}

	// 设置 Gin 模式
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	router := api.SetupRouter(db, cfg)

	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: router,
	}

	// 在 goroutine 中启动服务器
	go func() {
		logger.L().Info("系统服务启动", "addr", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.L().Error("服务器启动失败", "error", err)
			os.Exit(1)
		}
	}()

	// 启动服务注册与心跳（在服务器启动后）
	go func() {
		// 等待3秒确保服务完全启动
		time.Sleep(3 * time.Second)

		// 构建服务URL
		serviceHost := utils.GetServiceHost()
		port := utils.GetModulePort("system")
		serviceURL := utils.BuildServiceURL(serviceHost, port)

		// 注册自己到模块注册表
		moduleRegistryRepo := repository.NewModuleRegistryRepository(db)
		moduleRegistryService := service.NewModuleRegistryService(moduleRegistryRepo)

		// System 模块注册自己
		registrationReq := &models.ModuleRegistrationRequest{
			ModuleName:     "system",
			ModuleURL:      serviceURL,
			RoutePrefix:    "/system",
			HealthCheckURL: serviceURL + "/health",
			Metadata: map[string]interface{}{
				"module": "system",
			},
		}

		if err := moduleRegistryService.Register(registrationReq); err != nil {
			logger.L().Error("System 模块注册失败", "error", err)
		} else {
			logger.L().Info("System 模块注册成功", "url", serviceURL)
		}

		// 启动心跳 goroutine
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := moduleRegistryService.SendHeartbeat("system"); err != nil {
					logger.L().Error("System 心跳失败", "error", err)
				} else {
					logger.L().Debug("System 心跳成功")
				}
			}
		}()

		// 启动服务清理定时任务（标记超时模块为down）
		ctx := context.Background()
		go moduleRegistryService.StartCleanupTask(ctx, 60*time.Second)
	}()

	// 启动健康检查（在后台goroutine中）
	go func() {
		// 等待3秒确保服务完全启动
		// 工作流引擎会主动触发连接检查，无需等待过长时间
		time.Sleep(3 * time.Second)

		// 初始化 Redis 客户端（用于 EngineService）
		var redisClient *redis.Client
		if cfg.RedisHost != "" {
			redisClient = redis.NewClient(&redis.Options{
				Addr:     cfg.RedisHost + ":" + cfg.RedisPort,
				Password: cfg.RedisPassword,
				DB:       cfg.RedisDB,
			})
		}

		// 创建 EngineService
		userRepo := repository.NewUserRepository(db)
		engineRepo := repository.NewEngineRepository(db)
		engineService := service.NewEngineService(engineRepo, userRepo, cfg.EncryptionKey, redisClient)

		// 创建并运行健康检查器
		healthChecker := service.NewHealthChecker(engineService)
		healthChecker.CheckAllResourcesOnStartup()
	}()

	// 启动日志归档定时任务（如果启用）
	var scheduler *service.SchedulerService
	if service.IsArchiveEnabled() {
		// 初始化 MinIO 客户端
		minioClient, err := service.InitMinIOClient(cfg)
		if err != nil {
			logger.L().Error("MinIO 客户端初始化失败，日志归档功能将被禁用", "error", err)
		} else {
			// 创建归档服务
			logRepo := repository.NewLogRepository(db)
			archiveService := service.NewLogArchiveService(logRepo, minioClient)

			// 创建并启动调度器（每天凌晨2点执行）
			scheduler = service.NewSchedulerService(archiveService, "0 2 * * *")
			if err := scheduler.Start(); err != nil {
				logger.L().Error("日志归档调度器启动失败", "error", err)
			}
		}
	}

	// 等待中断信号以优雅关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.L().Info("正在关闭 System 服务器...")

	// 关闭所有数据库连接池
	dbbridge.CloseAllPools()
	logger.L().Info("已关闭所有数据库连接池")

	// 关闭 HTTP 服务器，设置 5 秒超时
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.L().Error("服务器强制关闭", "error", err)
	}

	logger.L().Info("System 服务器已关闭")
}
