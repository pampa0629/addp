package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/api"
	"github.com/addp/quality/internal/config"
	"github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
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
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}

	migrationContext, cancelMigration := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelMigration()
	if err := migration.NewRunner(db).Run(migrationContext); err != nil {
		log.Fatalf("Failed to migrate Quality database: %v", err)
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
	modelClient := commonClient.NewModelClient(cfg.ModelURL, serviceTokenSource, nil)
	executionAuthorizationClient := commonClient.NewSystemExecutionAuthorizationClient(cfg.SystemURL, nil)

	// Repositories
	ruleAppRepo := repository.NewRuleApplicationRepository(db)
	checkTaskRepo := repository.NewCheckTaskRepository(db)
	gateTaskRepo := repository.NewMaterializationGateRepository(db)
	issueRepo := repository.NewIssueRepository(db)
	catalogSummaryRepo := repository.NewCatalogSummaryRepository(db)

	// Services
	ruleEngineSvc := service.NewRuleEngineService(standardClient, systemServiceClient, ruleAppRepo)
	checkTaskSvc := service.NewCheckTaskService(checkTaskRepo, systemServiceClient)
	gateTaskSvc := service.NewMaterializationGateService(gateTaskRepo, modelClient, cfg.CheckTimeout)
	checkExecutor := service.NewCheckExecutor(systemServiceClient, executionAuthorizationClient, checkTaskRepo, issueRepo, cfg.CheckTimeout, cfg.WorkerConcurrency)
	checkExecutor.ConfigureMaterializationGate(modelClient, gateTaskRepo)
	issueSvc := service.NewIssueService(issueRepo)
	catalogSummarySvc := service.NewCatalogSummaryService(catalogSummaryRepo)
	cleanupService := service.NewCleanupService(db, redisClient, commonExecution.NewTaskExecutionRepository(db))
	if err := cleanupService.Start(runtimeContext); err != nil {
		log.Printf("Quality 资源回收服务启动失败: %v", err)
	}
	defer cleanupService.Stop()

	lifecycleController := modulelifecycle.NewBusiness("quality", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(
		ruleEngineSvc,
		checkTaskSvc,
		gateTaskSvc,
		checkExecutor,
		issueSvc,
		catalogSummarySvc,
		db,
		cfg.SystemURL,
		redisClient,
		lifecycleController,
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
	provider, err := service.QualityTaskProviderDeclaration()
	if err != nil {
		log.Fatalf("构建 Quality TaskProvider 声明失败: %v", err)
	}
	registration := systemServiceClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
		ModuleName: "quality", ModuleURL: serviceURL, RoutePrefix: "/quality", HealthCheckURL: serviceURL + "/health/ready",
		TaskProvider: provider,
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
