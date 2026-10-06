package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/addp/common/buildinfo"
	commonConfig "github.com/addp/common/config"
	commonconfiguration "github.com/addp/common/configuration"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/logger"
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
	commonConfig.InitLogger(nil)

	// 加载配置
	cfg := config.Load()

	// 检查端口是否可用
	if err := commonConfig.CheckPortAvailable(cfg.ServerAddr); err != nil {
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

	// 注释：MigrateExistingEnginesDisplayName 已删除（display_name 字段已移除）

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

	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopRuntime()
	executionMaintenanceDone := make(chan struct{})
	go func() {
		defer close(executionMaintenanceDone)
		service.RunExecutionEventMaintenance(runtimeContext, db, logger.L())
	}()

	// 在 goroutine 中启动服务器
	go func() {
		logger.L().Info("系统服务启动", "addr", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.L().Error("服务器启动失败", "error", err)
			os.Exit(1)
		}
	}()

	// System 直接使用本地注册服务，退出前等待注销完成。
	moduleRegistryService := service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	serviceURL := commonConfig.BuildServiceURL(commonConfig.GetServiceHost(), cfg.ServerAddr)
	registrationDone, registrationErr := startSystemRegistration(runtimeContext, moduleRegistryService,
		newSystemRegistrationRequest(serviceURL, buildinfo.ProcessInstanceID()))
	if registrationErr != nil {
		logger.L().Error("System 模块注册失败", "error", registrationErr)
	}
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		moduleRegistryService.StartCleanupTask(runtimeContext)
	}()

	// 启动健康检查（在后台 goroutine 中持续更新最近连接状态）
	go func() {
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
		healthChecker.Run(runtimeContext, service.DefaultHealthCheckInterval)
	}()

	<-runtimeContext.Done()
	logger.L().Info("正在关闭 System 服务器...")
	if registrationDone != nil {
		<-registrationDone
	}
	<-cleanupDone
	<-executionMaintenanceDone

	// 关闭 HTTP 服务器，设置 5 秒超时
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.L().Error("服务器强制关闭", "error", err)
	}

	// 先结束 HTTP 在途请求，再关闭它们使用的数据库连接池。
	dbbridge.CloseAllPools()
	logger.L().Info("已关闭所有数据库连接池")
	logger.L().Info("System 服务器已关闭")
}

func startSystemRegistration(ctx context.Context, registry *service.ModuleRegistryService, request *models.ModuleRegistrationRequest) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := registry.Register(request); err != nil {
		return nil, err
	}
	logger.L().Info("System 模块注册成功", "url", request.ModuleURL)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if err := registry.Deregister(request.ModuleName, request.InstanceID); err != nil {
				logger.L().Error("System 模块注销失败", "error", err)
			}
		}()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := registry.SendHeartbeat(request.ModuleName, request.InstanceID); err != nil {
					logger.L().Error("System 心跳失败", "error", err)
				}
			}
		}
	}()
	return done, nil
}

func newSystemRegistrationRequest(serviceURL, instanceID string) *models.ModuleRegistrationRequest {
	hostNodeName, runtimeHostname := commonConfig.RuntimeNodeIdentity()
	return &models.ModuleRegistrationRequest{
		HostNodeName: hostNodeName, RuntimeHostname: runtimeHostname,
		NodeID: commonConfig.RuntimeHostNodeID(), RegistrationClientID: "addp-system",
		HostNodeIPs:      commonConfig.RuntimeHostNodeIPs(),
		ModuleName:       "system",
		InstanceID:       instanceID,
		Role:             models.ModuleRuntimeRoleBackend,
		ModuleURL:        serviceURL,
		RoutePrefix:      "/system",
		HealthCheckURL:   serviceURL + "/health/ready",
		ProcessStartedAt: buildinfo.ProcessStartedAt(),
		Metadata: map[string]interface{}{
			"module": "system",
		},
		ConfigurationManagement: &commonconfiguration.ManagementDeclaration{
			SchemaVersion: commonconfiguration.ManagementSchemaVersion,
			Entries: []commonconfiguration.ManagementEntry{{
				ID: "system.iam_security_policy", OwnerModule: "system",
				ScopeTypes:       []string{commonconfiguration.ScopePlatformOnly},
				FrontendRoute:    "/system/configuration",
				ReadPermission:   systemauthorization.PermissionIamSecurityPolicyRead,
				UpdatePermission: systemauthorization.PermissionIamSecurityPolicyUpdate,
			}, {
				ID: "system.engine_raster_policy", OwnerModule: "system",
				ScopeTypes:       []string{commonconfiguration.ScopePlatformDefaultWithTenantOverride},
				FrontendRoute:    "/system/configuration",
				ReadPermission:   systemauthorization.PermissionSystemEngineRasterPolicyRead,
				UpdatePermission: systemauthorization.PermissionSystemEngineRasterPolicyUpdate,
			}},
		},
	}
}
