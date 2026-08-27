package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/model/i18n"
	"github.com/addp/model/internal/api"
	"github.com/addp/model/internal/config"
	modelmigration "github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/repository"
	"github.com/addp/model/internal/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// @title           ADDP Model API
// @version         1.0
// @host      localhost:8181
// @BasePath  /api/v1/model
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()

	// 连接数据库
	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := commonExecution.EnsureStore(db); err != nil {
		log.Fatalf("Failed to ensure execution store: %v", err)
	}
	if err := modelmigration.Run(db); err != nil {
		log.Fatalf("Failed to migrate Model database: %v", err)
	}

	// 连接 Redis
	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		defer redisClient.Close()
	}

	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemURL, "addp-model", cfg.ServiceClientSecret, nil,
	)
	if err != nil {
		log.Fatalf("Service Token Source 初始化失败: %v", err)
	}
	systemClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)
	standardClient := commonClient.NewStandardClient(cfg.StandardURL, serviceTokenSource, nil)

	// 创建 Repositories（仅 Model 相关）
	entityRepo := repository.NewEntityRepository(db)
	entityRelationRepo := repository.NewEntityRelationRepository(db)
	logicalTableRepo := repository.NewLogicalTableRepository(db)
	dwLayerRepo := repository.NewDWLayerRepository(db)
	factMetricRepo := repository.NewFactMetricRepository(db)
	tableRelationRepo := repository.NewTableRelationRepository(db)
	standardReferenceGuardRepo := repository.NewStandardReferenceGuardRepository(db)
	materializationRepo := repository.NewMaterializationBatchRepository(db)
	materializationGroupRepo := repository.NewMaterializationGroupRepository(db)
	catalogResourceRepo := repository.NewCatalogResourceRepository(db)

	// 创建 Services（仅 Model 相关，传入 standardURL 用于验证 element_id）
	entitySvc := service.NewEntityService(entityRepo, entityRelationRepo)
	entitySvc.SetStandardClient(standardClient)
	entityRelationSvc := service.NewEntityRelationService(entityRelationRepo, entityRepo)
	logicalTableSvc := service.NewLogicalTableService(logicalTableRepo, entityRepo, dwLayerRepo)
	logicalTableSvc.SetStandardClient(standardClient)
	dwLayerSvc := service.NewDWLayerService(dwLayerRepo)
	factMetricSvc := service.NewFactMetricService(factMetricRepo, logicalTableRepo)
	factMetricSvc.SetStandardClient(standardClient)
	tableRelationSvc := service.NewTableRelationService(tableRelationRepo, logicalTableRepo)
	tableRelationSvc.SetProfessionalRelationSources(entityRepo, factMetricRepo)
	standardReferenceGuardSvc := service.NewStandardReferenceGuardService(standardReferenceGuardRepo)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	materializationSvc := service.NewMaterializationService(systemClient, materializationRepo, logicalTableRepo, logicalTableSvc)
	materializationGroupSvc := service.NewMaterializationGroupService(materializationGroupRepo, materializationSvc)
	catalogResourceSvc := service.NewCatalogResourceService(catalogResourceRepo)
	materializationSvc.SetGroupService(materializationGroupSvc)
	materializationSvc.Start(runtimeContext)
	defer materializationSvc.Stop()
	cleanupSvc := service.NewCleanupService(db, redisClient, taskExecutionRepo)
	if err := cleanupSvc.Start(runtimeContext); err != nil {
		log.Printf("Model 资源回收执行方启动失败: %v", err)
	}
	defer cleanupSvc.Stop()

	// 设置路由
	lifecycleController := modulelifecycle.NewBusiness("model", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(
		entitySvc,
		entityRelationSvc,
		logicalTableSvc,
		dwLayerSvc,
		factMetricSvc,
		tableRelationSvc,
		standardReferenceGuardSvc,
		materializationSvc,
		materializationGroupSvc,
		catalogResourceSvc,
		taskExecutionRepo,
		cfg.SystemURL,
		redisClient,
		lifecycleController,
	)

	// 启动服务
	addr := ":" + cfg.Port
	log.Printf("Model service starting on %s", addr)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to bind Model listener: %v", err)
	}

	// 后台启动服务器
	go func() {
		if err := router.RunListener(listener); err != nil {
			log.Printf("Failed to start server: %v", err)
			stopRuntime()
		}
	}()

	// 启动模块注册和心跳
	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	taskProvider, err := service.ModelTaskProviderDeclaration()
	if err != nil {
		log.Fatalf("构建 Model TaskProvider 声明失败: %v", err)
	}
	registration := systemClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
		ModuleName: "model", ModuleURL: serviceURL, RoutePrefix: "/model", HealthCheckURL: serviceURL + "/health/ready",
		TaskProvider: taskProvider,
		Metadata: map[string]interface{}{
			"module": "model",
			"capabilities": map[string]interface{}{
				"cleanup_executor": map[string]interface{}{
					"enabled": true,
					"causes":  []string{events.CleanupCauseTenantDeleted},
				},
			},
		},
	})
	lifecycleController.AttachRegistration(registration)
	modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)

	// 阻塞主 goroutine
	<-runtimeContext.Done()
	<-registration.Done()
}
