package main

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/events"
	"github.com/addp/common/utils"
	"github.com/addp/develop/backend/internal/api"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/addp/develop/backend/internal/service"
	"github.com/redis/go-redis/v9"
)

// @title           ADDP Develop API
// @version         1.0
// @host      localhost:8185
// @BasePath  /api/v1/develop
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 加载 .env 文件
	commonConfig.LoadEnv() // 从项目根目录加载 .env

	// 加载配置
	cfg := config.Load()

	// 检查端口是否可用
	if err := utils.CheckPortAvailable(cfg.ServerAddr); err != nil {
		log.Fatalf("❌ 端口检查失败: %v", err)
	}

	log.Printf("🚀 Starting Develop Service on %s", cfg.ServerAddr)
	log.Printf("📦 Environment: %s", cfg.Env)
	log.Printf("🔗 System Service: %s", cfg.SystemServiceURL)

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		log.Fatalf("❌ Failed to initialize database: %v", err)
	}
	log.Printf("✅ Database initialized successfully")

	// ========== Repository 层 ==========
	devTaskRepo := repository.NewDevTaskRepository(db)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db) // 统一执行记录仓库
	log.Printf("✅ Repository 层初始化完成（使用统一执行表）")

	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer redisClient.Close()

	// ========== 创建 System Service Client ==========
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemServiceURL, "addp-develop", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, serviceTokenSource, nil)
	executionAuthorizationClient := commonClient.NewSystemExecutionAuthorizationClient(cfg.SystemServiceURL, nil)

	// ========== Service 层 ==========
	// 1. 工作流引擎服务（从 System 动态获取引擎配置）
	workflowEngine := service.NewWorkflowEngineService(systemServiceClient)
	log.Printf("✅ WorkflowEngineService 初始化完成")

	// 2. SQL引擎服务
	sqlEngine := service.NewSQLEngineService(cfg, systemServiceClient, executionAuthorizationClient)
	log.Printf("✅ SQLEngineService 初始化完成")

	// 3. Jupyter引擎服务
	jupyterService := service.NewJupyterService(systemServiceClient, serviceTokenSource)
	log.Printf("✅ JupyterService 初始化完成")

	// 3.6 NotebookExecutionService（Notebook 完整执行服务）
	notebookExecutionService, err := service.NewNotebookExecutionService(jupyterService)
	if err != nil {
		log.Fatalf("NotebookExecutionService 初始化失败: %v", err)
	}
	log.Printf("✅ NotebookExecutionService 初始化完成")

	// 4. DevTask业务逻辑服务
	devTaskService := service.NewDevTaskService(devTaskRepo)
	log.Printf("✅ DevTaskService 初始化完成")

	// 6. DuckDB 联邦查询服务
	metaClient := commonClient.NewMetaClient(cfg.MetaServiceURL, serviceTokenSource)
	duckdbService := service.NewDuckDBService(cfg, systemServiceClient, metaClient)
	log.Printf("✅ DuckDBService 初始化完成")

	// 7. 算子发现与工作流校验服务（动态发现工作流引擎）
	operatorDiscovery := service.NewOperatorDiscoveryService(systemServiceClient)
	log.Printf("✅ OperatorDiscoveryService 初始化完成")

	// 8. DevExecutor 统一执行器（执行前复用正式工作流校验）
	devExecutor := service.NewDevExecutor(devTaskRepo, taskExecutionRepo, workflowEngine, operatorDiscovery, metaClient, sqlEngine, duckdbService, notebookExecutionService)
	log.Printf("✅ DevExecutor 初始化完成（使用统一执行表）")
	toolApprovalService := service.NewToolApprovalService(db, devExecutor)

	cleanupService := service.NewCleanupService(db, redisClient, taskExecutionRepo)
	if err := cleanupService.Start(context.Background()); err != nil {
		log.Printf("Develop 资源回收服务启动失败: %v", err)
	}
	defer cleanupService.Stop()

	// ========== Handler 层 ==========
	devTaskHandler := api.NewDevTaskHandler(devTaskService)
	executionHandler := api.NewExecutionHandler(devExecutor, toolApprovalService)
	toolApprovalHandler := api.NewToolApprovalHandler(toolApprovalService)
	operatorHandler := api.NewOperatorHandler(operatorDiscovery)
	engineHandler := api.NewEngineHandler(systemServiceClient)
	queryHandler := api.NewQueryHandler(sqlEngine)
	notebookHandler := api.NewNotebookHandler(jupyterService, notebookExecutionService, devTaskService)
	duckdbHandler := api.NewDuckDBHandler(duckdbService)

	log.Printf("✅ Handler 层初始化完成")

	// ========== 设置路由 ==========
	router := api.SetupRouter(cfg, db, devTaskHandler, executionHandler, toolApprovalHandler, operatorHandler, engineHandler, queryHandler, notebookHandler, devTaskService, systemServiceClient, duckdbHandler)
	log.Printf("✅ 路由设置完成")

	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("develop")
	serviceURL := utils.BuildServiceURL(serviceHost, port)

	// ========== 模块注册（注册到 System service_registry）==========
	if cfg.SystemServiceURL != "" {
		systemServiceClient.RegisterAndHeartbeatWithMetadata(context.Background(), "develop", serviceURL, "/develop", map[string]interface{}{
			"module": "develop",
			"capabilities": map[string]interface{}{
				"cleanup_executor": map[string]interface{}{
					"enabled": true,
					"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
				},
			},
		})
	}

	// ========== 任务提供者注册（启动时自动注册到 System task_providers）==========
	if cfg.SystemServiceURL != "" {
		taskProviderRegistry := service.NewTaskProviderRegistryService(
			systemServiceClient,
			serviceURL,
		)

		// 后台异步注册（不阻塞启动，支持重试）
		go func() {
			time.Sleep(2 * time.Second) // 等待服务完全启动
			maxRetries := 5
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := taskProviderRegistry.Register(context.Background()); err != nil {
					log.Printf("⚠️  Registration attempt %d/%d failed: %v", attempt, maxRetries, err)
					time.Sleep(time.Duration(attempt*2) * time.Second) // 指数退避
					continue
				}
				log.Printf("✅ Develop task provider registered successfully")
				return
			}
			log.Printf("❌ Develop task provider registration failed after %d attempts", maxRetries)
		}()
	}

	// 设置优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// 启动服务器（非阻塞）
	addr := cfg.ServerAddr
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
