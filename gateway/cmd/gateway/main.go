package main

import (
	"os"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/gateway/internal/config"
	"github.com/addp/gateway/internal/router"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()
	commonConfig.InitLogger("gateway.log", nil)

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
