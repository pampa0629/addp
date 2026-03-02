package main

import (
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/quality/internal/api"
	"github.com/addp/quality/internal/config"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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

	if err := db.AutoMigrate(
		&models.RuleApplication{},
		&models.CheckTask{},
		&models.Issue{},
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
	standardClient := commonClient.NewStandardClientWithInternalKey(cfg.StandardURL, cfg.InternalAPIKey)
	metaClient := commonClient.NewMetaClientWithInternalKey(cfg.MetaURL, cfg.InternalAPIKey)

	// Repositories
	ruleAppRepo := repository.NewRuleApplicationRepository(db)
	checkTaskRepo := repository.NewCheckTaskRepository(db)
	issueRepo := repository.NewIssueRepository(db)

	// Services
	ruleEngineSvc := service.NewRuleEngineService(standardClient, metaClient, ruleAppRepo, checkTaskRepo)
	checkTaskSvc := service.NewCheckTaskService(checkTaskRepo)
	checkExecutor := service.NewCheckExecutor(db, systemClient, ruleAppRepo, checkTaskRepo, issueRepo)
	issueSvc := service.NewIssueService(issueRepo)

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

	go func() {
		time.Sleep(2 * time.Second)

		serviceURL := fmt.Sprintf("http://localhost:%s", cfg.Port)
		registrationReq := &commonClient.ModuleRegistrationRequest{
			ModuleName:     "quality",
			ModuleURL:      serviceURL,
			RoutePrefix:    "/quality",
			HealthCheckURL: serviceURL + "/health",
			Metadata: map[string]interface{}{
				"module": "quality",
			},
		}

		maxRetries := 3
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if err := systemClient.RegisterModule(registrationReq); err != nil {
				log.Printf("⚠️  Quality 模块注册失败 (尝试 %d/%d): %v", attempt, maxRetries, err)
				time.Sleep(time.Duration(attempt*5) * time.Second)
				continue
			}
			log.Printf("✅ Quality 模块注册成功: %s", serviceURL)
			break
		}

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := systemClient.SendHeartbeat("quality"); err != nil {
				log.Printf("⚠️  Quality 心跳失败: %v", err)
			}
		}
	}()

	select {}
}
