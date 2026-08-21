package main

import (
	"context"
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/standard/internal/api"
	"github.com/addp/standard/internal/config"
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

	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}

	if err := repository.Migrate(db); err != nil {
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

	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemURL, "addp-standard", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)
	modelClient := commonClient.NewModelClient(cfg.ModelURL, serviceTokenSource, nil)

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
	tenantReferenceRepo := repository.NewTenantReferenceRepository(db)
	standardReferenceDeletionSvc := service.NewStandardReferenceDeletionService(db, modelClient)
	standardReferenceDeletionSvc.RegisterLocalDelete("domain", func(tx *gorm.DB, resourceID, tenantID int64) error {
		return domainRepo.DeleteTx(tx, resourceID, tenantID)
	})
	standardReferenceDeletionSvc.RegisterLocalDelete("element", func(tx *gorm.DB, resourceID, tenantID int64) error {
		return elementRepo.DeleteTx(tx, resourceID, tenantID)
	})
	standardReferenceDeletionSvc.RegisterLocalDelete("dimension_hierarchy", func(tx *gorm.DB, resourceID, tenantID int64) error {
		return dimHierarchyRepo.DeleteTx(tx, resourceID, tenantID)
	})
	standardReferenceDeletionSvc.RegisterLocalDelete("metric", func(tx *gorm.DB, resourceID, tenantID int64) error {
		return metricRepo.DeleteTx(tx, resourceID, tenantID)
	})

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
	domainSvc := service.NewDomainService(domainRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	glossarySvc := service.NewGlossaryService(glossaryRepo, tenantReferenceRepo)
	elementSvc := service.NewElementService(elementRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	codeSetSvc := service.NewCodeSetService(codeSetRepo)
	unitSvc := service.NewUnitService(mCatRepo, unitRepo)
	classificationSvc := service.NewClassificationService(classificationRepo, gradingRepo, tenantReferenceRepo)
	metricSvc := service.NewMetricService(metricCatRepo, metricRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	documentSvc := service.NewDocumentService(documentRepo, tenantReferenceRepo, minioClient, service.DocumentStorageOptions{
		MaxFileSize: cfg.DocumentMaxFileSize,
		Timeout:     cfg.DocumentStorageTimeout,
	})
	defer documentSvc.Stop()
	dimHierarchySvc := service.NewDimensionHierarchyService(dimHierarchyRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	cleanupSvc := service.NewCleanupService(db, redisClient, taskExecutionRepo, minioClient)
	if err := cleanupSvc.Start(context.Background()); err != nil {
		log.Printf("Standard 资源回收执行方启动失败: %v", err)
	}
	defer cleanupSvc.Stop()
	standardReferenceDeletionSvc.Start(context.Background())
	defer standardReferenceDeletionSvc.Stop()

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
	)

	addr := ":" + cfg.Port
	log.Printf("Standard service starting on %s", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	systemClient.RegisterAndHeartbeatWithMetadata(context.Background(), "standard", serviceURL, "/standard", map[string]interface{}{
		"module": "standard",
		"capabilities": map[string]interface{}{
			"cleanup_executor": map[string]interface{}{
				"enabled": true,
				"causes":  []string{events.CleanupCauseTenantDeleted},
			},
		},
	})

	select {}
}
