package main

import (
	"fmt"
	"log"

	"github.com/addp/meta/internal/api"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/repository"
)

func main() {
	// 加载配置
	cfg := config.LoadConfig()

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	log.Println("Database initialized successfully")

	// TODO: 实现定时任务调度（Phase 4）
	// 可以使用 robfig/cron 库实现定时扫描

	// 设置路由（使用新的简化路由）
	router := api.SetupRouterNew(cfg, db)

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("Meta service starting on %s", addr)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
