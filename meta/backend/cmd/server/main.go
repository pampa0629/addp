package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/embedding"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/common/vectorstore"
	"github.com/addp/meta/internal/api"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/search"
	"github.com/addp/meta/internal/service"
	"github.com/addp/meta/internal/worker"
	"github.com/redis/go-redis/v9"

	// 导入数据库插件以触发自动注册
	_ "github.com/addp/common/engine/plugins/clickhouse"
	_ "github.com/addp/common/engine/plugins/doris"
	_ "github.com/addp/common/engine/plugins/minio"
	_ "github.com/addp/common/engine/plugins/mongodb"
	_ "github.com/addp/common/engine/plugins/mysql"
	_ "github.com/addp/common/engine/plugins/postgresql"
	_ "github.com/addp/common/engine/plugins/s3"
	_ "github.com/addp/common/engine/plugins/spark_sql"
)

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.LoadConfig()

	// 立即检查向量化配置（在InitLogger之前）
	fmt.Printf("🔍 [EARLY CHECK] Embedding BaseURL: %s (len=%d)\n", cfg.EmbeddingService.BaseURL, len(cfg.EmbeddingService.BaseURL))
	fmt.Printf("🔍 [EARLY CHECK] API Key length: %d\n", len(cfg.EmbeddingService.APIKey))
	fmt.Printf("🔍 [EARLY CHECK] Vector DB: %s:%s/%s\n", cfg.VectorDB.Host, cfg.VectorDB.Port, cfg.VectorDB.Schema)

	// 重新初始化日志（支持动态级别/格式，并写入日志文件）
	commonConfig.InitLogger("meta-backend.log", &commonConfig.LoggerOptions{
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		AddSource: &cfg.LogAddSource,
		File:      cfg.LogFile,
	})

	logger.L().Info("Meta 服务配置加载完成",
		"port", cfg.ServerPort,
		"db_host", cfg.DBHost,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
	)

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	logger.L().Info("数据库连接初始化完成")

	// 初始化 Redis 客户端（可选，用于资源变更事件同步和任务队列）
	var redisClient *redis.Client
	var taskQueue *worker.TaskQueue
	if cfg.RedisHost != "" && cfg.RedisPort != "" {
		redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		logger.L().Info("Redis 客户端已初始化", "addr", redisAddr)

		// 创建任务队列（用于异步扫描）
		taskQueue = worker.NewTaskQueue(redisAddr, cfg.RedisPassword)
		logger.L().Info("任务队列已初始化", "redis_addr", redisAddr)
	} else {
		logger.L().Warn("Redis 未配置，将使用本地队列执行扫描任务")
	}
	if taskQueue != nil {
		defer taskQueue.Close()
	}

	// 初始化服务
	engineService := service.NewEngineService(db, cfg.SystemServiceURL, cfg.InternalAPIKey, redisClient)
	if err := engineService.PreloadResources(); err != nil {
		logger.L().Warn("资源预加载失败，延迟到首次请求", "error", err)
	}

	// 初始化 System 客户端（用于审计日志和服务间调用）
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
		logger.L().Info("SystemClient 已初始化", "system_url", cfg.SystemServiceURL)
	}

	searchIndexer, err := search.NewIndexer(cfg)
	if err != nil {
		logger.L().Warn("搜索索引器初始化失败，搜索功能将被禁用", "error", err)
		searchIndexer = nil // 继续运行，但不使用搜索索引
	}
	scanService := service.NewScanService(db, engineService)
	scanService.SetConfig(cfg) // 注入配置
	if searchIndexer != nil {
		scanService.SetIndexer(searchIndexer)
	}

	// 初始化扫描事件发布器（如果 Redis 可用）
	if redisClient != nil {
		scanEventPublisher := events.NewScanEventPublisher(redisClient, logger.L())
		scanService.SetScanEventPublisher(scanEventPublisher)
		logger.L().Info("扫描事件发布器已初始化")
	}

	var pgVectorStore *vectorstore.PgVectorStore
	baseURL := strings.TrimSpace(cfg.EmbeddingService.BaseURL)
	fmt.Printf("🔍 [VECTOR INIT] BaseURL check: %s (empty=%v)\n", baseURL, baseURL == "")
	if baseURL != "" {
		fmt.Printf("🔄 [VECTOR INIT] Starting vector store init...\n")
		store, err := vectorstore.NewPgVectorStore(context.Background(), cfg.VectorDB)
		if err != nil {
			fmt.Printf("❌ [VECTOR INIT] Vector store failed: %v\n", err)
		} else {
			fmt.Printf("✅ [VECTOR INIT] Vector store success!\n")
			models := map[embedding.Modality]string{
				embedding.ModalityText:     cfg.EmbeddingService.TextModel,
				embedding.ModalityDocument: cfg.EmbeddingService.TextModel,
				embedding.ModalityImage:    cfg.EmbeddingService.ImageModel,
				embedding.ModalityAudio:    cfg.EmbeddingService.AudioModel,
				embedding.ModalityVideo:    cfg.EmbeddingService.VideoModel,
			}
			fmt.Printf("🔄 [VECTOR INIT] Starting embedding client init...\n")
			client, err := embedding.NewHTTPEmbeddingClient(embedding.ServiceConfig{
				BaseURL: cfg.EmbeddingService.BaseURL,
				APIKey:  cfg.EmbeddingService.APIKey,
				Timeout: cfg.EmbeddingService.Timeout,
				Models:  models,
			})
			if err != nil {
				fmt.Printf("❌ [VECTOR INIT] Embedding client failed: %v\n", err)
				store.Close()
			} else {
				fmt.Printf("✅ [VECTOR INIT] Embedding client success!\n")
				scanService.EnableDocumentVectorization(store, client, cfg.EmbeddingService.Timeout)
				pgVectorStore = store
				fmt.Printf("🎉 [VECTOR INIT] Document vectorization ENABLED! Schema=%s Table=%s Dim=%d\n",
					cfg.VectorDB.Schema, cfg.VectorDB.Table, cfg.VectorDB.Dimension)
			}
		}
	} else {
		fmt.Printf("⚠️ [VECTOR INIT] BaseURL is empty, vectorization disabled\n")
	}

	if pgVectorStore != nil {
		defer pgVectorStore.Close()
	}

	taskService := service.NewScanTaskService(db, scanService, engineService, redisClient)
	// 将 taskService 注入到 engineService（用于处理 ScanConfig）
	engineService.SetTaskService(taskService)

	// 如果配置了任务队列，则使用任务队列（worker 模式）
	if taskQueue != nil {
		taskService.SetTaskQueue(taskQueue)
		logger.L().Info("扫描任务服务将使用 Worker 队列执行任务")
	} else {
		logger.L().Info("扫描任务服务将使用本地 goroutine 执行任务")
	}
	if err := taskService.Start(context.Background()); err != nil {
		logger.L().Error("扫描任务服务启动失败", "error", err)
		os.Exit(1)
	}
	defer taskService.Stop(context.Background())

	// ========== 启动清理服务 ==========
	cleanupService := service.NewCleanupService(db, service.CleanupConfig{
		Enabled:         true,
		RetentionDays:   90,
		CleanupInterval: 24 * time.Hour,
	})
	if err := cleanupService.Start(context.Background()); err != nil {
		logger.L().Error("清理服务启动失败", "error", err)
		os.Exit(1)
	}
	defer cleanupService.Stop(context.Background())
	logger.L().Info("清理服务已启动", "retention_days", 90)
	// ===================================

	// 设置路由
	router := api.SetupRouter(cfg, engineService, scanService, taskService, redisClient, systemClient)

	// ========== 任务提供者注册（启动时自动注册到 System task_providers）==========
	// 构造 Meta 服务的外部访问 URL（供 Orchestrator 调用）
	metaServiceURL := fmt.Sprintf("http://meta-backend:%s", cfg.ServerPort)
	if os.Getenv("META_SERVICE_URL") != "" {
		metaServiceURL = os.Getenv("META_SERVICE_URL")
	}

	if cfg.EnableIntegration && cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		taskProviderRegistry := service.NewTaskProviderRegistryService(
			cfg.SystemServiceURL,
			cfg.InternalAPIKey,
			metaServiceURL,
		)

		// 后台异步注册（不阻塞启动，支持重试）
		go func() {
			time.Sleep(2 * time.Second) // 等待服务完全启动
			maxRetries := 5
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := taskProviderRegistry.Register(); err != nil {
					logger.L().Warn("任务提供者注册失败",
						"attempt", fmt.Sprintf("%d/%d", attempt, maxRetries),
						"error", err)
					time.Sleep(time.Duration(attempt*2) * time.Second) // 指数退避
					continue
				}
				logger.L().Info("✅ Meta 模块已注册到 task_providers")
				return
			}
			logger.L().Error("任务提供者注册失败（已达最大重试次数）", "max_retries", maxRetries)
		}()
	}

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	logger.L().Info("Meta 服务启动", "addr", addr)

	if err := router.Run(addr); err != nil {
		logger.L().Error("HTTP 服务启动失败", "error", err)
		os.Exit(1)
	}
}
