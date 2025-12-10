package main

import (
	"fmt"
	"log"
	"time"

	"github.com/addp/orchestrator/internal/api"
	"github.com/addp/orchestrator/internal/config"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// 加载配置
	cfg := config.LoadConfig()

	// 连接数据库
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSchema)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	// 自动迁移
	if err := db.AutoMigrate(&models.Orchestration{}, &models.Execution{}); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	log.Println("✅ 数据库连接成功")

	// 初始化 Repository
	orchRepo := repository.NewOrchestrationRepository(db)
	execRepo := repository.NewExecutionRepository(db)

	// 初始化 EngineRegistry（从 System 动态加载引擎）
	engineRegistry := service.NewEngineRegistry(
		cfg.SystemServiceURL,
		cfg.InternalAPIKey,
		5*time.Minute, // 缓存 TTL
	)

	// 初始化 TaskClient（通用任务客户端）
	taskClient := service.NewTaskClient(30 * time.Second)

	// 初始化 ModuleClient（向后兼容旧的硬编码模式）
	moduleClient := service.NewModuleClient(map[string]string{
		"transfer": cfg.TransferServiceURL,
		"meta":     cfg.MetaServiceURL,
		"manager":  cfg.ManagerServiceURL,
	})

	// 初始化 Executor（支持新旧两种模式）
	executor := service.NewExecutor(execRepo, orchRepo, engineRegistry, taskClient, moduleClient)

	// 初始化 Scheduler
	scheduler := service.NewScheduler(orchRepo, execRepo, executor)
	if err := scheduler.Start(); err != nil {
		log.Fatalf("调度器启动失败: %v", err)
	}
	defer scheduler.Stop()

	log.Println("✅ 调度器启动成功")
	log.Println("✅ 引擎注册表已初始化（从 System 动态加载）")

	// 设置路由
	router := api.SetupRouter(orchRepo, execRepo, executor, scheduler, moduleClient)

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("🚀 Orchestrator 服务启动: %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
