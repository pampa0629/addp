package main

import (
	"os"
	"path/filepath"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/gateway/internal/config"
	"github.com/addp/gateway/internal/router"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载 .env 文件（gateway/cmd/gateway 到项目根目录需要回退3级）
	commonConfig.LoadEnv(3)
	initLogger()

	// 加载配置
	cfg := config.Load()

	// 设置 Gin 模式
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	r := router.SetupRouter(cfg)

	// 启动服务器
	logger.L().Info("Gateway 服务启动", "addr", cfg.Port)
	if err := r.Run(cfg.Port); err != nil {
		logger.L().Error("Gateway 服务启动失败", "error", err)
		os.Exit(1)
	}
}

func initLogger() {
	logLevel := commonConfig.GetEnv("LOG_LEVEL", "info")
	logFormat := commonConfig.GetEnv("LOG_FORMAT", "json")
	addSource := commonConfig.GetEnvBool("LOG_ADD_SOURCE", false)

	logFile := commonConfig.GetEnv("LOG_FILE", "")
	if logFile == "" {
		logFile = commonConfig.ResolveFromRoot("logs", "gateway.log")
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
