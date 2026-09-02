package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
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
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

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
	metricCatRepo := repository.NewMetricCategoryRepository(db)
	metricRepo := repository.NewMetricRepository(db)
	documentRepo := repository.NewDocumentRepository(db)
	dimHierarchyRepo := repository.NewDimensionHierarchyRepository(db)
	tenantReferenceRepo := repository.NewTenantReferenceRepository(db)
	referenceResolutionRepo := repository.NewReferenceResolutionRepository(db)
	catalogResourceRepo := repository.NewCatalogResourceRepository(db)
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
	elementSvc := service.NewElementService(elementRepo, codeSetRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	codeSetSvc := service.NewCodeSetService(codeSetRepo, tenantReferenceRepo)
	unitSvc := service.NewUnitService(mCatRepo, unitRepo)
	metricSvc := service.NewMetricService(metricCatRepo, metricRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	documentSvc := service.NewDocumentService(documentRepo, tenantReferenceRepo, minioClient, service.DocumentStorageOptions{
		MaxFileSize: cfg.DocumentMaxFileSize,
		Timeout:     cfg.DocumentStorageTimeout,
	})
	defer documentSvc.Stop()
	dimHierarchySvc := service.NewDimensionHierarchyService(dimHierarchyRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	referenceResolutionSvc := service.NewReferenceResolutionService(referenceResolutionRepo)
	elementRevisionResolutionSvc := service.NewElementRevisionResolutionService(elementRepo, codeSetRepo)
	catalogResourceSvc := service.NewCatalogResourceService(catalogResourceRepo)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	cleanupSvc := service.NewCleanupService(db, redisClient, taskExecutionRepo, minioClient)
	if err := cleanupSvc.Start(runtimeContext); err != nil {
		log.Printf("Standard 资源回收执行方启动失败: %v", err)
	}
	defer cleanupSvc.Stop()
	standardReferenceDeletionSvc.Start(runtimeContext)
	defer standardReferenceDeletionSvc.Stop()

	lifecycleController := modulelifecycle.NewBusiness("standard", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(
		db,
		domainSvc,
		glossarySvc,
		elementSvc,
		codeSetSvc,
		unitSvc,
		metricSvc,
		documentSvc,
		dimHierarchySvc,
		referenceResolutionSvc,
		elementRevisionResolutionSvc,
		catalogResourceSvc,
		cfg.SystemURL,
		lifecycleController,
	)

	addr := ":" + cfg.Port
	log.Printf("Standard service starting on %s", addr)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to bind Standard listener: %v", err)
	}

	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Failed to start server: %v", err)
			stopRuntime()
		}
	}()

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	registration := systemClient.RegisterAndHeartbeatWithMetadata(runtimeContext, "standard", serviceURL, "/standard", map[string]interface{}{
		"module": "standard",
		"capabilities": map[string]interface{}{
			"cleanup_executor": map[string]interface{}{
				"enabled": true,
				"causes":  []string{events.CleanupCauseTenantDeleted},
			},
		},
	})
	lifecycleController.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)

	<-runtimeContext.Done()
	<-registration.Done()
}
