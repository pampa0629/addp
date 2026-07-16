package main

import (
	"context"
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/utils"
	_ "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/api"
	"github.com/addp/monitor/internal/config"
	"github.com/addp/monitor/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title           ADDP Monitor API
// @version         1.0
// @host      localhost:8100
// @BasePath  /api/v1/monitor
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 连接数据库
	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 确保统一执行记录表存在
	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}
	if err := service.EnsureMonitorStore(db); err != nil {
		log.Fatalf("Failed to ensure monitor store: %v", err)
	}

	// 连接 Redis
	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	// 创建 System 客户端（用于健康检查）
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemURL, cfg.InternalAPIKey)

	// 创建 Repository
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)

	// 创建 Services
	queryService := service.NewExecutionQueryService(taskExecutionRepo)
	statisticsService := service.NewStatisticsService(taskExecutionRepo)
	healthService := service.NewHealthCheckService(systemClient, cfg.InternalAPIKey)
	webhookSender := service.NewHTTPWebhookSender(cfg.WebhookHTTPTimeout, cfg.WebhookAllowPrivate)
	webhookService := service.NewWebhookService(db, cfg.EncryptionKey, cfg.WebhookAllowPrivate, cfg.ConsoleBaseURL, webhookSender)
	var emailSender service.EmailSender
	if cfg.EmailSMTPConfigured() {
		emailSender, err = service.NewSMTPEmailSender(service.SMTPEmailSenderConfig{
			Host: cfg.EmailSMTPHost, Port: cfg.EmailSMTPPort, Username: cfg.EmailSMTPUsername,
			Password: cfg.EmailSMTPPassword, TLSMode: cfg.EmailSMTPTLSMode,
			FromAddress: cfg.EmailFromAddress, FromName: cfg.EmailFromName, Timeout: cfg.EmailSMTPTimeout,
		})
		if err != nil {
			log.Fatalf("Failed to configure email sender: %v", err)
		}
	}
	emailService := service.NewEmailService(db, cfg.ConsoleBaseURL, emailSender)
	notificationService := service.NewNotificationService(webhookService, emailService)
	alertService := service.NewAlertService(db, notificationService)
	alertRuleService := service.NewAlertRuleService(db, alertService, systemClient)
	webhookDispatcher := service.NewWebhookDispatcher(
		db,
		webhookSender,
		service.WebhookDispatcherConfig{
			WorkerID: "monitor-webhook-" + uuid.NewString(), DispatchInterval: cfg.WebhookDispatchInterval,
			LeaseDuration: cfg.WebhookLeaseDuration, MaxAttempts: cfg.WebhookMaxAttempts,
			RetryInitial: cfg.WebhookRetryInitial, RetryMax: cfg.WebhookRetryMax, EncryptionKey: cfg.EncryptionKey,
		},
	)

	// 设置路由
	router := api.SetupRouter(
		queryService,
		statisticsService,
		healthService,
		alertService,
		alertRuleService,
		webhookService,
		emailService,
		cfg.SystemURL,
		redisClient,
		systemClient,
	)

	go func() {
		ticker := time.NewTicker(cfg.AlertEvaluationInterval)
		defer ticker.Stop()
		for {
			if err := alertService.Evaluate(context.Background(), time.Now()); err != nil {
				log.Printf("Alert evaluation failed: %v", err)
			}
			<-ticker.C
		}
	}()
	go webhookDispatcher.Run(context.Background())
	if emailSender != nil {
		emailDispatcher := service.NewEmailDispatcher(
			db,
			emailSender,
			service.EmailDispatcherConfig{
				WorkerID: "monitor-email-" + uuid.NewString(), DispatchInterval: cfg.EmailDispatchInterval,
				LeaseDuration: cfg.EmailLeaseDuration, MaxAttempts: cfg.EmailMaxAttempts,
				RetryInitial: cfg.EmailRetryInitial, RetryMax: cfg.EmailRetryMax,
			},
		)
		go emailDispatcher.Run(context.Background())
	} else {
		log.Printf("Email dispatcher disabled: MONITOR_EMAIL_SMTP_HOST is not configured")
	}

	// 启动服务
	addr := ":" + cfg.ServerPort
	log.Printf("Monitor service starting on %s", addr)

	// 后台启动服务器
	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 启动模块注册和心跳
	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("monitor")
	serviceURL := utils.BuildServiceURL(serviceHost, port)
	systemClient.RegisterAndHeartbeat("monitor", serviceURL, "/monitor")

	// 阻塞主 goroutine
	select {}
}
