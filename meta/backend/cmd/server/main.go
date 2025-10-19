package main

import (
	"context"
	"fmt"
	"os"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/api"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/service"

	// Import plugins package to auto-register third-party extractors
	_ "github.com/addp/meta/internal/scanner/plugins"
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

	// 初始化服务
	resourceService := service.NewResourceService(db, cfg.SystemServiceURL, cfg.InternalAPIKey)
	if err := resourceService.PreloadResources(); err != nil {
		logger.L().Warn("资源预加载失败，延迟到首次请求", "error", err)
	}
	scanService := service.NewScanServiceNew(db, resourceService)
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
