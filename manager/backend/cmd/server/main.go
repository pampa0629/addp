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
	commonModels "github.com/addp/common/models"
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

	// 初始化缓存管理器和扫描事件处理器（用于 Meta 扫描完成后自动刷新缓存）
	var scanEventHandler *service.ScanEventHandler
	if redisClient != nil {
		cacheManager := service.NewCacheManager(metadataRepo, redisClient)
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

	// 注册内置预览插件（通过 init() 自动注册到全局注册表）
	if err := service.RegisterBuiltinProviders(previewRegistry, metadataRepo, metaClient, contentRegistry); err != nil {
		logger.L().Error("注册内置预览插件失败", "error", err)
		os.Exit(1)
	}

	// 加载外部插件（从配置目录）
	service.LoadPreviewPlugins(previewRegistry, metadataRepo, metaClient, contentRegistry, cfg.PreviewPluginDir)
	logger.L().Info("数据预览: 已激活预览插件", "providers", previewRegistry.Providers())

	// 初始化 services
	resourceService := service.NewResourceService(resourceRepo)
	searchHistoryService := service.NewSearchHistoryService(searchHistoryRepo)
	metadataService := service.NewMetadataService(metadataRepo, resourceRepo, systemClient, metaClient, previewRegistry, contentRegistry, cfg.MetaServiceURL)
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
	quickViewService := service.NewQuickViewService(db, taskQueue, systemClient, metaClient)

	// 设置 UnifiedMVTService 的 QuickViewService（延迟注入避免循环依赖）
	unifiedMVTService.SetQuickViewService(quickViewService)
	logger.L().Info("Quick View 服务已初始化（自动缓存 + 批量生成）")

	router := api.SetupRouter(cfg, resourceService, metadataService, searchService, searchHistoryService, unifiedMVTService, quickViewService, resourceRepo, metadataRepo, systemClient, redisClient)

	// 注册能力到 System 模块（在服务启动后）
	if cfg.EnableIntegration && cfg.SystemServiceURL != "" {
		go registerCapabilities(cfg)
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

// registerCapabilities 注册 Manager 模块的能力到 System
func registerCapabilities(cfg *config.Config) {
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)

	// 构建能力声明
	capabilities := &commonModels.Capability{
		Compute: []commonModels.ComputeCapability{
			{
				Type:             "cache.tile",
				SupportedSources: []string{"postgresql", "geojson", "shapefile"},
				Features:         []string{"async", "zoom_range", "bbox_filter", "mvt"},
			},
		},
	}

	// 构建任务 API 配置
	baseURL := fmt.Sprintf("http://localhost:%s", cfg.Port)
	taskAPIConfig := &commonModels.TaskAPIConfig{
		BaseURL: baseURL,
		Endpoints: map[string]commonModels.APIEndpoint{
			"create": {
				Method: "POST",
				Path:   "/api/quick-view",
				BodyTemplate: map[string]interface{}{
					"tenant_id":  "{{.TenantID}}",
					"table_name": "{{.TableName}}",
					"min_zoom":   "{{.MinZoom}}",
					"max_zoom":   "{{.MaxZoom}}",
				},
			},
			"status": {
				Method: "GET",
				Path:   "/api/quick-view/status",
				QueryParams: map[string]string{
					"table_name": "{{.TableName}}",
				},
				ResponseMapping: &commonModels.ResponseMapping{
					StatusField:   "status",
					MessageField:  "error_message",
					ProgressField: "progress",
					TaskIDField:   "id",
				},
			},
			"list": {
				Method: "GET",
				Path:   "/api/quick-view/list",
				QueryParams: map[string]string{
					"tenant_id": "{{.TenantID}}",
				},
			},
		},
		Timeout: map[string]int{
			"create": 30,
			"status": 10,
			"list":   10,
		},
	}

	// 健康检查配置
	healthCheckConfig := &commonModels.HealthCheckConfig{
		Endpoint: "/health",
		Timeout:  5,
		Interval: 60,
	}

	// 注册请求
	req := &commonModels.CapabilityRegistrationRequest{
		UniqueIdentifier:  "manager.tile_cache.default",
		Name:              "Manager Tile Cache Engine",
		DisplayName:       "地图瓦片缓存引擎",
		ResourceType:      "compute_engine",
		IsBuiltin:         true,
		Capabilities:      capabilities,
		TaskAPIConfig:     taskAPIConfig,
		HealthCheckConfig: healthCheckConfig,
		Description:       "内置 MVT 瓦片缓存引擎，支持空间数据快速预览",
	}

	// 发送注册请求（失败不阻塞启动）
	if err := systemClient.RegisterCapability(req); err != nil {
		logger.L().Warn("能力注册失败（不影响服务运行）", "error", err)
	} else {
		logger.L().Info("Manager 模块能力已注册到 System", "unique_identifier", req.UniqueIdentifier)
	}
}
