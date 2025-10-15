package main

import (
	"os"
	"path/filepath"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/system/internal/api"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/repository"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载 .env 文件（system/backend/cmd/server 到项目根目录需要回退4级）
	commonConfig.LoadEnv(4)
	initLogger()

	// 加载配置
	cfg := config.Load()

	// 初始化数据库
	db, err := repository.InitDB(cfg.DatabaseURL)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	// 自动迁移
	if err := repository.AutoMigrate(db); err != nil {
		logger.L().Error("数据库迁移失败", "error", err)
		os.Exit(1)
	}

	// 初始化超级管理员用户
	if err := repository.InitSuperAdmin(db); err != nil {
		logger.L().Error("超级管理员用户初始化失败", "error", err)
		os.Exit(1)
	}

	// 设置 Gin 模式
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	router := api.SetupRouter(db, cfg)

	// 启动服务器
	logger.L().Info("系统服务启动", "addr", cfg.ServerAddr)
	if err := router.Run(cfg.ServerAddr); err != nil {
		logger.L().Error("服务器启动失败", "error", err)
		os.Exit(1)
	}
}

func initLogger() {
	logLevel := commonConfig.GetEnv("LOG_LEVEL", "info")
	logFormat := commonConfig.GetEnv("LOG_FORMAT", "json")
	addSource := commonConfig.GetEnvBool("LOG_ADD_SOURCE", false)

	logFile := commonConfig.GetEnv("LOG_FILE", "")
	if logFile == "" {
		logFile = commonConfig.ResolveFromRoot("logs", "system-backend.log")
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
