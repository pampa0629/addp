package main

import (
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"github.com/addp/orchestrator/internal/api"
	"github.com/addp/orchestrator/internal/config"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// 加载配置
	cfg := config.LoadConfig()

	// 检查端口是否可用
	if err := utils.CheckPortAvailable(cfg.ServerPort); err != nil {
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

	// 自动迁移
	if err := db.AutoMigrate(
		&models.Orchestration{},
		// &models.Execution{}, // 【已废弃】改用统一执行表
		&commonModels.TaskExecution{}, // 统一执行记录表（common.task_executions）
	); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
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

	// 初始化 EngineRegistry（从 System 动态加载引擎）
	engineRegistry := service.NewEngineRegistry(
		cfg.SystemServiceURL,
		cfg.InternalAPIKey,
		5*time.Minute, // 缓存 TTL
	)

	// 初始化 TaskProviderRegistry（从 System 动态加载任务提供者）
	taskProviderRegistry := service.NewTaskProviderRegistry(
		cfg.SystemServiceURL,
		cfg.InternalAPIKey,
		5*time.Minute, // 缓存 TTL
	)

	// 初始化 TaskClient（通用任务客户端）
	taskClient := service.NewTaskClient(30 * time.Second)

	// 初始化 ModuleClient（向后兼容旧的硬编码模式）
	moduleClient := service.NewModuleClient(map[string]string{
		"transfer": cfg.TransferServiceURL,
		"meta":     cfg.MetaServiceURL,
		"manager":  cfg.ManagerServiceURL,
	})

	// 初始化 Executor（支持新旧两种模式，使用统一执行服务）
	executor := service.NewExecutor(executionService, orchRepo, engineRegistry, taskClient, moduleClient)

	// 初始化 Scheduler（使用统一执行服务）
	scheduler := service.NewScheduler(orchRepo, executionService, executor)
	if err := scheduler.Start(); err != nil {
		log.Fatalf("调度器启动失败: %v", err)
	}
	defer scheduler.Stop()

	log.Println("✅ 调度器启动成功")
	log.Println("✅ 引擎注册表已初始化（从 System 动态加载）")
	log.Println("✅ 任务提供者注册表已初始化（从 System 动态加载）")

	// 初始化 System 客户端（用于审计日志）
	var systemClient *commonClient.SystemClient
	if cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
		log.Println("✅ SystemClient 已初始化（用于审计日志）")
	}

	// 设置路由（传递 engineRegistry、taskProviderRegistry、systemURL、redisClient 和 systemClient）
	router := api.SetupRouter(orchRepo, executionService, executor, scheduler, moduleClient, engineRegistry, taskProviderRegistry, cfg.SystemServiceURL, redisClient, systemClient)

	// ========== 模块注册（注册到 System service_registry）==========
	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		go func() {
			// 等待3秒确保服务完全启动
			time.Sleep(3 * time.Second)

			// 构建服务URL
			serviceHost := utils.GetServiceHost()
			port := utils.GetModulePort("orchestrator")
			serviceURL := utils.BuildServiceURL(serviceHost, port)

			// 创建 System 客户端用于模块注册
			registryClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)

			// 注册服务
			registrationReq := &commonClient.ModuleRegistrationRequest{
				ModuleName:    "orchestrator",
				ModuleURL:     serviceURL,
				RoutePrefix:    "/orchestrator",
				HealthCheckURL: serviceURL + "/health",
				Metadata: map[string]interface{}{
					"module": "orchestrator",
				},
			}

			maxRetries := 3
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := registryClient.RegisterModule(registrationReq); err != nil {
					log.Printf("⚠️  Orchestrator 模块注册失败 (尝试 %d/%d): %v", attempt, maxRetries, err)
					time.Sleep(time.Duration(attempt*5) * time.Second)
					continue
				}
				log.Printf("✅ Orchestrator 模块注册成功: %s", serviceURL)
				break
			}

			// 启动心跳
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := registryClient.SendHeartbeat("orchestrator"); err != nil {
					log.Printf("❌ Orchestrator 心跳失败: %v", err)
				}
			}
		}()
	}

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("🚀 Orchestrator 服务启动: %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
