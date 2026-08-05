package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonConfig "github.com/addp/common/config"
	commonconfiguration "github.com/addp/common/configuration"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/logger"
	"github.com/addp/common/utils"
	"github.com/addp/system/internal/api"
	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
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
// @description Type "Bearer" followed by a space and an ADDP opaque access token.
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

	// IAM 必须先执行显式版本化 migration；非 IAM 表随后暂由 GORM 管理。
	migrationContext, cancelMigration := context.WithTimeout(context.Background(), 2*time.Minute)
	if err := migration.NewRunner(cfg.PostgreSQLDSN()).Run(migrationContext); err != nil {
		cancelMigration()
		logger.L().Error("IAM 数据库迁移失败", "error", err)
		os.Exit(1)
	}
	cancelMigration()
	if err := repository.AutoMigrateNonIAM(db); err != nil {
		logger.L().Error("非 IAM 数据库迁移失败", "error", err)
		os.Exit(1)
	}
	serviceCredentialProvisioner, err := iam.NewServiceCredentialProvisioner(iam.NewRepository(db), nil)
	if err != nil {
		logger.L().Error("初始化 Service Principal Credential Provisioner 失败", "error", err)
		os.Exit(1)
	}
	credentialContext, cancelCredentials := context.WithTimeout(context.Background(), 30*time.Second)
	if err := serviceCredentialProvisioner.Apply(credentialContext, cfg.ServiceClientSecrets); err != nil {
		cancelCredentials()
		logger.L().Error("同步内置 Service Principal Credential 失败", "error", err)
		os.Exit(1)
	}
	cancelCredentials()

	// 迁移 task_providers 表（删除旧 create_task_url/edit_task_url 列）
	if err := repository.MigrateTaskProviders(db); err != nil {
		logger.L().Error("task_providers 迁移失败", "error", err)
		os.Exit(1)
	}

	// 清理误注册到 System 的本地文件型连接器。SQLite/SpatiaLite 作为文件格式或容器处理，不作为 System engine 注册。
	if err := repository.RemoveLocalFileEnginesFromSystem(db); err != nil {
		logger.L().Error("清理本地文件型引擎失败", "error", err)
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
		engineRepo := repository.NewEngineRepository(db)
		registryService := service.NewRegistryService(engineRepo)
		registrationContext, cancelRegistration := context.WithTimeout(context.Background(), 10*time.Second)
		builtinRuntimes := []struct {
			engineType  string
			endpoint    string
			description string
		}{
			{engineType: "duckdb", endpoint: cfg.DuckDBRuntimeURL, description: "ADDP 内置联邦只读查询 Runtime"},
			{engineType: "inference_runtime", endpoint: cfg.InferenceRuntimeURL, description: "ADDP 内置统一 AI 推理 Runtime"},
		}
		for _, runtime := range builtinRuntimes {
			if _, err := registryService.RegisterBuiltinRuntime(registrationContext, runtime.engineType, runtime.endpoint, runtime.description); err != nil {
				cancelRegistration()
				logger.L().Error("注册内置 Runtime 失败", "engine_type", runtime.engineType, "error", err)
				os.Exit(1)
			}
		}
		cancelRegistration()
		engineService := service.NewEngineService(engineRepo, cfg.EncryptionKey, nil)
		if err := engineService.RefreshAllEngineCapabilities(); err != nil {
			logger.L().Error("刷新引擎能力声明失败", "error", err)
			os.Exit(1)
		}
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
			ConfigurationManagement: &commonconfiguration.ManagementDeclaration{
				SchemaVersion: commonconfiguration.ManagementSchemaVersion,
				Entries: []commonconfiguration.ManagementEntry{{
					ID: "system.iam_security_policy", OwnerModule: "system",
					ScopeTypes:       []string{commonconfiguration.ScopePlatformOnly},
					FrontendRoute:    "/system/settings/security-policy",
					ReadPermission:   systemauthorization.PermissionIamSecurityPolicyRead,
					UpdatePermission: systemauthorization.PermissionIamSecurityPolicyUpdate,
				}},
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
		engineRepo := repository.NewEngineRepository(db)
		engineService := service.NewEngineService(engineRepo, cfg.EncryptionKey, redisClient)

		// 创建并运行健康检查器
		healthChecker := service.NewHealthChecker(engineService)
		healthChecker.CheckAllResourcesOnStartup()
	}()

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
