package main

import (
	"fmt"
	"log"
	"os"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/api"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	_ "github.com/addp/transfer/internal/transform"
	"github.com/addp/transfer/internal/worker"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

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

	// 连接数据库
	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 初始化 Repository 层
	_ = repository.NewMappingRepository(db) // mappingRepo unused for now

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

	// 初始化 Service 层（传入 taskQueue 和 redisClient）
	taskService := service.NewTaskService(db, nil, cfg, taskQueue) // engine 传 nil（暂不执行任务）
	executionService := service.NewExecutionService(db)
	localEngineService := service.NewLocalEngineService(db, cfg, redisClient)
	objectStorageService := service.NewObjectStorageService(localEngineService)

	// 初始化 System 客户端（用于审计日志和服务间调用）
	var systemClient *commonClient.SystemClient
	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
		log.Printf("✅ SystemClient 已初始化: %s", cfg.SystemServiceURL)
	}

	// 设置路由
	router := api.SetupRouter(taskService, executionService, localEngineService, objectStorageService, cfg.SystemServiceURL, redisClient, systemClient)

	// ========== 任务提供者注册（启动时自动注册到 System task_providers）==========
	// 构造 Transfer 服务的外部访问 URL（供 Orchestrator 调用）
	transferServiceURL := fmt.Sprintf("http://transfer-backend:%s", cfg.Port)
	if os.Getenv("TRANSFER_SERVICE_URL") != "" {
		transferServiceURL = os.Getenv("TRANSFER_SERVICE_URL")
	}

	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		taskProviderRegistry := service.NewTaskProviderRegistryService(
			cfg.SystemServiceURL,
			cfg.InternalAPIKey,
			transferServiceURL,
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

	// Initialize database with auto-migration
	db, err := commonRepo.InitDatabase(dbConfig,
		&models.Task{},
		&models.TaskExecution{},
		&models.DataMapping{},
		&models.LocalEngine{},
		// &pipeline.Checkpoint{}, // TODO: 启用 pipeline 时取消注释
	)
	if err != nil {
		return nil, err
	}

	return db, nil
}
