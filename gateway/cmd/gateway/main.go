package main

import (
	"log"

	"github.com/addp/gateway/internal/config"
	"github.com/addp/gateway/internal/router"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 设置 Gin 模式
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	r := router.SetupRouter(cfg)

	// 启动服务器
	log.Printf("Gateway 启动在 %s", cfg.Port)
	if err := r.Run(cfg.Port); err != nil {
		log.Fatalf("Gateway 启动失败: %v", err)
	}
}