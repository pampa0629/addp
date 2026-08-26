package main

// @title           ADDP Graph API
// @version         1.0
// @description     全域数据平台 - 知识图谱模块 API | All Domain Data Platform - Knowledge Graph Module API

// @host      localhost:8186
// @BasePath  /api/v1/graph

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	commonExecution "github.com/addp/common/execution"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/engine/contentadapter"
	commonPlugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/minio"
	"github.com/addp/common/events"
	commonLogger "github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/graph/internal/api"
	"github.com/addp/graph/internal/config"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"github.com/redis/go-redis/v9"
)

func main() {
	commonConfig.LoadEnv()

	cfg := config.Load()
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	commonConfig.InitLogger("graph-backend.log", &commonConfig.LoggerOptions{
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		AddSource: &cfg.LogAddSource,
		File:      cfg.LogFile,
	})
	logger := commonLogger.L()

	// 端口检查
	if err := commonConfig.CheckPortAvailable(cfg.Port); err != nil {
		logger.Error("端口检查失败", "error", err, "port", cfg.Port)
		os.Exit(1)
	}

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	// 初始化 repositories
	ontologyRepo := repository.NewOntologyRepository(db)
	entityTypeRepo := repository.NewEntityTypeRepository(db)
	relationTypeRepo := repository.NewRelationTypeRepository(db)
	versionRepo := repository.NewOntologyVersionRepository(db)
	graphRepo := repository.NewKnowledgeGraphRepository(db)
	buildRepo := repository.NewBuildRepository(db)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer redisClient.Close()

	materialConnInfo := commonPlugin.ConnectionInfo{
		"endpoint":   cfg.MinioEndpoint,
		"access_key": cfg.MinioAccessKey,
		"secret_key": cfg.MinioSecretKey,
	}
	materialPlugin := &minio.MinIOPlugin{}
	materialPathMapper := contentadapter.ObjectPathMapper(0)
	materialReader := contentadapter.NewMappedReader(materialPlugin, materialConnInfo, materialPathMapper, commonPlugin.ReadOptions{})
	materialWriter := contentadapter.NewMappedWriter(materialPlugin, materialConnInfo, materialPathMapper, commonPlugin.WriteOptions{Overwrite: true})

	// 初始化 services
	ontologySvc := service.NewOntologyService(ontologyRepo, entityTypeRepo, relationTypeRepo, versionRepo)
	graphSvc := service.NewKnowledgeGraphService(graphRepo)
	graphTokenSource, err := commonClient.NewOAuthServiceTokenSource(
		cfg.SystemServiceURL,
		"addp-graph",
		cfg.ServiceClientSecret,
		nil,
	)
	if err != nil {
		logger.Error("Graph Service Token Provider 初始化失败", "error", err)
		os.Exit(1)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, graphTokenSource, nil)
	neo4jSvc := service.NewNeo4jService(graphRepo, ontologyRepo, systemServiceClient)
	knowledgeSvc := service.NewKnowledgeService(neo4jSvc, ontologyRepo, graphRepo)
	schemaInferenceSvc := service.NewSchemaInferenceService(graphRepo, ontologyRepo, neo4jSvc, ontologySvc, systemServiceClient)
	buildSvc := service.NewBuildService(buildRepo, ontologyRepo, ontologySvc, graphRepo, taskExecutionRepo, neo4jSvc, materialReader, materialWriter, cfg.CopilotServiceURL, graphTokenSource)
	analysisSvc := service.NewAnalysisService(graphRepo, ontologyRepo, systemServiceClient, neo4jSvc)
	cleanupSvc := service.NewCleanupService(db, redisClient, taskExecutionRepo)
	if err := cleanupSvc.Start(runtimeContext); err != nil {
		logger.Warn("Graph 资源回收执行方启动失败", "error", err)
	}
	defer cleanupSvc.Stop()

	// 初始化 Model 导入服务（如果配置了 MODEL_URL）
	var modelImportSvc *service.ModelImportService
	if cfg.ModelServiceURL != "" {
		modelClient := commonClient.NewModelClient(cfg.ModelServiceURL, graphTokenSource, nil)
		modelImportSvc = service.NewModelImportService(modelClient, ontologySvc, entityTypeRepo, relationTypeRepo)
	}

	// 初始化 handlers
	ontologyHandler := api.NewOntologyHandler(ontologySvc, neo4jSvc, modelImportSvc, schemaInferenceSvc)
	graphHandler := api.NewKnowledgeGraphHandler(graphSvc)
	browseHandler := api.NewBrowseHandler(neo4jSvc, schemaInferenceSvc)
	buildHandler := api.NewBuildHandler(buildSvc)
	taskProviderHandler := api.NewTaskProviderHandler(buildSvc, taskExecutionRepo)
	analysisHandler := api.NewAnalysisHandler(analysisSvc)
	serviceHandler := api.NewServiceHandler(knowledgeSvc)

	// 设置路由
	lifecycleController := modulelifecycle.NewBusiness("graph", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(cfg, ontologyHandler, graphHandler, browseHandler, buildHandler, taskProviderHandler, analysisHandler, serviceHandler, lifecycleController)
	addr := ":" + cfg.Port
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("Graph 监听绑定失败", "error", err, "addr", addr)
		return
	}

	// 模块注册
	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	var registration *commonClient.ModuleRegistrationLifecycle
	if cfg.SystemServiceURL != "" {
		provider, err := service.GraphTaskProviderDeclaration()
		if err != nil {
			logger.Error("构建 Graph TaskProvider 声明失败", "error", err)
			os.Exit(1)
		}
		registration = systemServiceClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
			ModuleName: "graph", ModuleURL: serviceURL, RoutePrefix: "/graph", HealthCheckURL: serviceURL + "/health/ready",
			TaskProvider: provider,
			Metadata: map[string]interface{}{
				"module": "graph",
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
	}

	logger.Info("Graph 模块启动", "addr", addr, "schema", cfg.DBSchema)

	go func() {
		if err := router.RunListener(listener); err != nil {
			logger.Error("Graph 模块启动失败", "error", err)
			stopRuntime()
		}
	}()
	<-runtimeContext.Done()
	if registration != nil {
		<-registration.Done()
	}
}
