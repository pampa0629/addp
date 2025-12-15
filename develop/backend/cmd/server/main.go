package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

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
	gisExecutionRepo := repository.NewGISExecutionRepository(db)
	spatialTaskRepo := repository.NewSpatialTaskRepository(db)

	// 创建 SystemClient (使用 Internal API Key 获取未加密的资源配置)
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)

	// 创建 SQL Service
	sqlService := service.NewSQLExecutionService(cfg, executionRepo, systemClient)
	defer sqlService.Close()

	// 创建 Spatial Workflow Service
	geopandasEngineURL := cfg.GeoPandasEngineURL
	if geopandasEngineURL == "" {
		geopandasEngineURL = "http://localhost:8099"
		log.Printf("⚠️  GeoPandas Engine URL not configured, using default: %s", geopandasEngineURL)
	}
	spatialWorkflowService := service.NewSpatialWorkflowService(geopandasEngineURL)

	// 创建 GIS Execution Service
	gisExecutionService := service.NewGISExecutionService(gisExecutionRepo, spatialTaskRepo, spatialWorkflowService, db)

	// 创建并启动 Spatial Scheduler
	spatialScheduler := service.NewSpatialScheduler(spatialTaskRepo, gisExecutionService, db)
	ctx := context.Background()
	if err := spatialScheduler.Start(ctx); err != nil {
		log.Fatalf("Failed to start spatial scheduler: %v", err)
	}

	// 创建 Handler
	sqlHandler := api.NewSQLHandler(sqlService)
	spatialHandler := api.NewSpatialHandler(db, spatialWorkflowService, gisExecutionService)
	gisExecutionHandler := api.NewGISExecutionHandler(gisExecutionService)

	// 设置路由
	router := api.SetupRouter(cfg, sqlHandler, spatialHandler, gisExecutionHandler)

	// 设置优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// 启动服务器（非阻塞）
	addr := ":" + cfg.ServerAddr
	log.Printf("✅ Develop Service is running on %s", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 等待终止信号
	<-sigCh
	log.Println("🛑 Shutting down Develop Service...")

	// 停止调度器
	if err := spatialScheduler.Stop(ctx); err != nil {
		log.Printf("Failed to stop spatial scheduler: %v", err)
	}

	log.Println("👋 Develop Service stopped")
}
