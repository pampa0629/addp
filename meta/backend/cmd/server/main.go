package main

import (
	"fmt"
	"os"
	"path/filepath"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/api"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/repository"
)

func main() {
	// 加载 .env 文件（meta/backend/cmd/server 到项目根目录需要回退4级）
	commonConfig.LoadEnv(4)
	initLoggerDefaults()

	// 加载配置
	cfg := config.LoadConfig()

	// 根据配置重新初始化日志（支持动态级别/格式）
	_ = os.Setenv("LOG_FILE", cfg.LogFile)
	logger.Init(logger.Options{
		Level:          cfg.LogLevel,
		Format:         cfg.LogFormat,
		AddSource:      cfg.LogAddSource,
		FilePath:       cfg.LogFile,
		RedirectStdLog: true,
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

	// TODO: 实现定时任务调度（Phase 4）
	// 可以使用 robfig/cron 库实现定时扫描

	// 设置路由（使用新的简化路由）
	router := api.SetupRouterNew(cfg, db)

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	logger.L().Info("Meta 服务启动", "addr", addr)

	if err := router.Run(addr); err != nil {
		logger.L().Error("HTTP 服务启动失败", "error", err)
		os.Exit(1)
	}
}

func initLoggerDefaults() {
	logLevel := commonConfig.GetEnv("LOG_LEVEL", "info")
	logFormat := commonConfig.GetEnv("LOG_FORMAT", "json")
	addSource := commonConfig.GetEnvBool("LOG_ADD_SOURCE", false)

	logFile := commonConfig.GetEnv("LOG_FILE", "")
	if logFile == "" {
		logFile = commonConfig.ResolveFromRoot("logs", "meta-backend.log")
	} else if !filepath.IsAbs(logFile) {
		logFile = commonConfig.ResolveFromRoot(logFile)
	}
	_ = os.Setenv("LOG_FILE", logFile)

	logger.Init(logger.Options{
		Level:          logLevel,
		Format:         logFormat,
		AddSource:      addSource,
		FilePath:       logFile,
		RedirectStdLog: true,
	})
}
