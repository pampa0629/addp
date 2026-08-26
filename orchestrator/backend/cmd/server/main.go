package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/orchestrator/i18n"
	"github.com/addp/orchestrator/internal/api"
	"github.com/addp/orchestrator/internal/config"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title           ADDP Orchestrator API
// @version         1.0
// @host      localhost:8084
// @BasePath  /api/v1/orchestrator
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.LoadConfig()
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	// 检查端口是否可用
	if err := commonConfig.CheckPortAvailable(cfg.ServerPort); err != nil {
		log.Fatalf("❌ 端口检查失败: %v", err)
	}
	log.Printf("✅ 端口检查通过: %s", cfg.ServerPort)

	// 连接数据库
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSchema)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("统一执行记录存储初始化失败: %v", err)
	}

	// 自动迁移
	if err := db.AutoMigrate(
		&models.Orchestration{},
	); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	if err := repository.ApplySQLMigrations(db); err != nil {
		log.Fatalf("SQL 迁移失败: %v", err)
	}

	log.Println("✅ 数据库连接成功")

	// 初始化 Redis 客户端
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: cfg.RedisPassword,
		DB:       0,
	})
	log.Printf("✅ Redis 客户端已初始化: %s", redisAddr)

	// 初始化 Repository
	orchRepo := repository.NewOrchestrationRepository(db)

	// 初始化 ExecutionService（使用统一执行表）
	executionService := service.NewExecutionService(db, orchRepo)
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemServiceURL, "addp-orchestrator", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, serviceTokenSource, nil)
	taskAuthorizationClient := commonClient.NewSystemExecutionAuthorizationClient(cfg.SystemServiceURL, nil)

	// 初始化 TaskProviderResolver（每次使用前从 System 动态解析任务提供者）
	taskProviderResolver := service.NewTaskProviderResolver(systemServiceClient)

	// 初始化 Executor（通过 TaskProvider 引用任务）
	executor := service.NewExecutor(executionService, orchRepo, taskProviderResolver, serviceTokenSource)

	// 初始化 Scheduler（使用统一执行服务）
	var registration *commonClient.ModuleRegistrationLifecycle
	scheduler := service.NewScheduler(orchRepo, executionService, executor, systemServiceClient)
	log.Println("✅ TaskProvider 动态解析器已初始化")

	// 设置路由（传递 taskProviderResolver、systemURL、redisClient 和 systemClient）
	lifecycleController := modulelifecycle.NewBusiness("orchestrator", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(
		orchRepo, executionService, executor, taskProviderResolver,
		cfg.SystemServiceURL, redisClient, systemServiceClient, taskAuthorizationClient, serviceTokenSource, lifecycleController,
	)
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Orchestrator 监听绑定失败: %v", err)
	}

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.ServerPort)
	// ========== 模块定义、运行实例与 TaskProvider 声明发布 ==========
	if cfg.SystemServiceURL != "" {
		provider, err := service.OrchestratorTaskProviderDeclaration()
		if err != nil {
			log.Fatalf("构建 Orchestrator TaskProvider 声明失败: %v", err)
		}
		registration = systemServiceClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
			ModuleName: "orchestrator", ModuleURL: serviceURL, RoutePrefix: "/orchestrator",
			HealthCheckURL: serviceURL + "/health/ready", Metadata: map[string]interface{}{"module": "orchestrator"},
			TaskProvider: provider,
		})
		lifecycleController.AttachRegistration(registration)
		modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)
	}
	scheduler.SetClaimGate(func() bool { return registration != nil && registration.IsRegistered() })
	if err := scheduler.Start(); err != nil {
		log.Fatalf("调度器启动失败: %v", err)
	}
	defer scheduler.Stop()
	log.Println("✅ 调度器启动成功")

	// 启动服务器
	log.Printf("🚀 Orchestrator 服务启动: %s", addr)
	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("服务器启动失败: %v", err)
			stopRuntime()
		}
	}()
	<-runtimeContext.Done()
	if registration != nil {
		<-registration.Done()
	}
}
