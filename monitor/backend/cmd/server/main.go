package main

import (
	"context"
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	commonconfiguration "github.com/addp/common/configuration"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	_ "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/api"
	monitorauthorization "github.com/addp/monitor/internal/authorization"
	"github.com/addp/monitor/internal/config"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// serviceTaskProviderLister adapts the System service client to the monitor
// health and alert services without relying on a separate file in single-file
// build invocations used by the development scripts.
type serviceTaskProviderLister struct {
	client *commonClient.SystemServiceClient
}

func (l serviceTaskProviderLister) ListTaskProviders() ([]*commonModels.TaskProvider, error) {
	return l.client.ListTaskProviders(context.Background())
}

// @title           ADDP Monitor API
// @version         1.0
// @host      localhost:8100
// @BasePath  /api/v1/monitor
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	commonConfig.LoadEnv()

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
	runtimePolicyService := service.NewRuntimePolicyService(repository.NewRuntimePolicyRepository(db))
	if err := runtimePolicyService.Apply(context.Background(), cfg); err != nil {
		log.Fatalf("Failed to load monitor runtime policy: %v", err)
	}
	smtpRelayService := service.NewSMTPRelayService(repository.NewSMTPRelayRepository(db), cfg.EncryptionKey)
	if err := smtpRelayService.Apply(context.Background(), cfg); err != nil {
		log.Fatalf("Failed to load monitor SMTP relay: %v", err)
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

	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemURL, "addp-monitor", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)
	systemTaskProviderLister := serviceTaskProviderLister{client: systemServiceClient}

	// 创建 Repository
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)

	// 创建 Services
	queryService := service.NewExecutionQueryService(taskExecutionRepo)
	statisticsService := service.NewStatisticsService(taskExecutionRepo)
	healthService := service.NewHealthCheckService(systemTaskProviderLister, serviceTokenSource)
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
	alertRuleService := service.NewAlertRuleService(db, alertService, systemTaskProviderLister)
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
		runtimePolicyService,
		smtpRelayService,
		cfg.SystemURL,
		redisClient,
		systemServiceClient,
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
		log.Printf("Email dispatcher disabled: SMTP relay is not configured")
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
	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.ServerPort)
	systemServiceClient.RegisterAndHeartbeat(context.Background(), &commonClient.ModuleRegistrationRequest{
		ModuleName: "monitor", ModuleURL: serviceURL, RoutePrefix: "/monitor", HealthCheckURL: serviceURL + "/health",
		Metadata: map[string]interface{}{"module": "monitor"},
		ConfigurationManagement: &commonconfiguration.ManagementDeclaration{SchemaVersion: commonconfiguration.ManagementSchemaVersion, Entries: []commonconfiguration.ManagementEntry{{
			ID: "monitor.configuration", OwnerModule: "monitor", ScopeTypes: []string{commonconfiguration.ScopePlatformOnly}, FrontendRoute: "/configuration/monitor",
			ReadPermission: monitorauthorization.PermissionMonitorConfigurationRead, UpdatePermission: monitorauthorization.PermissionMonitorConfigurationUpdate,
		}}},
	})

	// 阻塞主 goroutine
	select {}
}
