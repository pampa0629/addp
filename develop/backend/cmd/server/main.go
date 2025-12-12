package main

import (
	"log"

	"github.com/addp/develop/backend/internal/api"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/addp/develop/backend/internal/service"
	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
)

func main() {
	// 加载 .env 文件
	commonConfig.LoadEnv() // 从项目根目录加载 .env

	// 加载配置
	cfg := config.Load()
	log.Printf("🚀 Starting Develop Service on %s", cfg.ServerAddr)
	log.Printf("📦 Environment: %s", cfg.Env)
	log.Printf("🔗 System Service: %s", cfg.SystemServiceURL)

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// 创建 Repository
	executionRepo := repository.NewExecutionRepository(db)

	// 创建 SystemClient
	systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, "")

	// 创建 SQL Service
	sqlService := service.NewSQLExecutionService(cfg, executionRepo, systemClient)
	defer sqlService.Close()

	// 创建 Spatial Workflow Service
	geopandasEngineURL := cfg.GeoPandasEngineURL
	if geopandasEngineURL == "" {
		geopandasEngineURL = "http://localhost:8090"
		log.Printf("⚠️  GeoPandas Engine URL not configured, using default: %s", geopandasEngineURL)
	}
	spatialWorkflowService := service.NewSpatialWorkflowService(geopandasEngineURL)

	// 创建 Handler
	sqlHandler := api.NewSQLHandler(sqlService)
	spatialHandler := api.NewSpatialHandler(db, spatialWorkflowService)

	// 设置路由
	router := api.SetupRouter(cfg, sqlHandler, spatialHandler)

	// 启动服务器
	addr := ":" + cfg.ServerAddr
	log.Printf("✅ Develop Service is running on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
