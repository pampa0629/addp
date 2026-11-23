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

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/common/schema"
	"github.com/addp/workbench/internal/api"
	"github.com/addp/workbench/internal/config"
	"github.com/addp/workbench/internal/repository"
	"github.com/addp/workbench/internal/service"
	"gorm.io/gorm"
)

// @title ADDP Workbench API
// @version 1.0
// @host localhost:8193
// @BasePath /api/v1/workbench
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "prepare-database" {
			fmt.Fprintln(os.Stderr, "expected prepare-database")
			os.Exit(2)
		}
		db, err := bootstrap.OpenPlatformInstallation(config.PlatformDatabase(), "workbench")
		if err == nil {
			err = repository.Migrate(db)
		}
		if err != nil {
			var pending *schema.PendingMySQLTriggers
			if errors.As(err, &pending) {
				if err := pending.WriteSQL(os.Stdout, "workbench"); err != nil {
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
		log.Fatalf("Failed to load Workbench config: %v", err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	db, err := bootstrap.OpenPlatformRuntime(config.PlatformDatabase(), "workbench", &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Failed to connect Workbench database: %v", err)
	}
	if err := schema.Require(db, "workbench", repository.SchemaVersion); err != nil {
		log.Fatalf("Workbench database version check failed: %v", err)
	}
	descriptorReader, err := service.NewHTTPDescriptorReader(cfg.ServiceURL, nil)
	if err != nil {
		log.Fatalf("Failed to initialize Service Consumer Descriptor client: %v", err)
	}
	resourceAccessRepository := repository.NewResourceAccessRuleRepository(db)
	dataApplicationService := service.NewDataApplicationService(repository.NewDataApplicationRepository(db), descriptorReader, resourceAccessRepository)
	catalogResourceService := service.NewCatalogResourceService(repository.NewCatalogResourceRepository(db))
	resourceGrantService := service.NewResourceGrantService(resourceAccessRepository)

	tokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-workbench", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Failed to initialize Workbench service token source: %v", err)
	}
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemURL, tokenSource, nil)
	lifecycle := modulelifecycle.NewBusiness("workbench", commonClient.ModuleRuntimeRoleBackend, databaseReadyCheck(db))
	router := api.SetupRouter(cfg.SystemURL, lifecycle, dataApplicationService, catalogResourceService, resourceGrantService)
	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("Failed to bind Workbench listener: %v", err)
	}
	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Workbench HTTP server stopped: %v", err)
			stopRuntime()
		}
	}()

	serviceURL := commonConfig.BuildServiceURL(commonConfig.GetServiceHost(), cfg.Port)
	registration := systemClient.RegisterAndHeartbeatWithMetadata(runtimeContext, "workbench", serviceURL, map[string]interface{}{"module": "workbench"})
	lifecycle.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)
	<-runtimeContext.Done()
	<-registration.Done()
}

func databaseReadyCheck(db *gorm.DB) modulelifecycle.CheckFunc {
	return func(ctx context.Context) modulelifecycle.CheckResult {
		sqlDB, err := db.DB()
		if err == nil {
			err = sqlDB.PingContext(ctx)
		}
		if err != nil {
			return modulelifecycle.CheckResult{Name: "workbench_database", Status: modulelifecycle.CheckNotReady, ErrorCode: "workbench_database_unavailable"}
		}
		return modulelifecycle.CheckResult{Name: "workbench_database", Status: modulelifecycle.CheckReady}
	}
}
