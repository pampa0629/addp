package main

import (
	"log"

	"github.com/addp/manager/internal/api"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/connector"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// 初始化 repositories
	dataSourceRepo := repository.NewDataSourceRepository(db)

	// 初始化 System 客户端
	systemClient := connector.NewSystemClient(cfg.SystemServiceURL)

	// 初始化 services
	dataSourceService := service.NewDataSourceService(dataSourceRepo, systemClient)

	// 设置路由
	router := api.SetupRouter(cfg, dataSourceService)

	// 启动服务
	log.Printf("Manager service starting on port %s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}