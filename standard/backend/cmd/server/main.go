package main

import (
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
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
		redisClient,
	)

	addr := ":" + cfg.Port
	log.Printf("Standard service starting on %s", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	go func() {
		time.Sleep(2 * time.Second)

		serviceURL := fmt.Sprintf("http://localhost:%s", cfg.Port)
		registrationReq := &commonClient.ModuleRegistrationRequest{
			ModuleName:     "standard",
			ModuleURL:      serviceURL,
			RoutePrefix:    "/standard",
			HealthCheckURL: serviceURL + "/health",
			Metadata: map[string]interface{}{
				"module": "standard",
			},
		}

		maxRetries := 3
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if err := systemClient.RegisterModule(registrationReq); err != nil {
				log.Printf("⚠️  Standard 模块注册失败 (尝试 %d/%d): %v", attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*5) * time.Second)
				continue
			}
			log.Printf("✅ Standard 模块注册成功: %s", serviceURL)
			break
		}

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := systemClient.SendHeartbeat("standard"); err != nil {
				log.Printf("⚠️  Standard 心跳失败: %v", err)
			}
		}
	}()

	select {}
}
