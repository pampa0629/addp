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
	"github.com/addp/common/events"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/common/schema"
	_ "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/api"
	"github.com/addp/quality/internal/config"
	"github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// @title           ADDP Quality API
// @version         1.0
// @host      localhost:8182
// @BasePath  /api/v1/quality
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "prepare-database" {
			fmt.Fprintln(os.Stderr, "expected prepare-database")
			os.Exit(2)
		}
		db, err := bootstrap.OpenPlatformInstallation(cfg.PlatformDatabase(), "quality")
		if err != nil {
			log.Fatal(err)
		}
		if err := migration.PrepareDatabase(db); err != nil {
			var pending *schema.PendingMySQLTriggers
			if errors.As(err, &pending) {
				if err := pending.WriteSQL(os.Stdout, "quality"); err != nil {
					log.Fatal(err)
				}
				os.Exit(3)
			}
			log.Fatal(err)
		}
		return
	}

	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	db, err := bootstrap.OpenPlatformRuntime(cfg.PlatformDatabase(), "quality", &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}

	if err := schema.Require(db, "quality", migration.SchemaVersion); err != nil {
		log.Fatal(err)
	}

	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-quality", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)
	standardClient := commonClient.NewStandardClient(cfg.StandardURL, serviceTokenSource, nil)
	executionAuthorizationClient := commonClient.NewSystemExecutionAuthorizationClient(cfg.SystemURL, nil)

	// Repositories
	planRepo := repository.NewPlanRepository(db)
	issueRepo := repository.NewIssueRepository(db)
	catalogSummaryRepo := repository.NewCatalogSummaryRepository(db)

	// Services
	planSvc := service.NewPlanService(planRepo, cfg.CheckTimeout).WithClients(systemServiceClient, executionAuthorizationClient)
	planSvc.WithStandardClient(standardClient)
	ruleSvc := service.NewRuleService(repository.NewRuleRepository(db), standardClient)
	issueSvc := service.NewIssueService(issueRepo)
	catalogSummarySvc := service.NewCatalogSummaryService(catalogSummaryRepo)
	standardReferenceGuardSvc := service.NewStandardReferenceGuardService(repository.NewStandardReferenceGuardRepository(db))
	cleanupService := service.NewCleanupService(db, systemServiceClient)
	if err := cleanupService.Start(runtimeContext); err != nil {
		log.Printf("Quality 资源回收服务启动失败: %v", err)
	}
	defer cleanupService.Stop()

	lifecycleController := modulelifecycle.NewBusiness("quality", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(
		planSvc,
		ruleSvc,
		issueSvc,
		catalogSummarySvc,
		db,
		cfg.SystemURL,
		redisClient,
		lifecycleController,
		standardReferenceGuardSvc,
	)

	addr := ":" + cfg.Port
	log.Printf("Quality service starting on %s", addr)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to bind Quality listener: %v", err)
	}

	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Failed to start server: %v", err)
			stopRuntime()
		}
	}()

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	registration := systemServiceClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
		ModuleName: "quality", ModuleURL: serviceURL, HealthCheckURL: serviceURL + "/health/ready",
		Metadata: map[string]interface{}{
			"module": "quality",
			"capabilities": map[string]interface{}{
				"cleanup_executor": map[string]interface{}{
					"enabled": true,
					"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
				},
			},
		},
	})
	lifecycleController.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)

	<-runtimeContext.Done()
	<-registration.Done()
}
