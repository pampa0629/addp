package api

import (
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// getTenantID 从 context 获取租户 ID
func getTenantID(c *gin.Context) int64 {
	if tenantID, exists := c.Get("tenant_id"); exists {
		if id, ok := tenantID.(int64); ok {
			return id
		}
	}
	return 1
}

// getUserID 从 context 获取用户 ID
func getUserID(c *gin.Context) int64 {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(int64); ok {
			return id
		}
	}
	return 1
}

// SetupRouter 设置路由
func SetupRouter(
	db *gorm.DB,
	domainSvc *service.DomainService,
	glossarySvc *service.GlossaryService,
	elementSvc *service.ElementService,
	codeSetSvc *service.CodeSetService,
	unitSvc *service.UnitService,
	classificationSvc *service.ClassificationService,
	metricSvc *service.MetricService,
	documentSvc *service.DocumentService,
	dimHierarchySvc *service.DimensionHierarchyService,
	systemURL string,
	redisClient *redis.Client,
) *gin.Engine {
	router := gin.Default()

	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	domainHandler := NewDomainHandler(domainSvc)
	glossaryHandler := NewGlossaryHandler(glossarySvc)
	elementHandler := NewElementHandler(elementSvc)
	codeSetHandler := NewCodeSetHandler(codeSetSvc)
	unitHandler := NewUnitHandler(unitSvc)
	classificationHandler := NewClassificationHandler(classificationSvc)
	metricHandler := NewMetricHandler(metricSvc)
	documentHandler := NewDocumentHandler(documentSvc)
	dimHierarchyHandler := NewDimensionHierarchyHandler(dimHierarchySvc)
	assetDiscHandler := newAssetDiscoverableHandler(db)

	api := router.Group("/api/standard")
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		api.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}

	{
		// 资产发现接口（供 Asset 模块调用）
		api.GET("/assets/discoverable", assetDiscHandler.listDiscoverableAssets)

		domains := api.Group("/domains")
		{
			domains.GET("", domainHandler.ListDomains)
			domains.POST("", domainHandler.CreateDomain)
			domains.GET("/:id", domainHandler.GetDomain)
			domains.PUT("/:id", domainHandler.UpdateDomain)
			domains.DELETE("/:id", domainHandler.DeleteDomain)
		}

		glossaries := api.Group("/glossaries")
		{
			glossaries.GET("", glossaryHandler.ListGlossaries)
			glossaries.POST("", glossaryHandler.CreateGlossary)
			glossaries.GET("/:id", glossaryHandler.GetGlossary)
			glossaries.PUT("/:id", glossaryHandler.UpdateGlossary)
			glossaries.DELETE("/:id", glossaryHandler.DeleteGlossary)
			glossaries.POST("/:id/approve", glossaryHandler.ApproveGlossary)
			glossaries.POST("/:id/deprecate", glossaryHandler.DeprecateGlossary)
			glossaries.GET("/:id/elements", glossaryHandler.GetElementMappings)
			glossaries.PUT("/:id/elements", glossaryHandler.SetElementMappings)
			glossaries.GET("/:id/documents", documentHandler.ListDocsByGlossary)
			glossaries.POST("/:id/documents", documentHandler.CreateAndLinkGlossary)
			glossaries.POST("/:id/documents/link", documentHandler.LinkDocToGlossary)
			glossaries.DELETE("/:id/documents/:doc_id", documentHandler.UnlinkDocFromGlossary)
		}

		elements := api.Group("/elements")
		{
			elements.GET("", elementHandler.ListElements)
			elements.POST("", elementHandler.CreateElement)
			elements.GET("/:id", elementHandler.GetElement)
			elements.PUT("/:id", elementHandler.UpdateElement)
			elements.DELETE("/:id", elementHandler.DeleteElement)
			elements.POST("/:id/approve", elementHandler.ApproveElement)
			elements.GET("/:id/quality-rules", elementHandler.GetElementQualityRules)
			elements.GET("/:id/documents", documentHandler.ListDocsByElement)
			elements.POST("/:id/documents", documentHandler.CreateAndLinkElement)
			elements.POST("/:id/documents/link", documentHandler.LinkDocToElement)
			elements.DELETE("/:id/documents/:doc_id", documentHandler.UnlinkDocFromElement)
		}

		codeSets := api.Group("/code-sets")
		{
			codeSets.GET("", codeSetHandler.ListCodeSets)
			codeSets.POST("", codeSetHandler.CreateCodeSet)
			codeSets.GET("/:id", codeSetHandler.GetCodeSet)
			codeSets.PUT("/:id", codeSetHandler.UpdateCodeSet)
			codeSets.DELETE("/:id", codeSetHandler.DeleteCodeSet)
			codeSets.GET("/:id/items", codeSetHandler.GetCodeItems)
			codeSets.POST("/:id/items", codeSetHandler.CreateCodeItem)
			codeSets.PUT("/:id/items/:iid", codeSetHandler.UpdateCodeItem)
			codeSets.DELETE("/:id/items/:iid", codeSetHandler.DeleteCodeItem)
		}

		mCats := api.Group("/measurement-categories")
		{
			mCats.GET("", unitHandler.ListCategories)
			mCats.POST("", unitHandler.CreateCategory)
			mCats.PUT("/:id", unitHandler.UpdateCategory)
			mCats.DELETE("/:id", unitHandler.DeleteCategory)
		}

		units := api.Group("/units")
		{
			units.GET("", unitHandler.ListUnits)
			units.POST("", unitHandler.CreateUnit)
			units.GET("/:id", unitHandler.GetUnit)
			units.PUT("/:id", unitHandler.UpdateUnit)
			units.DELETE("/:id", unitHandler.DeleteUnit)
		}

		classifications := api.Group("/classifications")
		{
			classifications.GET("", classificationHandler.ListClassifications)
			classifications.POST("", classificationHandler.CreateClassification)
			classifications.PUT("/:id", classificationHandler.UpdateClassification)
			classifications.DELETE("/:id", classificationHandler.DeleteClassification)
		}

		gradingLevels := api.Group("/grading-levels")
		{
			gradingLevels.GET("", classificationHandler.ListGradingLevels)
			gradingLevels.PUT("/:id", classificationHandler.UpdateGradingLevel)
		}

		metricCats := api.Group("/metric-categories")
		{
			metricCats.GET("", metricHandler.ListCategories)
			metricCats.POST("", metricHandler.CreateCategory)
			metricCats.PUT("/:id", metricHandler.UpdateCategory)
			metricCats.DELETE("/:id", metricHandler.DeleteCategory)
		}

		metrics := api.Group("/metrics")
		{
			metrics.GET("", metricHandler.ListMetrics)
			metrics.POST("", metricHandler.CreateMetric)
			metrics.GET("/:id", metricHandler.GetMetric)
			metrics.PUT("/:id", metricHandler.UpdateMetric)
			metrics.DELETE("/:id", metricHandler.DeleteMetric)
			metrics.POST("/:id/approve", metricHandler.ApproveMetric)
			metrics.POST("/:id/deprecate", metricHandler.DeprecateMetric)
			metrics.GET("/:id/documents", documentHandler.ListDocsByMetric)
			metrics.POST("/:id/documents", documentHandler.CreateAndLinkMetric)
			metrics.POST("/:id/documents/link", documentHandler.LinkDocToMetric)
			metrics.DELETE("/:id/documents/:doc_id", documentHandler.UnlinkDocFromMetric)
		}

		documents := api.Group("/documents")
		{
			documents.GET("", documentHandler.ListDocuments)
			documents.POST("", documentHandler.CreateDocument)
			documents.GET("/:id", documentHandler.GetDocument)
			documents.PUT("/:id", documentHandler.UpdateDocument)
			documents.DELETE("/:id", documentHandler.DeleteDocument)
			documents.GET("/:id/mappings", documentHandler.GetMappings)
			documents.PUT("/:id/mappings", documentHandler.SetMappings)
			documents.POST("/:id/upload", documentHandler.UploadFile)
			documents.GET("/:id/download", documentHandler.DownloadFile)
		}

		// 维度层级
		dimHierarchies := api.Group("/dimension-hierarchies")
		{
			dimHierarchies.GET("", dimHierarchyHandler.List)
			dimHierarchies.POST("", dimHierarchyHandler.Create)
			dimHierarchies.GET("/:id", dimHierarchyHandler.Get)
			dimHierarchies.PUT("/:id", dimHierarchyHandler.Update)
			dimHierarchies.DELETE("/:id", dimHierarchyHandler.Delete)
			dimHierarchies.GET("/:id/levels", dimHierarchyHandler.ListLevels)
			dimHierarchies.POST("/:id/levels", dimHierarchyHandler.CreateLevel)
			dimHierarchies.PUT("/:id/levels/:lid", dimHierarchyHandler.UpdateLevel)
			dimHierarchies.DELETE("/:id/levels/:lid", dimHierarchyHandler.DeleteLevel)
		}
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return router
}
