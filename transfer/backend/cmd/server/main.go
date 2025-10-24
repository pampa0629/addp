package main

import (
	"fmt"
	"log"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/transfer/internal/api"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/worker"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// 加载 .env 文件（从项目根目录，使用无参数版本）
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.Load()

	// 连接数据库
	db, err := connectDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 初始化 Repository 层
	_ = repository.NewMappingRepository(db) // mappingRepo unused for now

	// 创建任务队列（连接 Redis）
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	taskQueue := worker.NewTaskQueue(redisAddr, cfg.RedisPassword)
	defer taskQueue.Close()

	log.Printf("✅ Task queue connected to Redis: %s", redisAddr)

	// 初始化 Pipeline 组件（简化版 - 暂时不启动）
	// registry := pipeline.NewConnectorRegistry()
	// if err := connector.RegisterAllConnectors(registry); err != nil {
	// 	log.Fatalf("Failed to register connectors: %v", err)
	// }

	// 初始化 Service 层（传入 taskQueue）
	taskService := service.NewTaskService(db, nil, cfg, taskQueue) // engine 传 nil（暂不执行任务）
	executionService := service.NewExecutionService(db)
	localResourceService := service.NewLocalResourceService(db, cfg)

	// 设置路由
	router := api.SetupRouter(taskService, executionService, localResourceService, cfg.JWTSecret)

	// 启动服务器
	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🚀 Transfer service starting on %s", addr)
	log.Printf("📊 Database: %s@%s:%s/%s (schema: %s)",
		cfg.DBUser, cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBSchema)
	log.Printf("🔗 System Service: %s", cfg.SystemServiceURL)
	log.Printf("✅ Health check: http://localhost%s/health", addr)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// connectDatabase 连接数据库
func connectDatabase(cfg *config.Config) (*gorm.DB, error) {
	// 构建 DSN（注意：DBPort 是 string 类型，使用 %s）
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSchema,
	)

	// 连接数据库
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	log.Println("✅ Database connected successfully")

	// 自动迁移（创建表）
	if err := autoMigrate(db); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return db, nil
}

// autoMigrate 自动迁移数据库表
func autoMigrate(db *gorm.DB) error {
	log.Println("🔄 Running database migrations...")

	// 导入所有需要迁移的模型（暂时不包括 Checkpoint）
	err := db.AutoMigrate(
		&models.Task{},
		&models.TaskExecution{},
		&models.DataMapping{},
		&models.LocalResource{},
		// &pipeline.Checkpoint{}, // TODO: 启用 pipeline 时取消注释
	)

	if err != nil {
		return err
	}

	log.Println("✅ Database migrations completed")
	return nil
}
