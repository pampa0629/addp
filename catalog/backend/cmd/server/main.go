package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/addp/catalog/internal/api"
	"github.com/addp/catalog/internal/config"
	"github.com/addp/catalog/internal/repository"
	"github.com/addp/catalog/internal/service"
	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/modulelifecycle"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title ADDP Catalog API
// @version 1.0
// @host localhost:8192
// @BasePath /api/v1/catalog
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load Catalog config: %v", err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Failed to connect Catalog database: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		log.Fatalf("Failed to migrate Catalog database: %v", err)
	}

	tokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-catalog", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Failed to initialize Catalog service token source: %v", err)
	}
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemURL, tokenSource, nil)
	metaClient := commonClient.NewMetaClient(cfg.MetaURL, tokenSource)
	modelClient := commonClient.NewModelClient(cfg.ModelURL, tokenSource, nil)
	standardClient := commonClient.NewStandardClient(cfg.StandardURL, tokenSource, nil)
	serviceClient := commonClient.NewServiceClient(cfg.ServiceURL, tokenSource, nil)
	developClient := commonClient.NewDevelopClient(cfg.DevelopURL, tokenSource, nil)
	workbenchClient := commonClient.NewWorkbenchClient(cfg.WorkbenchURL, tokenSource, nil)
	qualityClient := commonClient.NewQualityClient(cfg.QualityURL, tokenSource, nil)
	systemReferenceResolver := service.NewSystemClientReferenceResolver(systemClient)
	metaSyncService := service.NewSourceSyncService(db, service.NewTenantMetaChangeSource(metaClient))
	modelSyncService := service.NewProfessionalSourceSyncService(db, service.NewTenantModelChangeSource(modelClient))
	standardSyncService := service.NewProfessionalSourceSyncService(db, service.NewTenantStandardChangeSource(standardClient))
	serviceSyncService := service.NewProfessionalSourceSyncService(db, service.NewTenantServiceChangeSource(serviceClient))
	developSyncService := service.NewProfessionalSourceSyncService(db, service.NewTenantDevelopChangeSource(developClient))
	workbenchSyncService := service.NewProfessionalSourceSyncService(db, service.NewTenantWorkbenchChangeSource(workbenchClient))
	syncRunner := service.NewSourceSyncRunner(db, cfg.SourceSyncInterval, systemClient, metaSyncService, modelSyncService, standardSyncService, serviceSyncService, developSyncService, workbenchSyncService)
	syncRunner.Start(runtimeContext)
	governanceTaskService := service.NewGovernanceTaskService(db, systemReferenceResolver)
	responsibilityRunner := service.NewResponsibilityReconciliationRunner(
		governanceTaskService, syncRunner, cfg.ResponsibilityReconciliationInterval,
	)
	responsibilityRunner.Start(runtimeContext)
	searchIndex, err := service.NewMeilisearchCatalogIndex(cfg.MeilisearchURL, cfg.MeilisearchAPIKey, cfg.MeilisearchIndex)
	if err != nil {
		log.Fatalf("Failed to initialize Catalog search projection: %v", err)
	}
	projectionWorker := service.NewProjectionWorker(db, searchIndex, cfg.ProjectionInterval)
	projectionWorker.Start(runtimeContext)

	lifecycle := modulelifecycle.NewBusiness(
		"catalog", commonClient.ModuleRuntimeRoleBackend,
		databaseReadyCheck(db), meilisearchReadyCheck(searchIndex),
	)
	entryService := service.NewEntryService(
		db,
		service.NewStandardClientReferenceResolver(standardClient),
		systemReferenceResolver,
	).WithReferenceCandidateResolvers(
		service.NewStandardClientCandidateResolver(standardClient),
		service.NewSystemClientCandidateResolver(systemClient),
	).WithEngineReferenceResolver(service.NewSystemClientEngineReferenceResolver(systemClient)).WithSearch(searchIndex).WithProfessionalSourceResolvers(
		service.NewModelClientSourceResolver(modelClient),
		service.NewStandardClientSourceResolver(standardClient),
		service.NewServiceClientSourceResolver(serviceClient),
		service.NewDevelopClientSourceResolver(developClient),
		service.NewWorkbenchClientSourceResolver(workbenchClient),
	).WithQualitySummaryResolver(service.NewQualityClientSummaryResolver(qualityClient)).WithDataDictionaryResolvers(
		service.NewMetaClientFieldResolver(metaClient),
		service.NewStandardClientElementRevisionResolver(standardClient),
	)
	personalCatalogService := service.NewPersonalCatalogService(db, entryService)
	collectionService := service.NewCollectionService(db, entryService).WithSystemReferenceResolver(systemReferenceResolver)
	router := api.SetupRouter(cfg.SystemURL, lifecycle, entryService, governanceTaskService, personalCatalogService, collectionService, syncRunner)
	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("Failed to bind Catalog listener: %v", err)
	}
	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Catalog HTTP server stopped: %v", err)
			stopRuntime()
		}
	}()

	serviceURL := commonConfig.BuildServiceURL(commonConfig.GetServiceHost(), cfg.Port)
	registration := systemClient.RegisterAndHeartbeatWithMetadata(runtimeContext, "catalog", serviceURL, "/catalog", map[string]interface{}{
		"module": "catalog",
	})
	lifecycle.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)

	<-runtimeContext.Done()
	<-registration.Done()
}

func meilisearchReadyCheck(index *service.MeilisearchCatalogIndex) modulelifecycle.CheckFunc {
	return func(context.Context) modulelifecycle.CheckResult {
		if err := index.Health(); err != nil {
			return modulelifecycle.CheckResult{Name: "catalog_meilisearch", Status: modulelifecycle.CheckNotReady, ErrorCode: "catalog_meilisearch_unavailable"}
		}
		return modulelifecycle.CheckResult{Name: "catalog_meilisearch", Status: modulelifecycle.CheckReady}
	}
}

func databaseReadyCheck(db *gorm.DB) modulelifecycle.CheckFunc {
	return func(ctx context.Context) modulelifecycle.CheckResult {
		sqlDB, err := db.DB()
		if err == nil {
			err = sqlDB.PingContext(ctx)
		}
		if err != nil {
			return modulelifecycle.CheckResult{Name: "catalog_database", Status: modulelifecycle.CheckNotReady, ErrorCode: "catalog_database_unavailable"}
		}
		return modulelifecycle.CheckResult{Name: "catalog_database", Status: modulelifecycle.CheckReady}
	}
}
