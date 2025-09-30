package main

import (
	"log"

	"github.com/addp/system/internal/api"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/repository"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 初始化数据库
	db, err := repository.InitDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	// 自动迁移
	if err := repository.AutoMigrate(db); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	// 设置 Gin 模式
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建路由
	router := api.SetupRouter(db, cfg)

	// 启动服务器
	log.Printf("服务器启动在 %s", cfg.ServerAddr)
	if err := router.Run(cfg.ServerAddr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}