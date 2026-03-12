package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/utils"
	commonRepo "github.com/addp/common/repository"
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

	// 检查端口是否可用
	if err := utils.CheckPortAvailable(cfg.ServerAddr); err != nil {
		log.Fatalf("❌ 端口检查失败: %v", err)
	}

	log.Printf("🚀 Starting Develop Service on %s", cfg.ServerAddr)
	log.Printf("📦 Environment: %s", cfg.Env)
	log.Printf("🔗 System Service: %s", cfg.SystemServiceURL)
	log.Printf("🔗 Jupyter Engine: %s", cfg.JupyterEngineURL)

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		log.Fatalf("❌ Failed to initialize database: %v", err)
	}
	log.Printf("✅ Database initialized successfully")

	// ========== Repository 层 ==========
	devTaskRepo := repository.NewDevTaskRepository(db)
	// devExecutionRepo := repository.NewDevExecutionRepository(db) // 【已废弃】改用统一执行表
	taskExecutionRepo := commonRepo.NewTaskExecutionRepository(db) // 统一执行记录仓库
	log.Printf("✅ Repository 层初始化完成（使用统一执行表）")

	// ========== 创建 System Client ==========
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	log.Printf("✅ System Client 创建成功")

	// ========== Service 层 ==========
	// 1. 工作流引擎服务（从 System 动态获取引擎配置）
	workflowEngine := service.NewWorkflowEngineService(systemClient)
	log.Printf("✅ WorkflowEngineService 初始化完成")

	// 2. SQL引擎服务
	sqlEngine := service.NewSQLEngineService(cfg, systemClient)
	log.Printf("✅ SQLEngineService 初始化完成")

	// 3. Jupyter引擎服务
	jupyterService := service.NewJupyterService(cfg)
	log.Printf("✅ JupyterService 初始化完成")

	// 3.5 Jupyter实例管理服务（租户隔离的 Jupyter 容器管理 - Docker 方案,已废弃）
	jupyterInstanceService, err := service.NewJupyterInstanceService(cfg)
	if err != nil {
		log.Printf("⚠️  JupyterInstanceService 初始化失败: %v", err)
		jupyterInstanceService = nil
	} else {
		log.Printf("✅ JupyterInstanceService 初始化完成")
	}

	// 3.7 Jupyter虚拟环境管理服务（租户隔离的虚拟环境管理 - 开发环境新方案）
	jupyterVenvService, err := service.NewJupyterVenvService(cfg)
	if err != nil {
		log.Printf("⚠️  JupyterVenvService 初始化失败: %v", err)
		jupyterVenvService = nil
	} else {
		log.Printf("✅ JupyterVenvService 初始化完成")
	}

	// 3.6 NotebookExecutionService（Notebook 完整执行服务）
	notebookExecutionService, err := service.NewNotebookExecutionService(cfg, systemClient, jupyterService)
	if err != nil {
		log.Printf("⚠️  NotebookExecutionService 初始化失败: %v（将使用简化模式）", err)
		notebookExecutionService = nil // 允许降级到简化模式
	} else {
		log.Printf("✅ NotebookExecutionService 初始化完成")
	}

	// 4. DevItem业务逻辑服务
	devTaskService := service.NewDevTaskService(devTaskRepo)
	log.Printf("✅ DevTaskService 初始化完成")

	// 5. DevExecutor 统一执行器（使用统一执行表）
	devExecutor := service.NewDevExecutor(devTaskRepo, taskExecutionRepo, workflowEngine, sqlEngine, jupyterService, notebookExecutionService)
	log.Printf("✅ DevExecutor 初始化完成（使用统一执行表）")

	// 5. 算子发现服务（动态发现工作流引擎）
	operatorDiscovery := service.NewOperatorDiscoveryService(
		systemClient, // 新增：传入 System Client（动态发现引擎）
		cfg.MetaServiceURL,
		cfg.TransferServiceURL,
		cfg.ManagerServiceURL,
	)
	log.Printf("✅ OperatorDiscoveryService 初始化完成")

	// ========== Handler 层 ==========
	devItemHandler := api.NewDevItemHandler(devTaskService)
	devExecutionHandler := api.NewDevExecutionHandler(devExecutor)
	operatorHandler := api.NewOperatorHandler(operatorDiscovery)
	engineHandler := api.NewEngineHandler(systemClient)
	queryHandler := api.NewQueryHandler(sqlEngine, devTaskService)
	notebookHandler := api.NewNotebookHandler(jupyterService, notebookExecutionService, devTaskService)

	// Jupyter 实例管理 Handler (Docker 方案,已废弃)
	var jupyterInstanceHandler *api.JupyterInstanceHandler
	if jupyterInstanceService != nil {
		jupyterInstanceHandler = api.NewJupyterInstanceHandler(jupyterInstanceService)
		log.Printf("✅ JupyterInstanceHandler 初始化完成")
	}

	// Jupyter 虚拟环境管理 Handler (开发环境新方案)
	var jupyterVenvHandler *api.JupyterVenvHandler
	if jupyterVenvService != nil {
		jupyterVenvHandler = api.NewJupyterVenvHandler(jupyterVenvService)
		log.Printf("✅ JupyterVenvHandler 初始化完成")
	}

	log.Printf("✅ Handler 层初始化完成")

	// ========== 设置路由 ==========
	router := api.SetupRouter(cfg, db, devItemHandler, devExecutionHandler, operatorHandler, engineHandler, queryHandler, notebookHandler, jupyterInstanceHandler, jupyterVenvHandler, devTaskService, systemClient)
	log.Printf("✅ 路由设置完成")

	// ========== 模块注册（注册到 System service_registry）==========
	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		go func() {
			// 等待3秒确保服务完全启动
			time.Sleep(3 * time.Second)

			// 构建服务URL
			serviceHost := utils.GetServiceHost()
			port := utils.GetModulePort("develop")
			serviceURL := utils.BuildServiceURL(serviceHost, port)

			// 创建 System 客户端用于模块注册
			registryClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)

			// 注册服务
			registrationReq := &commonClient.ModuleRegistrationRequest{
				ModuleName:    "develop",
				ModuleURL:     serviceURL,
				RoutePrefix:    "/develop",
				HealthCheckURL: serviceURL + "/health",
				Metadata: map[string]interface{}{
					"module": "develop",
				},
			}

			maxRetries := 3
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := registryClient.RegisterModule(registrationReq); err != nil {
					log.Printf("⚠️  Develop 模块注册失败 (尝试 %d/%d): %v", attempt, maxRetries, err)
					time.Sleep(time.Duration(attempt*5) * time.Second)
					continue
				}
				log.Printf("✅ Develop 模块注册成功: %s", serviceURL)
				break
			}

			// 启动心跳
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := registryClient.SendHeartbeat("develop"); err != nil {
					log.Printf("❌ Develop 心跳失败: %v", err)
				}
			}
		}()
	}

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
