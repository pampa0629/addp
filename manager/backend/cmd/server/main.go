package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/api"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/addp/manager/internal/worker"
	_ "github.com/addp/manager/internal/service/builtin" // 导入内置预览插件
	"github.com/redis/go-redis/v9"
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
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	// 初始化 repositories
	resourceRepo := repository.NewResourceRepository(db)
	searchHistoryRepo := repository.NewSearchHistoryRepository(db)
	metadataRepo := repository.NewMetadataRepository(db, cfg.EncryptionKey)

	logger.L().Info("Manager 配置加载完成",
		"enable_integration", cfg.EnableIntegration,
		"internal_api_key_set", cfg.InternalAPIKey != "",
	)

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
	resourceCacheService := service.NewResourceCacheService(cfg.SystemServiceURL, cfg.InternalAPIKey, redisClient)
	_ = resourceCacheService // TODO: 集成到 metadataService 中使用

	// 初始化 System 客户端（用于拉取解密的资源连接信息）
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	}

	contentRegistry := service.NewObjectContentRegistry()
	pluginDirs := service.ParsePluginDirSpec(cfg.PreviewPluginDir)
	contentDirSpec := buildContentDirSpec(pluginDirs)
	if contentDirSpec != "" {
		service.LoadObjectContentPlugins(contentRegistry, contentDirSpec)
	}
	logger.L().Info("数据预览: 已激活内容插件")

	previewRegistry := service.NewPreviewRegistry()

	// 注册内置预览插件（通过 init() 自动注册到全局注册表）
	if err := service.RegisterBuiltinProviders(previewRegistry, metadataRepo, contentRegistry); err != nil {
		logger.L().Error("注册内置预览插件失败", "error", err)
		os.Exit(1)
	}

	// 加载外部插件（从配置目录）
	service.LoadPreviewPlugins(previewRegistry, metadataRepo, contentRegistry, cfg.PreviewPluginDir)
	logger.L().Info("数据预览: 已激活预览插件", "providers", previewRegistry.Providers())

	// 初始化 services
	resourceService := service.NewResourceService(resourceRepo)
	searchHistoryService := service.NewSearchHistoryService(searchHistoryRepo)
	metadataService := service.NewMetadataService(metadataRepo, resourceRepo, systemClient, previewRegistry, contentRegistry, cfg.MetaServiceURL)
	searchService, err := service.NewFullTextSearchService(cfg)
	if err != nil {
		logger.L().Error("初始化全文检索服务失败", "error", err)
		os.Exit(1)
	}
	defer searchService.Close()

	// 创建统一 MVT 服务（整合实时生成 + 缓存访问，对前端隐藏 fingerprint）
	mvtService := service.NewMVTService(metadataRepo, resourceRepo)
	unifiedMVTService := service.NewUnifiedMVTService(
		service.NewSpatialPreviewService(redisClient),
		mvtService,
		metadataRepo,
	)
	logger.L().Info("统一 MVT 服务已初始化（RESTful API + 三层缓存穿透架构）")

	// 初始化 Task Queue（用于 Quick View 批量缓存生成）
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	taskQueue := worker.NewTaskQueue(redisAddr, cfg.RedisPassword)

	// 初始化 Quick View 服务（依赖 Redis 和数据库）
	quickViewService := service.NewQuickViewService(db, taskQueue, systemClient)

	// 设置 UnifiedMVTService 的 QuickViewService（延迟注入避免循环依赖）
	unifiedMVTService.SetQuickViewService(quickViewService)
	logger.L().Info("Quick View 服务已初始化（自动缓存 + 批量生成）")

	router := api.SetupRouter(cfg, resourceService, metadataService, searchService, searchHistoryService, unifiedMVTService, quickViewService, resourceRepo, metadataRepo, systemClient)

	// ✅ 注册优雅关闭处理器（关闭所有数据库连接池）
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.L().Info("收到关闭信号，正在清理资源...")
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
