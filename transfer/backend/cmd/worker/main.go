package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/connector"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/worker"
	"github.com/addp/transfer/pkg/pipeline"
	commonConfig "github.com/addp/common/config"
	"github.com/hibiken/asynq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// 加载 .env 文件（从项目根目录，上4级：transfer/backend/cmd/worker → 根目录）
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.Load()

	// 连接数据库
	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	// 初始化 Pipeline 组件
	registry := pipeline.NewConnectorRegistry()

	// 注册所有连接器
	connector.RegisterAllConnectors(registry)
	log.Printf("✅ 已注册连接器 - Readers: %v, Writers: %v",
		registry.ListReaders(), registry.ListWriters())

	// 创建 logger 和 engine config
	logger := slog.Default()
	engineConfig := pipeline.DefaultEngineConfig()

	stateManager := pipeline.NewStateManager(db)
	engine := pipeline.NewExecutionEngine(registry, stateManager, logger, engineConfig)

	// 创建任务队列
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	taskQueue := worker.NewTaskQueue(redisAddr, cfg.RedisPassword)
	defer taskQueue.Close()

	// 初始化 Service 层（传入 taskQueue）
	taskService := service.NewTaskService(db, engine, cfg, taskQueue)
	executionService := service.NewExecutionService(db)

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
				"critical": 6, // 高优先级队列
				"default":  3, // 默认队列
				"low":      1, // 低优先级队列
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

	// 创建定时调度器（需要先创建 repository）
	taskRepo := repository.NewTaskRepository(db)
	executionRepo := repository.NewExecutionRepository(db)
	scheduler := worker.NewScheduler(taskRepo, executionRepo, taskQueue)
	if err := scheduler.Start(context.Background()); err != nil {
		log.Fatalf("定时调度器启动失败: %v", err)
	}
	defer scheduler.Stop()

	// 启动 Worker
	log.Printf("🚀 Transfer Worker 启动中...")
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

	log.Println("✅ Transfer Worker 已启动，等待任务...")

	// 等待关闭信号
	<-ctx.Done()
	log.Println("🛑 收到关闭信号，正在优雅退出...")

	// 关闭 Worker
	srv.Shutdown()
	log.Println("✅ Worker 已安全关闭")
}

// connectDatabase 连接数据库
func connectDatabase(cfg *config.Config) (*gorm.DB, error) {
	// 构建 DSN
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSchema,
	)

	// 连接数据库
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	log.Println("✅ 数据库连接成功")
	return db, nil
}
