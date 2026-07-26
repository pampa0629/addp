package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	graphauthorization "github.com/addp/graph/internal/authorization"
	"github.com/addp/graph/internal/config"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/addp/graph/docs"
	_ "github.com/addp/graph/i18n"
)

func SetupRouter(
	cfg *config.Config,
	ontologyHandler *OntologyHandler,
	graphHandler *KnowledgeGraphHandler,
	browseHandler *BrowseHandler,
	buildHandler *BuildHandler,
	taskProviderHandler *TaskProviderHandler,
	analysisHandler *AnalysisHandler,
	serviceHandler *ServiceHandler,
) *gin.Engine {
	router := gin.Default()

	// i18n 中间件（解析 Accept-Language 请求头）
	router.Use(i18nmiddleware.I18nMiddleware())

	// 健康检查（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "service": "graph"})
	})

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 需要认证的路由
	auth := router.Group("/api/v1/graph")
	auth.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}
	{
		// 本体管理
		ontologies := auth.Group("/ontologies")
		{
			ontologies.GET("", permission(graphauthorization.PermissionGraphOntologyRead), ontologyHandler.List)
			ontologies.POST("", permission(graphauthorization.PermissionGraphOntologyCreate), ontologyHandler.Create)
			ontologies.GET("/import-preview/from-model", permission(graphauthorization.PermissionGraphOntologyRead), ontologyHandler.ImportPreviewFromModel)
			ontologies.GET("/neo4j-engines", permission(graphauthorization.PermissionGraphOntologyRead), ontologyHandler.ListNeo4jEngines)
			ontologies.GET("/infer-schema/from-engine", permission(graphauthorization.PermissionGraphOntologyRead), ontologyHandler.InferSchemaFromEngine)

			ontology := ontologies.Group("/:id")
			{
				ontology.GET("", permission(graphauthorization.PermissionGraphOntologyRead), ontologyHandler.Get)
				ontology.PUT("", permission(graphauthorization.PermissionGraphOntologyUpdate), ontologyHandler.Update)
				ontology.DELETE("", permission(graphauthorization.PermissionGraphOntologyDelete), ontologyHandler.Delete)
				ontology.POST("/import-from-model", permission(graphauthorization.PermissionGraphOntologyUpdate), ontologyHandler.ImportFromModel)
				ontology.POST("/infer-schema/from-engine/apply", permission(graphauthorization.PermissionGraphOntologyUpdate), ontologyHandler.ApplyInferredSchemaFromEngine)

				// 实体类型（:eid 为实体类型ID）
				ontology.GET("/entity-types", permission(graphauthorization.PermissionGraphOntologyRead), ontologyHandler.ListEntityTypes)
				ontology.POST("/entity-types", permission(graphauthorization.PermissionGraphOntologyCreate), ontologyHandler.CreateEntityType)
				ontology.PUT("/entity-types/:eid", permission(graphauthorization.PermissionGraphOntologyUpdate), ontologyHandler.UpdateEntityType)
				ontology.DELETE("/entity-types/:eid", permission(graphauthorization.PermissionGraphOntologyDelete), ontologyHandler.DeleteEntityType)
				ontology.POST("/entity-types/:eid/sync-constraints", permission(graphauthorization.PermissionGraphOntologyUpdate), ontologyHandler.SyncEntityTypeConstraints)
				ontology.PUT("/entity-types/:eid/sync-spatial-layer", permission(graphauthorization.PermissionGraphOntologyUpdate), ontologyHandler.SyncEntityTypeSpatialLayer)

				// 关系类型（:rid 为关系类型ID）
				ontology.GET("/relation-types", permission(graphauthorization.PermissionGraphOntologyRead), ontologyHandler.ListRelationTypes)
				ontology.POST("/relation-types", permission(graphauthorization.PermissionGraphOntologyCreate), ontologyHandler.CreateRelationType)
				ontology.PUT("/relation-types/:rid", permission(graphauthorization.PermissionGraphOntologyUpdate), ontologyHandler.UpdateRelationType)
				ontology.DELETE("/relation-types/:rid", permission(graphauthorization.PermissionGraphOntologyDelete), ontologyHandler.DeleteRelationType)

				// 版本管理
				ontology.GET("/versions", permission(graphauthorization.PermissionGraphOntologyRead), ontologyHandler.ListVersions)
				ontology.POST("/versions", permission(graphauthorization.PermissionGraphOntologyCreate), ontologyHandler.CreateVersion)
			}
		}

		// 知识图谱实例管理
		graphs := auth.Group("/graphs")
		{
			graphs.GET("", permission(graphauthorization.PermissionGraphGraphRead), graphHandler.List)
			graphs.POST("", permission(graphauthorization.PermissionGraphGraphCreate), graphHandler.Create)
			graphs.GET("/:id", permission(graphauthorization.PermissionGraphGraphRead), graphHandler.Get)
			graphs.PUT("/:id", permission(graphauthorization.PermissionGraphGraphUpdate), graphHandler.Update)
			graphs.DELETE("/:id", permission(graphauthorization.PermissionGraphGraphDelete), graphHandler.Delete)

			// 图谱浏览 API
			graph := graphs.Group("/:id")
			{
				graph.GET("/schema", permission(graphauthorization.PermissionGraphGraphRead), browseHandler.GetSchema)
				graph.GET("/stats", permission(graphauthorization.PermissionGraphGraphRead), browseHandler.GetStats)
				graph.GET("/overview", permission(graphauthorization.PermissionGraphGraphRead), browseHandler.GetOverview)
				graph.GET("/constraints", permission(graphauthorization.PermissionGraphGraphRead), browseHandler.GetConstraints)
				graph.GET("/infer-schema", permission(graphauthorization.PermissionGraphGraphRead), browseHandler.InferSchema)
				graph.POST("/infer-schema/apply", permission(graphauthorization.PermissionGraphGraphUpdate), browseHandler.ApplyInferredSchema)
				graph.POST("/search", permission(graphauthorization.PermissionGraphGraphRead), browseHandler.SearchNodes)
				graph.POST("/expand", permission(graphauthorization.PermissionGraphGraphRead), browseHandler.ExpandNode)
				graph.POST("/path", permission(graphauthorization.PermissionGraphGraphRead), browseHandler.FindPath)

				// 图谱构建 API
				build := graph.Group("/build")
				{
					tasks := build.Group("/tasks")
					{
						tasks.GET("", permission(graphauthorization.PermissionGraphBuildTaskRead), buildHandler.ListTasks)
						tasks.POST("", permission(graphauthorization.PermissionGraphBuildTaskCreate), buildHandler.CreateTask)
						task := tasks.Group("/:tid")
						{
							task.GET("", permission(graphauthorization.PermissionGraphBuildTaskRead), buildHandler.GetTask)
							task.DELETE("", permission(graphauthorization.PermissionGraphBuildTaskDelete), buildHandler.DeleteTask)
							task.POST("/run", permission(graphauthorization.PermissionGraphBuildTaskExecute), buildHandler.RunTask)
							task.POST("/cancel", permission(graphauthorization.PermissionGraphBuildTaskCancel), buildHandler.CancelTask)
							task.POST("/rerun", permission(graphauthorization.PermissionGraphBuildTaskExecute), buildHandler.RerunTask)
							task.GET("/materials", permission(graphauthorization.PermissionGraphBuildTaskRead), buildHandler.ListMaterials)
							task.POST("/materials", permission(graphauthorization.PermissionGraphBuildTaskUpdate), buildHandler.UploadMaterial)
							task.DELETE("/materials/:mid", permission(graphauthorization.PermissionGraphBuildTaskUpdate), buildHandler.DeleteMaterial)
						}
					}
				}

				// 图算法分析 API
				analysis := graph.Group("/analysis")
				{
					analysis.GET("/capabilities", permission(graphauthorization.PermissionGraphAnalysisRead), analysisHandler.GetCapabilities)
					analysis.POST("/run", permission(graphauthorization.PermissionGraphAnalysisExecute), analysisHandler.RunAlgorithm)
					analysis.POST("/sync-spatial", permission(graphauthorization.PermissionGraphAnalysisExecute), analysisHandler.SyncSpatialLayers)
				}

				// 审核队列 API
				review := graph.Group("/review")
				{
					review.GET("", permission(graphauthorization.PermissionGraphReviewRead), buildHandler.ListReviewItems)
					review.GET("/pending-count", permission(graphauthorization.PermissionGraphReviewRead), buildHandler.PendingReviewCount)
					review.POST("/batch/approve", permission(graphauthorization.PermissionGraphReviewApprove), buildHandler.BatchApproveReviewItems)
					review.POST("/batch/reject", permission(graphauthorization.PermissionGraphReviewReject), buildHandler.BatchRejectReviewItems)
					review.POST("/:iid/approve", permission(graphauthorization.PermissionGraphReviewApprove), buildHandler.ApproveReviewItem)
					review.POST("/:iid/reject", permission(graphauthorization.PermissionGraphReviewReject), buildHandler.RejectReviewItem)
					review.PUT("/:iid", permission(graphauthorization.PermissionGraphReviewUpdate), buildHandler.ModifyReviewItem)
				}
			}
		}

		// TaskProvider 标准入口
		tasks := auth.Group("/tasks")
		{
			tasks.GET("", permission(graphauthorization.PermissionGraphBuildTaskRead), taskProviderHandler.ListProviderTasks)
			tasks.GET("/:task_type/:id", permission(graphauthorization.PermissionGraphBuildTaskRead), taskProviderHandler.GetProviderTask)
			tasks.POST("/:task_type/:id/execute", permission(graphauthorization.PermissionGraphBuildTaskExecute), taskProviderHandler.ExecuteProviderTask)
		}
		auth.GET("/executions/:execution_id", permission(graphauthorization.PermissionGraphBuildTaskRead), taskProviderHandler.GetProviderExecution)
	}

	// 知识服务 API（可选 JWT，handler 内部判断 is_public）
	kg := router.Group("/api/v1/graph/kg/:graphId")
	kg.Use(optionalAuthMiddleware(cfg.SystemServiceURL))
	{
		kg.GET("/entities/:type", serviceHandler.ListEntities)
		kg.GET("/entities/:type/:nodeId", serviceHandler.GetEntity)
		kg.GET("/nodes/:nodeId/neighbors", serviceHandler.GetNeighbors)
		kg.POST("/paths", serviceHandler.FindPaths)
		kg.POST("/subgraph", serviceHandler.GetSubgraph)
		kg.GET("/search", serviceHandler.SearchEntities)
		kg.GET("/ontology", serviceHandler.GetOntology)
		kg.GET("/stats", serviceHandler.GetStats)
	}

	return router
}
