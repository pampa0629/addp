package main

import (
	"fmt"
	"log"

	"github.com/addp/meta/internal/api"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/repository"
	"github.com/robfig/cron/v3"
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

	// 设置定时任务
	if cfg.AutoSyncEnabled {
		c := cron.New()
		_, err := c.AddFunc(cfg.AutoSyncSchedule, func() {
			log.Println("Running auto sync task...")
			// TODO: 实现定时自动同步逻辑
			// syncService.AutoSyncAll(0) // 0表示所有租户
		})
		if err != nil {
			log.Printf("Failed to add cron job: %v", err)
		} else {
			c.Start()
			log.Printf("Auto sync scheduled: %s", cfg.AutoSyncSchedule)
		}
	}

	// 设置路由
	router := api.SetupRouter(cfg, db)

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("Meta service starting on %s", addr)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
