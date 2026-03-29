package main

import (
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/monitor/internal/api"
	"github.com/addp/monitor/internal/config"
	"github.com/addp/monitor/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title           ADDP Monitor API
// @version         1.0
// @host      localhost:8100
// @BasePath  /api/v1/monitor
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 连接数据库
	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 自动迁移（确保统一执行表存在）
	if err := db.AutoMigrate(&commonModels.TaskExecution{}); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// 连接 Redis
	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	// 创建 System 客户端（用于健康检查）
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemURL, cfg.InternalAPIKey)

	// 创建 Repository
	taskExecutionRepo := commonRepo.NewTaskExecutionRepository(db)

	// 创建 Services
	queryService := service.NewExecutionQueryService(taskExecutionRepo)
	statisticsService := service.NewStatisticsService(taskExecutionRepo)
	healthService := service.NewHealthCheckService(systemClient)

	// 设置路由
	router := api.SetupRouter(
		queryService,
		statisticsService,
		healthService,
		cfg.SystemURL,
		redisClient,
	)

	// 启动服务
	addr := ":" + cfg.ServerPort
	log.Printf("Monitor service starting on %s", addr)

	// 后台启动服务器
	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 启动模块注册和心跳
	go func() {
		// 等待服务器启动
		time.Sleep(2 * time.Second)

		// 注册模块到 System
		serviceURL := fmt.Sprintf("http://localhost:%s", cfg.ServerPort)
		registrationReq := &commonClient.ModuleRegistrationRequest{
			ModuleName:     "monitor",
			ModuleURL:      serviceURL,
			RoutePrefix:    "/monitor",
			HealthCheckURL: serviceURL + "/health",
			Metadata: map[string]interface{}{
				"module": "monitor",
			},
		}

		maxRetries := 3
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if err := systemClient.RegisterModule(registrationReq); err != nil {
				log.Printf("⚠️  Monitor 模块注册失败 (尝试 %d/%d): %v", attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*5) * time.Second)
				continue
			}
			log.Printf("✅ Monitor 模块注册成功: %s", serviceURL)
			break
		}

		// 启动心跳循环
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if err := systemClient.SendHeartbeat("monitor"); err != nil {
				log.Printf("⚠️  Monitor 心跳失败: %v", err)
			} else {
				log.Printf("✅ Monitor 心跳成功")
			}
		}
	}()

	// 阻塞主 goroutine
	select {}
}
