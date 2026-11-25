package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	dbbootstrap "github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/schema"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	"github.com/addp/common/infra/objectstore"
	"github.com/addp/common/infra/objectstore/bootstrap"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/standard/internal/api"
	"github.com/addp/standard/internal/config"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
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
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "prepare-database" {
			fmt.Fprintln(os.Stderr, "expected prepare-database")
			os.Exit(2)
		}
		db, err := dbbootstrap.OpenPlatformInstallation(platformDatabase(), "standard")
		if err != nil {
			log.Fatal(err)
		}
		if err := repository.PrepareDatabase(db); err != nil {
			var pending *schema.PendingMySQLTriggers
			if errors.As(err, &pending) {
				if err := pending.WriteSQL(os.Stdout, "standard"); err != nil {
					log.Fatal(err)
				}
				os.Exit(3)
			}
			log.Fatal(err)
		}
		return
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	db, err := openDatabase()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}

	if err := schema.Require(db, "standard", repository.SchemaVersion); err != nil {
		log.Fatalf("Failed to verify Standard database: %v", err)
	}

	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemURL, "addp-standard", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)
	modelClient := commonClient.NewModelClient(cfg.ModelURL, serviceTokenSource, nil)
	qualityClient := commonClient.NewQualityClient(cfg.QualityURL, serviceTokenSource, nil)

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
	tenantReferenceRepo := repository.NewTenantReferenceRepository(db)
	referenceResolutionRepo := repository.NewReferenceResolutionRepository(db)
	catalogResourceRepo := repository.NewCatalogResourceRepository(db)
	standardReferenceDeletionSvc := service.NewStandardReferenceDeletionService(db, modelClient, qualityClient)
	standardReferenceDeletionSvc.RegisterLocalDelete("domain", func(tx *gorm.DB, resourceID, tenantID int64) error {
		return domainRepo.DeleteTx(tx, resourceID, tenantID)
	})
	standardReferenceDeletionSvc.RegisterLocalDelete("element", func(tx *gorm.DB, resourceID, tenantID int64) error {
		return elementRepo.DeleteTx(tx, resourceID, tenantID)
	})
	standardReferenceDeletionSvc.RegisterLocalDelete("metric", func(tx *gorm.DB, resourceID, tenantID int64) error {
		return metricRepo.DeleteTx(tx, resourceID, tenantID)
	})

	areaContext, cancelAreaCheck := context.WithTimeout(runtimeContext, cfg.DocumentStorageTimeout)
	documentArea, err := bootstrap.OpenObjectArea(areaContext, objectstore.Standard)
	cancelAreaCheck()
	if err != nil {
		log.Fatalf("Failed to bind Standard document storage: %v", err)
	}

	// 创建 Services
	domainSvc := service.NewDomainService(domainRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	glossarySvc := service.NewGlossaryService(glossaryRepo, tenantReferenceRepo)
	elementSvc := service.NewElementService(elementRepo, codeSetRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	codeSetSvc := service.NewCodeSetService(codeSetRepo, tenantReferenceRepo)
	unitSvc := service.NewUnitService(mCatRepo, unitRepo)
	metricSvc := service.NewMetricService(metricCatRepo, metricRepo, tenantReferenceRepo, standardReferenceDeletionSvc)
	documentSvc := service.NewDocumentService(documentRepo, tenantReferenceRepo, documentArea, service.DocumentStorageOptions{
		MaxFileSize:        cfg.DocumentMaxFileSize,
		Timeout:            cfg.DocumentStorageTimeout,
		CopilotURL:         cfg.CopilotURL,
		ServiceTokenSource: serviceTokenSource,
	})
	defer documentSvc.Stop()
	referenceResolutionSvc := service.NewReferenceResolutionService(referenceResolutionRepo)
	elementRevisionResolutionSvc := service.NewElementRevisionResolutionService(elementRepo, codeSetRepo)
	catalogResourceSvc := service.NewCatalogResourceService(catalogResourceRepo)
	cleanupSvc := service.NewCleanupService(db, systemClient, documentArea)
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
	registration := systemClient.RegisterAndHeartbeatWithMetadata(runtimeContext, "standard", serviceURL, map[string]interface{}{
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

func platformDatabase() database.Config {
	return database.Config{Host: commonConfig.GetEnv("POSTGRES_HOST", "localhost"), Port: commonConfig.GetEnv("POSTGRES_PORT", "15432"), DBName: commonConfig.GetEnv("POSTGRES_DB", "addp"), Schema: "standard"}
}

func openDatabase() (*gorm.DB, error) {
	return dbbootstrap.OpenPlatformRuntime(platformDatabase(), "standard", &gorm.Config{TranslateError: true})
}
