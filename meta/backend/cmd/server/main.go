package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/embedding"
	"github.com/addp/common/logger"
	"github.com/addp/common/vectorstore"
	"github.com/addp/meta/internal/api"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/search"
	"github.com/addp/meta/internal/service"
	"github.com/redis/go-redis/v9"
)

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.LoadConfig()

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

	// 初始化 Redis 客户端（可选，用于资源变更事件同步）
	var redisClient *redis.Client
	if cfg.RedisAddr != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		logger.L().Info("Redis 客户端已初始化", "addr", cfg.RedisAddr)
	} else {
		logger.L().Warn("Redis 未配置，资源变更事件同步功能将被禁用")
	}

	// 初始化服务
	resourceService := service.NewResourceService(db, cfg.SystemServiceURL, cfg.InternalAPIKey, redisClient)
	if err := resourceService.PreloadResources(); err != nil {
		logger.L().Warn("资源预加载失败，延迟到首次请求", "error", err)
	}
	searchIndexer, err := search.NewIndexer(cfg)
	if err != nil {
		logger.L().Warn("Elasticsearch 索引器初始化失败，搜索功能将被禁用", "error", err)
		searchIndexer = nil // 继续运行，但不使用搜索索引
	}
	scanService := service.NewScanServiceNew(db, resourceService)
	if searchIndexer != nil {
		scanService.SetIndexer(searchIndexer)
	}

	var pgVectorStore *vectorstore.PgVectorStore
	if strings.TrimSpace(cfg.EmbeddingService.BaseURL) != "" {
		store, err := vectorstore.NewPgVectorStore(context.Background(), cfg.VectorDB)
		if err != nil {
			logger.L().Warn("向量存储初始化失败，将禁用文档向量化", "error", err)
		} else {
			models := map[embedding.Modality]string{
				embedding.ModalityText:     cfg.EmbeddingService.TextModel,
				embedding.ModalityDocument: cfg.EmbeddingService.TextModel,
				embedding.ModalityImage:    cfg.EmbeddingService.ImageModel,
				embedding.ModalityAudio:    cfg.EmbeddingService.AudioModel,
				embedding.ModalityVideo:    cfg.EmbeddingService.VideoModel,
			}
			client, err := embedding.NewHTTPEmbeddingClient(embedding.ServiceConfig{
				BaseURL: cfg.EmbeddingService.BaseURL,
				APIKey:  cfg.EmbeddingService.APIKey,
				Timeout: cfg.EmbeddingService.Timeout,
				Models:  models,
			})
			if err != nil {
				logger.L().Warn("向量化服务客户端初始化失败，将禁用文档向量化", "error", err)
				store.Close()
			} else {
				scanService.EnableDocumentVectorization(store, client, cfg.EmbeddingService.Timeout)
				pgVectorStore = store
				logger.L().Info("文档向量化已启用", "vector_schema", cfg.VectorDB.Schema, "vector_table", cfg.VectorDB.Table)
			}
		}
	} else {
		logger.L().Info("未配置向量化服务，文档向量化保持禁用状态")
	}

	if pgVectorStore != nil {
		defer pgVectorStore.Close()
	}

	taskService := service.NewScanTaskService(db, scanService, resourceService)
	if err := taskService.Start(context.Background()); err != nil {
		logger.L().Error("扫描任务服务启动失败", "error", err)
		os.Exit(1)
	}
	defer taskService.Stop(context.Background())

	// 设置路由（使用新的简化路由）
	router := api.SetupRouterNew(cfg, resourceService, scanService, taskService)

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	logger.L().Info("Meta 服务启动", "addr", addr)

	if err := router.Run(addr); err != nil {
		logger.L().Error("HTTP 服务启动失败", "error", err)
		os.Exit(1)
	}
}
