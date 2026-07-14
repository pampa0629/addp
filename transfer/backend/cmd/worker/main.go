package main

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/worker"
	"github.com/hibiken/asynq"
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

	// 加载 .env 文件（从项目根目录，上4级：transfer/backend/cmd/worker → 根目录）
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.Load()

	// 初始化结构化日志
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	logFile := filepath.Join("logs", "transfer-bounded-worker.log")
	logger.Init(logger.Options{
		Level:          logLevel,
		Format:         "json",
		FilePath:       logFile,
		AddSource:      true,
		RedirectStdLog: true,
	})
	logger.L().Info("transfer bounded worker starting",
		"version", "0.0.20",
		"log_level", logLevel,
		"log_file", logFile,
	)

	// 连接数据库
	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	// 创建任务队列
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	taskQueue := worker.NewTaskQueue(redisAddr, cfg.RedisPassword)
	defer taskQueue.Close()

	// 创建 Repository 层
	taskRepo := repository.NewTaskRepository(db)

	// 创建统一执行服务
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	executionService := service.NewExecutionService(db, taskExecutionRepo)
	executionService.SetTaskQueue(taskQueue)

	// 创建 SystemClient（用于执行引擎）
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.SystemServiceURL != "" {
		if cfg.InternalAPIKey != "" {
			systemClient = commonClient.NewSystemClientWithInternalKey(
				cfg.SystemServiceURL,
				cfg.InternalAPIKey,
			)
			log.Printf("✅ SystemClient initialized with internal API key: %s", cfg.SystemServiceURL)
		} else {
			log.Printf("⚠️  SystemClient not initialized - no internal API key configured")
		}
	}

	// 创建 MetaClient（用于元数据扫描）
	var metaClient *commonClient.MetaClient
	if cfg.EnableIntegration && cfg.MetaServiceURL != "" {
		if cfg.InternalAPIKey != "" {
			metaClient = commonClient.NewMetaClientWithInternalKey(
				cfg.MetaServiceURL,
				cfg.InternalAPIKey,
			)
			log.Printf("✅ MetaClient initialized: %s", cfg.MetaServiceURL)
		} else {
			log.Printf("⚠️  MetaClient not initialized - no internal API key configured")
		}
	}

	// 初始化 Service 层
	// 1. 创建 ExecutionEngineService (负责任务执行)
	executionEngineService := service.NewExecutionEngineService(
		taskRepo,
		repository.NewSyncStateRepository(db),
		executionService,
		systemClient,
		metaClient,
	)
	executionEngineService.SetConfig(cfg)

	// 2. 创建 TaskService (负责任务 CRUD，传入 executionEngineService)
	taskService := service.NewTaskService(db, executionEngineService, cfg, taskQueue)

	// 创建任务处理器
	taskHandler := worker.NewTaskHandler(taskService, executionService)

	// 创建 Asynq Server
	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     redisAddr,
			Password: cfg.RedisPassword,
		},
		asynq.Config{
			Concurrency: cfg.ConcurrentTasks, // 并发任务数
			Queues: map[string]int{
				"transfer:critical": 6, // 高优先级队列
				"transfer:default":  3, // 默认队列
				"transfer:low":      1, // 低优先级队列
			},
			// 重试配置
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				return time.Duration(n) * cfg.RetryDelay
			},
		},
	)

	// 注册任务处理器
	mux := asynq.NewServeMux()
	taskHandler.RegisterHandlers(mux)

	// 创建定时调度器
	scheduler := worker.NewScheduler(taskRepo, taskQueue)
	scheduler.SetExecutionService(executionService)
	if err := scheduler.Start(context.Background()); err != nil {
		log.Fatalf("定时调度器启动失败: %v", err)
	}
	defer scheduler.Stop()

	// 启动 Worker
	log.Printf("🚀 Transfer Bounded Worker 启动中...")
	log.Printf("📊 数据库: %s@%s:%s/%s (schema: %s)",
		cfg.DBUser, cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBSchema)
	log.Printf("📮 Redis: %s", redisAddr)
	log.Printf("🔧 并发数: %d", cfg.ConcurrentTasks)
	log.Printf("🔄 重试次数: %d", cfg.MaxRetries)
	log.Printf("⏱️  重试延迟: %v", cfg.RetryDelay)

	// 优雅关闭
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 在 goroutine 中运行 worker
	go func() {
		if err := srv.Run(mux); err != nil {
			log.Fatalf("Worker 运行失败: %v", err)
		}
	}()

	log.Println("✅ Transfer Bounded Worker 已启动，等待任务...")

	// 等待关闭信号
	<-ctx.Done()
	log.Println("🛑 收到关闭信号，正在优雅退出...")

	// 关闭 Worker
	srv.Shutdown()
	log.Println("✅ Worker 已安全关闭")
}

// connectDatabase 连接数据库
func connectDatabase(cfg *config.Config) (*gorm.DB, error) {
	// Use common repository InitDatabase (worker doesn't need auto-migration)
	dbConfig := commonRepo.DatabaseConfig{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		Schema:   cfg.DBSchema,
		SSLMode:  "disable",
	}

	// Initialize database without auto-migration (tables already created by server)
	db, err := commonRepo.InitDatabase(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	return db, nil
}
