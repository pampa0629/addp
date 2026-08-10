package main

import (
	"context"
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	commonExecution "github.com/addp/common/execution"
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

	// 初始化 TaskProviderRegistry（从 System 动态加载任务提供者）
	taskProviderRegistry := service.NewTaskProviderRegistry(
		systemServiceClient,
		5*time.Minute, // 缓存 TTL
	)

	// 初始化 Executor（通过 TaskProvider 引用任务）
	executor := service.NewExecutor(executionService, orchRepo, taskProviderRegistry, serviceTokenSource)

	// 初始化 Scheduler（使用统一执行服务）
	scheduler := service.NewScheduler(orchRepo, executionService, executor, systemServiceClient)
	if err := scheduler.Start(); err != nil {
		log.Fatalf("调度器启动失败: %v", err)
	}
	defer scheduler.Stop()

	log.Println("✅ 调度器启动成功")
	log.Println("✅ 任务提供者注册表已初始化（从 System 动态加载）")

	// 设置路由（传递 taskProviderRegistry、systemURL、redisClient 和 systemClient）
	router := api.SetupRouter(
		orchRepo, executionService, executor, taskProviderRegistry,
		cfg.SystemServiceURL, redisClient, systemServiceClient, taskAuthorizationClient, serviceTokenSource,
	)

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.ServerPort)

	// ========== 模块注册（注册到 System service_registry）==========
	if cfg.SystemServiceURL != "" {
		systemServiceClient.RegisterAndHeartbeatWithMetadata(
			context.Background(), "orchestrator", serviceURL, "/orchestrator",
			map[string]interface{}{"module": "orchestrator"},
		)
	}

	// ========== 任务提供者注册（启动时自动注册到 System task_providers）==========
	if cfg.SystemServiceURL != "" {
		taskProviderRegistration := service.NewTaskProviderRegistrationService(
			systemServiceClient,
			serviceURL,
		)

		go func() {
			time.Sleep(2 * time.Second)
			maxRetries := 5
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := taskProviderRegistration.Register(context.Background()); err != nil {
					log.Printf("⚠️  Orchestrator 任务提供者注册失败 (尝试 %d/%d): %v", attempt, maxRetries, err)
					time.Sleep(time.Duration(attempt*2) * time.Second)
					continue
				}
				log.Printf("✅ Orchestrator 模块已注册到 task_providers")
				return
			}
			log.Printf("❌ Orchestrator 任务提供者注册失败（已达最大重试次数：%d）", maxRetries)
		}()
	}

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("🚀 Orchestrator 服务启动: %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
