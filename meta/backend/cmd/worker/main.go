package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	commonConfig "github.com/addp/common/config"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/service"
	"github.com/addp/meta/internal/worker"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	// 导入数据库插件以触发自动注册
	_ "github.com/addp/common/engine/plugins/clickhouse"
	_ "github.com/addp/common/engine/plugins/doris"
	_ "github.com/addp/common/engine/plugins/minio"
	_ "github.com/addp/common/engine/plugins/mongodb"
	_ "github.com/addp/common/engine/plugins/mysql"
	_ "github.com/addp/common/engine/plugins/neo4j"
	_ "github.com/addp/common/engine/plugins/postgresql"
	_ "github.com/addp/common/engine/plugins/s3"
	_ "github.com/addp/common/engine/plugins/spark_sql"
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

	// 加载 .env 文件（从项目根目录）
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.LoadConfig()

	// 连接数据库
	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	// 创建 Redis 客户端用于事件订阅
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	log.Printf("✅ Redis 客户端已初始化: %s", redisAddr)

	// 初始化 Service 层（传入 Redis 客户端用于事件订阅）
	engineService := service.NewEngineService(db, cfg.SystemServiceURL, cfg.InternalAPIKey, redisClient)
	scanService := service.NewScanService(db, engineService)

	// 创建并注入去重服务
	dedupService := service.NewScanDedupService(redisClient)
	scanService.SetDedupService(dedupService)

	// 注意：这里不启动 ScanTaskService 的 worker loop 和 cron，因为 worker 进程不需要这些
	scanTaskService := service.NewScanTaskService(db, scanService, engineService, redisClient)

	// 创建任务队列（复用已有的 redisAddr）
	taskQueue := worker.NewTaskQueue(redisAddr, cfg.RedisPassword)
	defer taskQueue.Close()

	// 创建任务处理器
	taskHandler := worker.NewTaskHandler(scanTaskService)

	// 创建 Asynq Server
	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     redisAddr,
			Password: cfg.RedisPassword,
		},
		asynq.Config{
			Concurrency: cfg.ConcurrentTasks, // 并发任务数
			Queues: map[string]int{
				"meta:critical": 6, // 高优先级队列
				"meta:default":  3, // 默认队列
				"meta:low":      1, // 低优先级队列
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

	// 启动 Worker
	log.Printf("🚀 Meta Worker 启动中...")
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

	log.Println("✅ Meta Worker 已启动，等待扫描任务...")

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
