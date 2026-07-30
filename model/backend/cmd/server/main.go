package main

import (
	"context"
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/utils"
	_ "github.com/addp/model/i18n"
	"github.com/addp/model/internal/api"
	"github.com/addp/model/internal/config"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/addp/model/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title           ADDP Model API
// @version         1.0
// @host      localhost:8181
// @BasePath  /api/v1/model
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 连接数据库
	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 确保 model schema 存在
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.DBSchema)).Error; err != nil {
		log.Fatalf("Failed to create schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}

	// 自动迁移 model schema 表（仅 Model 相关）
	if err := db.AutoMigrate(
		&models.DWLayer{},
		&models.Entity{},
		&models.EntityAttribute{},
		&models.EntityRelation{},
		&models.LogicalTable{},
		&models.LogicalField{},
		&models.TableRelation{},
		&models.FactMetricMapping{},
	); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// 连接 Redis
	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		defer redisClient.Close()
	}

	// 创建 System 客户端
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemURL, cfg.InternalAPIKey)

	// 创建 Repositories（仅 Model 相关）
	entityRepo := repository.NewEntityRepository(db)
	entityRelationRepo := repository.NewEntityRelationRepository(db)
	logicalTableRepo := repository.NewLogicalTableRepository(db)
	dwLayerRepo := repository.NewDWLayerRepository(db)
	factMetricRepo := repository.NewFactMetricRepository(db)
	tableRelationRepo := repository.NewTableRelationRepository(db)

	// 创建 Services（仅 Model 相关，传入 standardURL 用于验证 element_id）
	entitySvc := service.NewEntityService(entityRepo, entityRelationRepo, cfg.StandardURL, cfg.InternalAPIKey)
	entityRelationSvc := service.NewEntityRelationService(entityRelationRepo, entityRepo)
	logicalTableSvc := service.NewLogicalTableService(logicalTableRepo, cfg.StandardURL, cfg.InternalAPIKey)
	dwLayerSvc := service.NewDWLayerService(dwLayerRepo)
	factMetricSvc := service.NewFactMetricService(factMetricRepo, logicalTableRepo)
	tableRelationSvc := service.NewTableRelationService(tableRelationRepo, logicalTableRepo)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	cleanupSvc := service.NewCleanupService(db, redisClient, taskExecutionRepo)
	if err := cleanupSvc.Start(context.Background()); err != nil {
		log.Printf("Model 资源回收执行方启动失败: %v", err)
	}
	defer cleanupSvc.Stop()

	// 设置路由
	router := api.SetupRouter(
		entitySvc,
		entityRelationSvc,
		logicalTableSvc,
		dwLayerSvc,
		factMetricSvc,
		tableRelationSvc,
		cfg.SystemURL,
		cfg.StandardURL,
		redisClient,
	)

	// 启动服务
	addr := ":" + cfg.Port
	log.Printf("Model service starting on %s", addr)

	// 后台启动服务器
	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 启动模块注册和心跳
	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("model")
	serviceURL := utils.BuildServiceURL(serviceHost, port)
	systemClient.RegisterAndHeartbeatWithMetadata("model", serviceURL, "/model", map[string]interface{}{
		"module": "model",
		"capabilities": map[string]interface{}{
			"cleanup_executor": map[string]interface{}{
				"enabled": true,
				"causes":  []string{events.CleanupCauseTenantDeleted},
			},
		},
	})

	// 阻塞主 goroutine
	select {}
}
