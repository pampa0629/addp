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
	"fmt"
	"os"
	"time"

	commonExecution "github.com/addp/common/execution"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/engine/contentadapter"
	commonPlugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/minio"
	commonLogger "github.com/addp/common/logger"
	"github.com/addp/common/utils"
	"github.com/addp/graph/internal/api"
	"github.com/addp/graph/internal/config"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
)

func main() {
	commonConfig.LoadEnv()

	cfg := config.Load()
	commonConfig.InitLogger("graph-backend.log", &commonConfig.LoggerOptions{
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		AddSource: &cfg.LogAddSource,
		File:      cfg.LogFile,
	})
	logger := commonLogger.L()

	// 端口检查
	if err := utils.CheckPortAvailable(cfg.Port); err != nil {
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
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	neo4jSvc := service.NewNeo4jService(graphRepo, ontologyRepo, systemClient)
	knowledgeSvc := service.NewKnowledgeService(neo4jSvc, ontologyRepo, graphRepo)
	schemaInferenceSvc := service.NewSchemaInferenceService(graphRepo, ontologyRepo, neo4jSvc, ontologySvc, systemClient)
	buildSvc := service.NewBuildService(buildRepo, ontologyRepo, ontologySvc, graphRepo, taskExecutionRepo, neo4jSvc, materialReader, materialWriter, cfg.CopilotServiceURL)
	analysisSvc := service.NewAnalysisService(graphRepo, ontologyRepo, systemClient)

	// 初始化 Model 导入服务（如果配置了 MODEL_URL）
	var modelImportSvc *service.ModelImportService
	if cfg.ModelServiceURL != "" && cfg.InternalAPIKey != "" {
		modelClient := commonClient.NewModelClientWithInternalKey(cfg.ModelServiceURL, cfg.InternalAPIKey)
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
	router := api.SetupRouter(cfg, ontologyHandler, graphHandler, browseHandler, buildHandler, taskProviderHandler, analysisHandler, serviceHandler)

	// 模块注册
	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("graph")
	serviceURL := utils.BuildServiceURL(serviceHost, port)
	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		registryClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
		registryClient.RegisterAndHeartbeat("graph", serviceURL, "/graph")
	}

	if cfg.EnableIntegration && cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		taskProviderRegistry := service.NewTaskProviderRegistryService(
			cfg.SystemServiceURL,
			cfg.InternalAPIKey,
			serviceURL,
		)

		go func() {
			time.Sleep(2 * time.Second)
			maxRetries := 5
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := taskProviderRegistry.Register(); err != nil {
					logger.Warn("任务提供者注册失败",
						"attempt", fmt.Sprintf("%d/%d", attempt, maxRetries),
						"error", err)
					time.Sleep(time.Duration(attempt*2) * time.Second)
					continue
				}
				logger.Info("✅ Graph 模块已注册到 task_providers")
				return
			}
		}()
	}

	addr := ":" + cfg.Port
	logger.Info("Graph 模块启动", "addr", addr, "schema", cfg.DBSchema)

	if err := router.Run(addr); err != nil {
		logger.Error("Graph 模块启动失败", "error", err)
		os.Exit(1)
	}
}
