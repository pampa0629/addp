package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/api"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/addp/manager/internal/worker"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
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

	// 导入格式解析器以触发自动注册
	_ "github.com/addp/common/format/document" // MongoDB/NoSQL collection parser
)

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.Load()
	commonConfig.InitLogger("manager-backend.log", &commonConfig.LoggerOptions{
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		AddSource: &cfg.LogAddSource,
		File:      cfg.LogFile,
	})

	// 初始化数据库
	log.Println("🔍 [DEBUG] 开始初始化数据库...")
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}
	log.Println("🔍 [DEBUG] 数据库初始化完成")

	// 初始化 repositories
	log.Println("🔍 [DEBUG] 开始初始化 repositories...")
	searchHistoryRepo := repository.NewSearchHistoryRepository(db)
	metadataRepo := repository.NewMetadataRepository(db, cfg.EncryptionKey)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	log.Println("🔍 [DEBUG] Repositories 初始化完成")

	log.Printf("🔍 [DEBUG] cfg.EnableMetaIntegration=%v, cfg.InternalAPIKey=%s, cfg.MetaServiceURL=%s",
		cfg.EnableMetaIntegration,
		func() string { if cfg.InternalAPIKey != "" { return "***set***" } else { return "empty" } }(),
		cfg.MetaServiceURL,
	)
	log.Println("🔍 [DEBUG] 即将输出 Manager 配置加载完成日志...")
	logger.L().Info("Manager 配置加载完成",
		"enable_integration", cfg.EnableIntegration,
		"enable_meta_integration", cfg.EnableMetaIntegration,
		"internal_api_key_set", cfg.InternalAPIKey != "",
		"meta_service_url", cfg.MetaServiceURL,
	)
	log.Println("🔍 [DEBUG] Manager 配置加载完成日志已输出")

	// 初始化 Redis 客户端（可选，用于资源变更事件同步）
	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		logger.L().Info("Redis 客户端已初始化", "addr", redisAddr)
	} else {
		logger.L().Warn("Redis 未配置，资源变更事件同步功能将被禁用")
	}

	// 初始化资源缓存服务（带 Redis 事件订阅）
	engineCacheService := service.NewEngineCacheService(cfg.SystemServiceURL, cfg.InternalAPIKey, redisClient)
	_ = engineCacheService // TODO: 集成到 metadataService 中使用

	// 初始化缓存管理器和扫描事件处理器（用于 Meta 扫描完成后自动刷新缓存）
	var cacheManager *service.CacheManager
	var scanEventHandler *service.ScanEventHandler
	if redisClient != nil {
		cacheManager = service.NewCacheManager(metadataRepo, redisClient)
		scanEventHandler = service.NewScanEventHandler(cacheManager, redisClient)
		logger.L().Info("扫描事件订阅已启动，将自动清理扫描完成的资源缓存")
	}

	// 初始化 System 客户端（用于拉取解密的资源连接信息）
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	}

	// 初始化 Meta 客户端（用于查询元数据）
	var metaClient *commonClient.MetaClient
	if cfg.EnableMetaIntegration && cfg.InternalAPIKey != "" && cfg.MetaServiceURL != "" {
		metaClient = commonClient.NewMetaClientWithInternalKey(cfg.MetaServiceURL, cfg.InternalAPIKey)
		logger.L().Info("MetaClient 已初始化",
			"meta_url", cfg.MetaServiceURL)
	} else {
		logger.L().Warn("Meta 集成未启用或配置不完整，元数据查询功能将不可用")
	}

	contentRegistry := service.NewObjectContentRegistry()
	pluginDirs := service.ParsePluginDirSpec(cfg.PreviewPluginDir)
	contentDirSpec := buildContentDirSpec(pluginDirs)
	if contentDirSpec != "" {
		service.LoadObjectContentPlugins(contentRegistry, contentDirSpec)
	}
	logger.L().Info("数据预览: 已激活内容插件")

	previewRegistry := service.NewPreviewRegistry()

	// 加载预览插件（从配置目录）
	// 同时扫描 plugins/ 根目录（向后兼容）和 plugins/providers/ 子目录（新架构）
	providerDirSpec := buildProviderDirSpec(pluginDirs)
	service.LoadPreviewPlugins(previewRegistry, metadataRepo, metaClient, contentRegistry, cfg.MetaServiceURL, providerDirSpec)
	logger.L().Info("数据预览: 已激活预览插件", "providers", previewRegistry.Providers())

	// 初始化 services（注意：Manager 不负责引擎管理，引擎信息通过 SystemClient 获取）
	searchHistoryService := service.NewSearchHistoryService(searchHistoryRepo)
	metadataService := service.NewMetadataService(metadataRepo, systemClient, metaClient, previewRegistry, contentRegistry, cfg.MetaServiceURL)
	searchService, err := service.NewHybridSearchService(cfg)
	if err != nil {
		logger.L().Error("初始化混合检索服务失败", "error", err)
		os.Exit(1)
	}
	defer searchService.Close()

	// 创建统一 MVT 服务（整合实时生成 + 缓存访问，对前端隐藏 fingerprint）
	mvtService := service.NewMVTService(metadataRepo, systemClient)
	unifiedMVTService := service.NewUnifiedMVTService(
		service.NewSpatialPreviewService(redisClient),
		mvtService,
		metadataRepo,
	)
	logger.L().Info("统一 MVT 服务已初始化（RESTful API + 三层缓存穿透架构）")

	// 初始化 Task Queue（用于 Quick View 批量缓存生成）
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	taskQueue := worker.NewTaskQueue(redisAddr, cfg.RedisPassword)

	// 初始化 MinIO 客户端（用于瓦片存储和删除）
	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		log.Fatalf("❌ Failed to create MinIO client: %v", err)
	}

	// MinIO Bucket 名称（与 Worker 保持一致）
	minioBucket := "manager"

	// 初始化 Quick View 服务（依赖 Redis、MinIO 和数据库）
	quickViewService := service.NewQuickViewService(db, taskQueue, systemClient, metaClient, minioClient, minioBucket, redisClient)

	// 初始化向量化服务（Manager 模块的按需向量化）
	embeddingService, err := service.NewEmbeddingService(embeddingRepo, systemClient, cfg, logger.L())
	if err != nil {
		logger.L().Warn("向量化服务初始化失败（功能将不可用）", "error", err)
		embeddingService = nil // 设置为 nil，允许服务继续启动
	} else {
		logger.L().Info("向量化服务已初始化（支持单对象、目录、Bucket 三级向量化）")
	}

	// 设置 UnifiedMVTService 的 QuickViewService（延迟注入避免循环依赖）
	unifiedMVTService.SetQuickViewService(quickViewService)
	logger.L().Info("Quick View 服务已初始化（自动缓存 + 批量生成）")

	router := api.SetupRouter(cfg, metadataService, searchService, searchHistoryService, unifiedMVTService, quickViewService, metadataRepo, systemClient, metaClient, cacheManager, redisClient, embeddingService)

	// ========== 任务提供者注册（启动时自动注册到 System task_providers）==========
	// 构造 Manager 服务的外部访问 URL（供 Orchestrator 调用）
	managerServiceURL := fmt.Sprintf("http://manager-backend:%s", cfg.Port)
	if os.Getenv("MANAGER_SERVICE_URL") != "" {
		managerServiceURL = os.Getenv("MANAGER_SERVICE_URL")
	}

	if cfg.EnableIntegration && cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		taskProviderRegistry := service.NewTaskProviderRegistryService(
			cfg.SystemServiceURL,
			cfg.InternalAPIKey,
			managerServiceURL,
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
				logger.L().Info("✅ Manager 模块已注册到 task_providers")
				return
			}
			logger.L().Error("任务提供者注册失败（已达最大重试次数）", "max_retries", maxRetries)
		}()
	}

	// ✅ 注册优雅关闭处理器（关闭所有数据库连接池）
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.L().Info("收到关闭信号，正在清理资源...")

		// 停止扫描事件订阅
		if scanEventHandler != nil {
			scanEventHandler.Stop()
		}

		if err := mvtService.Close(); err != nil {
			logger.L().Error("关闭数据库连接池失败", "error", err)
		} else {
			logger.L().Info("所有数据库连接池已关闭")
		}
		os.Exit(0)
	}()

	// 启动服务
	addr := ":" + cfg.Port
	logger.L().Info("Manager 服务启动", "addr", addr)
	if err := router.Run(addr); err != nil {
		logger.L().Error("Manager 服务启动失败", "error", err)
		os.Exit(1)
	}
}

func buildContentDirSpec(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	contentDirs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			continue
		}
		contentDirs = append(contentDirs, filepath.Join(trimmed, "content"))
	}
	return strings.Join(contentDirs, ",")
}

// buildProviderDirSpec 构建 Provider 插件目录列表
// 同时扫描根目录（向后兼容）和 providers/ 子目录（新架构）
func buildProviderDirSpec(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	providerDirs := make([]string, 0, len(dirs)*2)
	for _, dir := range dirs {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			continue
		}
		// 扫描根目录（向后兼容，目前已无文件）
		providerDirs = append(providerDirs, trimmed)
		// 扫描 providers/ 子目录（新架构）
		providerDirs = append(providerDirs, filepath.Join(trimmed, "providers"))
	}
	return strings.Join(providerDirs, ",")
}

