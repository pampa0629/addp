package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/addp/common/dbbridge"
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
	log.Printf("🔗 GeoPandas Engine: %s", cfg.GeoPandasEngineURL)
	log.Printf("🔗 Spark Sedona Engine: %s", cfg.SparkEngineURL)

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		log.Fatalf("❌ Failed to initialize database: %v", err)
	}
	log.Printf("✅ Database initialized successfully")

	// ========== Repository 层 ==========
	devItemRepo := repository.NewDevItemRepository(db)
	devExecutionRepo := repository.NewDevExecutionRepository(db)
	log.Printf("✅ Repository 层初始化完成")

	// ========== 创建 System Client ==========
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	log.Printf("✅ System Client 创建成功")

	// ========== Service 层 ==========
	// 1. 工作流引擎服务
	workflowEngine := service.NewWorkflowEngineService(cfg.GeoPandasEngineURL, systemClient)
	log.Printf("✅ WorkflowEngineService 初始化完成")

	// 2. SQL引擎服务
	sqlEngine := service.NewSQLEngineService(cfg, systemClient)
	log.Printf("✅ SQLEngineService 初始化完成")

	// 3. DevItem业务逻辑服务
	devItemService := service.NewDevItemService(devItemRepo)
	log.Printf("✅ DevItemService 初始化完成")

	// 4. DevExecutor 统一执行器
	devExecutor := service.NewDevExecutor(devItemRepo, devExecutionRepo, workflowEngine, sqlEngine)
	log.Printf("✅ DevExecutor 初始化完成")

	// 5. 算子发现服务
	operatorDiscovery := service.NewOperatorDiscoveryService(
		cfg.GeoPandasEngineURL,
		cfg.SparkEngineURL, // 新增: Spark Engine
		cfg.MetaServiceURL,
		cfg.TransferServiceURL,
		cfg.ManagerServiceURL,
	)
	log.Printf("✅ OperatorDiscoveryService 初始化完成")

	// ========== Handler 层 ==========
	devItemHandler := api.NewDevItemHandler(devItemService)
	devExecutionHandler := api.NewDevExecutionHandler(devExecutor)
	operatorHandler := api.NewOperatorHandler(operatorDiscovery)
	resourceHandler := api.NewResourceHandler(systemClient)
	sqlHandler := api.NewSQLHandler(sqlEngine, devItemService)
	log.Printf("✅ Handler 层初始化完成")

	// ========== 设置路由 ==========
	router := api.SetupRouter(cfg, devItemHandler, devExecutionHandler, operatorHandler, resourceHandler, sqlHandler, devItemService)
	log.Printf("✅ 路由设置完成")

	// ========== 任务提供者注册（启动时自动注册到 System task_providers）==========
	// 构造 Develop 服务的外部访问 URL（供 Orchestrator 调用）
	developServiceURL := fmt.Sprintf("http://develop-backend:%s", cfg.ServerAddr)
	if os.Getenv("DEVELOP_SERVICE_URL") != "" {
		developServiceURL = os.Getenv("DEVELOP_SERVICE_URL")
	}

	engineRegistry := service.NewEngineRegistryService(
		cfg.SystemServiceURL,
		cfg.InternalAPIKey,
		developServiceURL,
	)

	// 后台异步注册（不阻塞启动，支持重试）
	go func() {
		time.Sleep(2 * time.Second) // 等待服务完全启动
		maxRetries := 5
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if err := engineRegistry.RegisterEngine(); err != nil {
				log.Printf("⚠️  Registration attempt %d/%d failed: %v", attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*2) * time.Second) // 指数退避
				continue
			}
			log.Printf("✅ Develop engine registered successfully")
			return
		}
		log.Printf("❌ Engine registration failed after %d attempts", maxRetries)
	}()

	// 设置优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// 启动服务器（非阻塞）
	addr := ":" + cfg.ServerAddr
	log.Printf("🎉 Develop Service is running on %s", addr)
	log.Printf("📋 API文档: http://localhost%s/health", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("❌ Failed to start server: %v", err)
		}
	}()

	// 等待终止信号
	<-sigCh
	log.Println("🛑 Shutting down Develop Service...")

	// 优雅关闭：关闭所有数据库连接池
	dbbridge.CloseAllPools()
	log.Println("✅ All database connection pools closed")

	log.Println("👋 Develop Service stopped")
}
