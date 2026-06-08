package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
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
	auth.Use(commonAuth.SystemAuthMiddleware(cfg.SystemServiceURL))
	{
		// 本体管理
		ontologies := auth.Group("/ontologies")
		{
			ontologies.GET("", ontologyHandler.List)
			ontologies.POST("", ontologyHandler.Create)
			ontologies.GET("/import-preview/from-model", ontologyHandler.ImportPreviewFromModel)
			ontologies.GET("/neo4j-engines", ontologyHandler.ListNeo4jEngines)
			ontologies.GET("/infer-schema/from-engine", ontologyHandler.InferSchemaFromEngine)

			ontology := ontologies.Group("/:id")
			{
				ontology.GET("", ontologyHandler.Get)
				ontology.PUT("", ontologyHandler.Update)
				ontology.DELETE("", ontologyHandler.Delete)
				ontology.POST("/import-from-model", ontologyHandler.ImportFromModel)
				ontology.POST("/infer-schema/from-engine/apply", ontologyHandler.ApplyInferredSchemaFromEngine)

				// 实体类型（:eid 为实体类型ID）
				ontology.GET("/entity-types", ontologyHandler.ListEntityTypes)
				ontology.POST("/entity-types", ontologyHandler.CreateEntityType)
				ontology.PUT("/entity-types/:eid", ontologyHandler.UpdateEntityType)
				ontology.DELETE("/entity-types/:eid", ontologyHandler.DeleteEntityType)
				ontology.POST("/entity-types/:eid/sync-constraints", ontologyHandler.SyncEntityTypeConstraints)
				ontology.PUT("/entity-types/:eid/sync-spatial-layer", ontologyHandler.SyncEntityTypeSpatialLayer)

				// 关系类型（:rid 为关系类型ID）
				ontology.GET("/relation-types", ontologyHandler.ListRelationTypes)
				ontology.POST("/relation-types", ontologyHandler.CreateRelationType)
				ontology.PUT("/relation-types/:rid", ontologyHandler.UpdateRelationType)
				ontology.DELETE("/relation-types/:rid", ontologyHandler.DeleteRelationType)

				// 版本管理
				ontology.GET("/versions", ontologyHandler.ListVersions)
				ontology.POST("/versions", ontologyHandler.CreateVersion)
			}
		}

		// 知识图谱实例管理
		graphs := auth.Group("/graphs")
		{
			graphs.GET("", graphHandler.List)
			graphs.POST("", graphHandler.Create)
			graphs.GET("/:id", graphHandler.Get)
			graphs.PUT("/:id", graphHandler.Update)
			graphs.DELETE("/:id", graphHandler.Delete)

			// 图谱浏览 API
			graph := graphs.Group("/:id")
			{
				graph.GET("/schema", browseHandler.GetSchema)
				graph.GET("/stats", browseHandler.GetStats)
				graph.GET("/overview", browseHandler.GetOverview)
				graph.GET("/constraints", browseHandler.GetConstraints)
				graph.GET("/infer-schema", browseHandler.InferSchema)
				graph.POST("/infer-schema/apply", browseHandler.ApplyInferredSchema)
				graph.POST("/search", browseHandler.SearchNodes)
				graph.POST("/expand", browseHandler.ExpandNode)
				graph.POST("/path", browseHandler.FindPath)

				// 图谱构建 API
				build := graph.Group("/build")
				{
					tasks := build.Group("/tasks")
					{
						tasks.GET("", buildHandler.ListTasks)
						tasks.POST("", buildHandler.CreateTask)
						task := tasks.Group("/:tid")
						{
							task.GET("", buildHandler.GetTask)
							task.DELETE("", buildHandler.DeleteTask)
							task.POST("/run", buildHandler.RunTask)
							task.POST("/cancel", buildHandler.CancelTask)
							task.POST("/rerun", buildHandler.RerunTask)
							task.GET("/materials", buildHandler.ListMaterials)
							task.POST("/materials", buildHandler.UploadMaterial)
							task.DELETE("/materials/:mid", buildHandler.DeleteMaterial)
						}
					}
				}

				// 图算法分析 API
				analysis := graph.Group("/analysis")
				{
					analysis.GET("/capabilities", analysisHandler.GetCapabilities)
					analysis.POST("/run", analysisHandler.RunAlgorithm)
					analysis.POST("/sync-spatial", analysisHandler.SyncSpatialLayers)
				}

				// 审核队列 API
				review := graph.Group("/review")
				{
					review.GET("", buildHandler.ListReviewItems)
					review.GET("/pending-count", buildHandler.PendingReviewCount)
					review.POST("/batch", buildHandler.BatchReview)
					review.POST("/:iid/approve", buildHandler.ApproveReviewItem)
					review.POST("/:iid/reject", buildHandler.RejectReviewItem)
					review.PUT("/:iid", buildHandler.ModifyReviewItem)
				}
			}
		}

		// TaskProvider 标准入口
		tasks := auth.Group("/tasks")
		{
			tasks.GET("", taskProviderHandler.ListProviderTasks)
			tasks.GET("/:task_type/:id", taskProviderHandler.GetProviderTask)
			tasks.POST("/:task_type/:id/execute", taskProviderHandler.ExecuteProviderTask)
		}
		auth.GET("/executions/:execution_id", taskProviderHandler.GetProviderExecution)
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
