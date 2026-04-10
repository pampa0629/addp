package main

import (
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	"github.com/addp/quality/internal/api"
	"github.com/addp/quality/internal/config"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	_ "github.com/addp/quality/i18n"
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

	serviceURL := fmt.Sprintf("http://localhost:%s", cfg.Port)
	systemClient.RegisterAndHeartbeat("quality", serviceURL, "/quality")

	select {}
}
