package main

import (
	"context"
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/utils"
	"github.com/addp/standard/internal/api"
	"github.com/addp/standard/internal/config"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title           ADDP Standard API
// @version         1.0
// @host      localhost:8110
// @BasePath  /api/v1/standard
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.DBSchema)).Error; err != nil {
		log.Fatalf("Failed to create schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}

	// 自动迁移所有表
	if err := db.AutoMigrate(
		&models.Domain{},
		&models.Glossary{},
		&models.GlossaryElementMapping{},
		&models.Element{},
		&models.CodeSet{},
		&models.CodeItem{},
		&models.MeasurementCategory{},
		&models.Unit{},
		&models.Classification{},
		&models.GradingLevel{},
		&models.MetricCategory{},
		&models.Metric{},
		&models.MetricElementMapping{},
		&models.MetricDependency{},
		&models.Document{},
		&models.DocumentElementMapping{},
		&models.DocumentGlossaryMapping{},
		&models.DocumentMetricMapping{},
		&models.DimensionHierarchy{},
		&models.DimensionHierarchyLevel{},
	); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		defer redisClient.Close()
	}

	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemURL, cfg.InternalAPIKey)

	// 创建 Repositories
	domainRepo := repository.NewDomainRepository(db)
	glossaryRepo := repository.NewGlossaryRepository(db)
	elementRepo := repository.NewElementRepository(db)
	codeSetRepo := repository.NewCodeSetRepository(db)
	mCatRepo := repository.NewMeasurementCategoryRepository(db)
	unitRepo := repository.NewUnitRepository(db)
	classificationRepo := repository.NewClassificationRepository(db)
	gradingRepo := repository.NewGradingLevelRepository(db)
	metricCatRepo := repository.NewMetricCategoryRepository(db)
	metricRepo := repository.NewMetricRepository(db)
	documentRepo := repository.NewDocumentRepository(db)
	dimHierarchyRepo := repository.NewDimensionHierarchyRepository(db)

	// 初始化 MinIO 客户端（用于文档文件存储）
	var minioClient *minio.Client
	if mc, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	}); err != nil {
		log.Printf("⚠️  MinIO 初始化失败: %v", err)
	} else {
		minioClient = mc
		log.Printf("✅ MinIO 已连接: %s", cfg.MinioEndpoint)
	}

	// 创建 Services
	domainSvc := service.NewDomainService(domainRepo)
	glossarySvc := service.NewGlossaryService(glossaryRepo)
	elementSvc := service.NewElementService(elementRepo)
	codeSetSvc := service.NewCodeSetService(codeSetRepo)
	unitSvc := service.NewUnitService(mCatRepo, unitRepo)
	classificationSvc := service.NewClassificationService(classificationRepo, gradingRepo)
	metricSvc := service.NewMetricService(metricCatRepo, metricRepo)
	documentSvc := service.NewDocumentService(documentRepo, minioClient)
	dimHierarchySvc := service.NewDimensionHierarchyService(dimHierarchyRepo)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	cleanupSvc := service.NewCleanupService(db, redisClient, taskExecutionRepo, minioClient)
	if err := cleanupSvc.Start(context.Background()); err != nil {
		log.Printf("Standard 资源回收执行方启动失败: %v", err)
	}
	defer cleanupSvc.Stop()

	router := api.SetupRouter(
		db,
		domainSvc,
		glossarySvc,
		elementSvc,
		codeSetSvc,
		unitSvc,
		classificationSvc,
		metricSvc,
		documentSvc,
		dimHierarchySvc,
		cfg.SystemURL,
		cfg.InternalAPIKey,
	)

	addr := ":" + cfg.Port
	log.Printf("Standard service starting on %s", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("standard")
	serviceURL := utils.BuildServiceURL(serviceHost, port)
	systemClient.RegisterAndHeartbeatWithMetadata("standard", serviceURL, "/standard", map[string]interface{}{
		"module": "standard",
		"capabilities": map[string]interface{}{
			"cleanup_executor": map[string]interface{}{
				"enabled": true,
			},
		},
	})

	select {}
}
