package main

import (
	"os"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/common/utils"
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

	// 检查端口是否可用
	if err := utils.CheckPortAvailable(cfg.Port); err != nil {
		logger.L().Error("端口检查失败", "error", err, "port", cfg.Port)
		os.Exit(1)
	}
	logger.L().Info("端口检查通过", "port", cfg.Port)

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
