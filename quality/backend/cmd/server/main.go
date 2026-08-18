package main

import (
	"context"
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
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
	executionAuthorizationClient := commonClient.NewSystemExecutionAuthorizationClient(cfg.SystemURL, nil)

	// Repositories
	ruleAppRepo := repository.NewRuleApplicationRepository(db)
	checkTaskRepo := repository.NewCheckTaskRepository(db)
	issueRepo := repository.NewIssueRepository(db)

	// Services
	ruleEngineSvc := service.NewRuleEngineService(standardClient, systemServiceClient, ruleAppRepo)
	checkTaskSvc := service.NewCheckTaskService(checkTaskRepo, systemServiceClient)
	checkExecutor := service.NewCheckExecutor(systemServiceClient, executionAuthorizationClient, checkTaskRepo, issueRepo, cfg.CheckTimeout, cfg.WorkerConcurrency)
	checkExecutor.StartWorker(context.Background())
	defer checkExecutor.StopWorker()
	issueSvc := service.NewIssueService(issueRepo)
	cleanupService := service.NewCleanupService(db, redisClient, commonExecution.NewTaskExecutionRepository(db))
	if err := cleanupService.Start(context.Background()); err != nil {
		log.Printf("Quality 资源回收服务启动失败: %v", err)
	}
	defer cleanupService.Stop()

	router := api.SetupRouter(
		ruleEngineSvc,
		checkTaskSvc,
		checkExecutor,
		issueSvc,
		db,
		cfg.SystemURL,
		redisClient,
	)

	addr := ":" + cfg.Port
	log.Printf("Quality service starting on %s", addr)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	systemServiceClient.RegisterAndHeartbeatWithMetadata(context.Background(), "quality", serviceURL, "/quality", map[string]interface{}{
		"module": "quality",
		"capabilities": map[string]interface{}{
			"cleanup_executor": map[string]interface{}{
				"enabled": true,
				"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
			},
		},
	})

	if cfg.SystemURL != "" {
		taskProviderRegistry := service.NewTaskProviderRegistryService(
			systemServiceClient,
			serviceURL,
		)

		go func() {
			time.Sleep(2 * time.Second)
			maxRetries := 5
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := taskProviderRegistry.Register(context.Background()); err != nil {
					log.Printf("⚠️  Quality 任务提供者注册失败 (%d/%d): %v", attempt, maxRetries, err)
					time.Sleep(time.Duration(attempt*2) * time.Second)
					continue
				}
				log.Printf("✅ Quality 模块已注册到 task_providers")
				return
			}
		}()
	}

	select {}
}
