package api

import (
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/standard/docs"
	_ "github.com/addp/standard/i18n"
	standardauthorization "github.com/addp/standard/internal/authorization"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

// SetupRouter 设置路由
func SetupRouter(
	db *gorm.DB,
	domainSvc *service.DomainService,
	glossarySvc *service.GlossaryService,
	elementSvc *service.ElementService,
	codeSetSvc *service.CodeSetService,
	unitSvc *service.UnitService,
	metricSvc *service.MetricService,
	documentSvc *service.DocumentService,
	collectionSvc *service.StandardCollectionService,
	referenceResolutionSvc *service.ReferenceResolutionService,
	elementRevisionResolutionSvc *service.ElementRevisionResolutionService,
	catalogResourceSvc *service.CatalogResourceService,
	systemURL string,
	lifecycle *modulelifecycle.Controller,
) *gin.Engine {
	router := gin.Default()

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

	router.Use(commoni18n.I18nMiddleware())
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
	metricHandler := NewMetricHandler(metricSvc)
	documentHandler := NewDocumentHandler(documentSvc)
	collectionHandler := NewStandardCollectionHandler(collectionSvc)
	referenceResolutionHandler := NewReferenceResolutionHandler(referenceResolutionSvc)
	elementRevisionResolutionHandler := NewElementRevisionResolutionHandler(elementRevisionResolutionSvc)
	catalogResourceHandler := NewCatalogResourceHandler(catalogResourceSvc)

	api := router.Group("/api/v1/standard")
	api.Use(
		commonAuth.MustNewOptionalResourceTicketMiddleware(commonAuth.ResourceTicketMiddlewareConfig{
			SystemURL: systemURL, Owner: "standard",
			RequiredPermissions: []string{standardauthorization.PermissionStandardDocumentRead},
		}, isStandardBrowserResourceRequest),
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}
	{
		catalogResources := api.Group("")
		catalogResources.Use(commonAuth.MustNewServiceClientGuard("addp-catalog"))
		catalogResources.GET("/catalog-resources/changes", permission(standardauthorization.PermissionStandardCatalogRead), catalogResourceHandler.ListChanges)
		catalogResources.POST("/runtime/catalog-references/resolve", permission(standardauthorization.PermissionStandardCatalogRead), catalogResourceHandler.ResolveReferences)

		api.POST(
			"/references/resolve",
			commonAuth.MustNewServiceClientGuard("addp-catalog"),
			permission(
				standardauthorization.PermissionStandardDomainRead,
				standardauthorization.PermissionStandardGlossaryRead,
				standardauthorization.PermissionStandardElementRead,
			),
			referenceResolutionHandler.Resolve,
		)
		api.GET(
			"/references/candidates",
			commonAuth.MustNewServiceClientGuard("addp-catalog"),
			permission(
				standardauthorization.PermissionStandardDomainRead,
				standardauthorization.PermissionStandardGlossaryRead,
				standardauthorization.PermissionStandardElementRead,
			),
			referenceResolutionHandler.ListCandidates,
		)
		api.POST(
			"/runtime/element-revisions/resolve",
			commonAuth.MustNewServiceClientGuard("addp-catalog", "addp-model"),
			permission(standardauthorization.PermissionStandardElementRead),
			elementRevisionResolutionHandler.Resolve,
		)

		domains := api.Group("/domains")
		{
			domains.GET("", permission(standardauthorization.PermissionStandardDomainRead), domainHandler.ListDomains)
			domains.POST("", permission(standardauthorization.PermissionStandardDomainCreate), domainHandler.CreateDomain)
			domains.GET("/:id", permission(standardauthorization.PermissionStandardDomainRead), domainHandler.GetDomain)
			domains.PUT("/:id", permission(standardauthorization.PermissionStandardDomainUpdate), domainHandler.UpdateDomain)
			domains.DELETE("/:id", permission(standardauthorization.PermissionStandardDomainDelete), domainHandler.DeleteDomain)
		}

		collections := api.Group("/collections")
		{
			collections.GET("", permission(standardauthorization.PermissionStandardCollectionRead), collectionHandler.List)
			collections.POST("", permission(standardauthorization.PermissionStandardCollectionCreate), collectionHandler.Create)
			collections.GET("/:id", permission(standardauthorization.PermissionStandardCollectionRead), collectionHandler.Get)
			collections.DELETE("/:id", permission(standardauthorization.PermissionStandardCollectionDelete), collectionHandler.Delete)
			collections.GET("/:id/revisions", permission(standardauthorization.PermissionStandardCollectionRead), collectionHandler.ListRevisions)
			collections.GET("/:id/events", permission(standardauthorization.PermissionStandardCollectionRead), collectionHandler.ListEvents)
			collections.POST("/:id/revisions", permission(standardauthorization.PermissionStandardCollectionUpdate), collectionHandler.CreateRevision)
			collections.PUT("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardCollectionUpdate), collectionHandler.UpdateRevision)
			collections.POST("/:id/revisions/:revision_id/submit", permission(standardauthorization.PermissionStandardCollectionUpdate), collectionHandler.Submit)
			collections.POST("/:id/revisions/:revision_id/return", permission(standardauthorization.PermissionStandardCollectionPublish), collectionHandler.Return)
			collections.POST("/:id/revisions/:revision_id/publish", permission(standardauthorization.PermissionStandardCollectionPublish), collectionHandler.Publish)
			collections.PUT("/:id/assignments", permission(standardauthorization.PermissionStandardCollectionAssignmentUpdate), collectionHandler.ReplaceAssignments)
		}
		api.GET("/collection-user-candidates", permission(standardauthorization.PermissionStandardCollectionAssignmentUpdate), collectionHandler.ListUserCandidates)

		glossaries := api.Group("/glossaries")
		{
			glossaries.GET("", permission(standardauthorization.PermissionStandardGlossaryRead), glossaryHandler.ListGlossaries)
			glossaries.POST("", permission(standardauthorization.PermissionStandardGlossaryCreate), glossaryHandler.CreateGlossary)
			glossaries.GET("/:id", permission(standardauthorization.PermissionStandardGlossaryRead), glossaryHandler.GetGlossary)
			glossaries.PUT("/:id", permission(standardauthorization.PermissionStandardGlossaryUpdate), glossaryHandler.UpdateGlossary)
			glossaries.DELETE("/:id", permission(standardauthorization.PermissionStandardGlossaryDelete), glossaryHandler.DeleteGlossary)
			glossaries.GET("/:id/revisions", permission(standardauthorization.PermissionStandardGlossaryRead), glossaryHandler.ListGlossaryRevisions)
			glossaries.POST("/:id/revisions", permission(standardauthorization.PermissionStandardGlossaryUpdate), glossaryHandler.CreateGlossaryRevision)
			glossaries.GET("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardGlossaryRead), glossaryHandler.GetGlossaryRevision)
			glossaries.PUT("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardGlossaryUpdate), glossaryHandler.UpdateGlossaryRevision)
			glossaries.POST("/:id/revisions/:revision_id/submit", permission(standardauthorization.PermissionStandardGlossaryUpdate), glossaryHandler.SubmitGlossaryRevision)
			glossaries.POST("/:id/revisions/:revision_id/return", permission(standardauthorization.PermissionStandardGlossaryPublish), glossaryHandler.ReturnGlossaryRevision)
			glossaries.POST("/:id/revisions/:revision_id/publish", permission(standardauthorization.PermissionStandardGlossaryPublish), glossaryHandler.PublishGlossaryRevision)
			glossaries.POST("/:id/revisions/:revision_id/withdraw", permission(standardauthorization.PermissionStandardGlossaryPublish), glossaryHandler.WithdrawGlossaryRevision)
			glossaries.GET("/:id/elements", permission(standardauthorization.PermissionStandardGlossaryRead, standardauthorization.PermissionStandardElementRead), glossaryHandler.GetElementMappings)
			glossaries.PUT("/:id/elements", permission(standardauthorization.PermissionStandardGlossaryUpdate, standardauthorization.PermissionStandardElementRead), glossaryHandler.UpdateElementMappings)
			glossaries.GET("/:id/documents", permission(standardauthorization.PermissionStandardGlossaryRead, standardauthorization.PermissionStandardDocumentRead), documentHandler.ListDocsByGlossary)
			glossaries.POST("/:id/documents", permission(standardauthorization.PermissionStandardGlossaryUpdate, standardauthorization.PermissionStandardDocumentCreate), documentHandler.CreateAndLinkGlossary)
			glossaries.POST("/:id/documents/link", permission(standardauthorization.PermissionStandardGlossaryUpdate, standardauthorization.PermissionStandardDocumentUpdate), documentHandler.LinkDocToGlossary)
			glossaries.DELETE("/:id/documents/:doc_id", permission(standardauthorization.PermissionStandardGlossaryUpdate, standardauthorization.PermissionStandardDocumentUpdate), documentHandler.UnlinkDocFromGlossary)
		}

		elements := api.Group("/elements")
		{
			elements.GET("", permission(standardauthorization.PermissionStandardElementRead), elementHandler.ListElements)
			elements.POST("", permission(standardauthorization.PermissionStandardElementCreate), elementHandler.CreateElement)
			elements.GET("/:id", permission(standardauthorization.PermissionStandardElementRead), elementHandler.GetElement)
			elements.PUT("/:id", permission(standardauthorization.PermissionStandardElementUpdate), elementHandler.UpdateElement)
			elements.DELETE("/:id", permission(standardauthorization.PermissionStandardElementDelete), elementHandler.DeleteElement)
			elements.GET("/:id/revisions", permission(standardauthorization.PermissionStandardElementRead), elementHandler.ListElementRevisions)
			elements.POST("/:id/revisions", permission(standardauthorization.PermissionStandardElementUpdate), elementHandler.CreateElementRevision)
			elements.GET("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardElementRead), elementHandler.GetElementRevision)
			elements.PUT("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardElementUpdate), elementHandler.UpdateElementRevision)
			elements.POST("/:id/revisions/:revision_id/submit", permission(standardauthorization.PermissionStandardElementUpdate), elementHandler.SubmitElementRevision)
			elements.POST("/:id/revisions/:revision_id/return", permission(standardauthorization.PermissionStandardElementPublish), elementHandler.ReturnElementRevision)
			elements.POST("/:id/revisions/:revision_id/publish", permission(standardauthorization.PermissionStandardElementPublish), elementHandler.PublishElementRevision)
			elements.POST("/:id/revisions/:revision_id/withdraw", permission(standardauthorization.PermissionStandardElementPublish), elementHandler.WithdrawElementRevision)
			elements.GET("/:id/quality-rules", permission(standardauthorization.PermissionStandardElementRead), elementHandler.GetElementQualityRules)
			elements.GET("/:id/documents", permission(standardauthorization.PermissionStandardElementRead, standardauthorization.PermissionStandardDocumentRead), documentHandler.ListDocsByElement)
			elements.POST("/:id/documents", permission(standardauthorization.PermissionStandardElementUpdate, standardauthorization.PermissionStandardDocumentCreate), documentHandler.CreateAndLinkElement)
			elements.POST("/:id/documents/link", permission(standardauthorization.PermissionStandardElementUpdate, standardauthorization.PermissionStandardDocumentUpdate), documentHandler.LinkDocToElement)
			elements.DELETE("/:id/documents/:doc_id", permission(standardauthorization.PermissionStandardElementUpdate, standardauthorization.PermissionStandardDocumentUpdate), documentHandler.UnlinkDocFromElement)
		}

		codeSets := api.Group("/code-sets")
		{
			codeSets.GET("", permission(standardauthorization.PermissionStandardCodeSetRead), codeSetHandler.ListCodeSets)
			codeSets.POST("", permission(standardauthorization.PermissionStandardCodeSetCreate), codeSetHandler.CreateCodeSet)
			codeSets.GET("/:id", permission(standardauthorization.PermissionStandardCodeSetRead), codeSetHandler.GetCodeSet)
			codeSets.PUT("/:id", permission(standardauthorization.PermissionStandardCodeSetUpdate), codeSetHandler.UpdateCodeSet)
			codeSets.DELETE("/:id", permission(standardauthorization.PermissionStandardCodeSetDelete), codeSetHandler.DeleteCodeSet)
			codeSets.GET("/:id/revisions", permission(standardauthorization.PermissionStandardCodeSetRead), codeSetHandler.ListCodeSetRevisions)
			codeSets.POST("/:id/revisions", permission(standardauthorization.PermissionStandardCodeSetUpdate), codeSetHandler.CreateCodeSetRevision)
			codeSets.GET("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardCodeSetRead), codeSetHandler.GetCodeSetRevision)
			codeSets.PUT("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardCodeSetUpdate), codeSetHandler.UpdateCodeSetRevision)
			codeSets.POST("/:id/revisions/:revision_id/submit", permission(standardauthorization.PermissionStandardCodeSetUpdate), codeSetHandler.SubmitCodeSetRevision)
			codeSets.POST("/:id/revisions/:revision_id/return", permission(standardauthorization.PermissionStandardCodeSetPublish), codeSetHandler.ReturnCodeSetRevision)
			codeSets.POST("/:id/revisions/:revision_id/publish", permission(standardauthorization.PermissionStandardCodeSetPublish), codeSetHandler.PublishCodeSetRevision)
			codeSets.POST("/:id/revisions/:revision_id/withdraw", permission(standardauthorization.PermissionStandardCodeSetPublish), codeSetHandler.WithdrawCodeSetRevision)
			codeSets.POST("/:id/revisions/:revision_id/items", permission(standardauthorization.PermissionStandardCodeSetUpdate), codeSetHandler.CreateCodeItem)
			codeSets.PUT("/:id/revisions/:revision_id/items/:item_id", permission(standardauthorization.PermissionStandardCodeSetUpdate), codeSetHandler.UpdateCodeItem)
			codeSets.DELETE("/:id/revisions/:revision_id/items/:item_id", permission(standardauthorization.PermissionStandardCodeSetUpdate), codeSetHandler.DeleteCodeItem)
		}

		mCats := api.Group("/measurement-categories")
		{
			mCats.GET("", permission(standardauthorization.PermissionStandardUnitRead), unitHandler.ListCategories)
			mCats.POST("", permission(standardauthorization.PermissionStandardUnitCreate), unitHandler.CreateCategory)
			mCats.PUT("/:id", permission(standardauthorization.PermissionStandardUnitUpdate), unitHandler.UpdateCategory)
			mCats.DELETE("/:id", permission(standardauthorization.PermissionStandardUnitDelete), unitHandler.DeleteCategory)
		}

		units := api.Group("/units")
		{
			units.GET("", permission(standardauthorization.PermissionStandardUnitRead), unitHandler.ListUnits)
			units.POST("", permission(standardauthorization.PermissionStandardUnitCreate), unitHandler.CreateUnit)
			units.GET("/:id", permission(standardauthorization.PermissionStandardUnitRead), unitHandler.GetUnit)
			units.PUT("/:id", permission(standardauthorization.PermissionStandardUnitUpdate), unitHandler.UpdateUnit)
			units.DELETE("/:id", permission(standardauthorization.PermissionStandardUnitDelete), unitHandler.DeleteUnit)
		}

		metricCats := api.Group("/metric-categories")
		{
			metricCats.GET("", permission(standardauthorization.PermissionStandardMetricRead), metricHandler.ListCategories)
			metricCats.POST("", permission(standardauthorization.PermissionStandardMetricCreate), metricHandler.CreateCategory)
			metricCats.PUT("/:id", permission(standardauthorization.PermissionStandardMetricUpdate), metricHandler.UpdateCategory)
			metricCats.DELETE("/:id", permission(standardauthorization.PermissionStandardMetricDelete), metricHandler.DeleteCategory)
		}

		metrics := api.Group("/metrics")
		{
			metrics.GET("", permission(standardauthorization.PermissionStandardMetricRead), metricHandler.ListMetrics)
			metrics.POST("", permission(standardauthorization.PermissionStandardMetricCreate), metricHandler.CreateMetric)
			metrics.GET("/:id", permission(standardauthorization.PermissionStandardMetricRead), metricHandler.GetMetric)
			metrics.GET("/:id/relations", permission(standardauthorization.PermissionStandardMetricRead), metricHandler.GetProfessionalRelations)
			metrics.PUT("/:id", permission(standardauthorization.PermissionStandardMetricUpdate), metricHandler.UpdateMetric)
			metrics.DELETE("/:id", permission(standardauthorization.PermissionStandardMetricDelete), metricHandler.DeleteMetric)
			metrics.GET("/:id/revisions", permission(standardauthorization.PermissionStandardMetricRead), metricHandler.ListMetricRevisions)
			metrics.POST("/:id/revisions", permission(standardauthorization.PermissionStandardMetricUpdate), metricHandler.CreateMetricRevision)
			metrics.GET("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardMetricRead), metricHandler.GetMetricRevision)
			metrics.PUT("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardMetricUpdate), metricHandler.UpdateMetricRevision)
			metrics.POST("/:id/revisions/:revision_id/submit", permission(standardauthorization.PermissionStandardMetricUpdate), metricHandler.SubmitMetricRevision)
			metrics.POST("/:id/revisions/:revision_id/return", permission(standardauthorization.PermissionStandardMetricPublish), metricHandler.ReturnMetricRevision)
			metrics.POST("/:id/revisions/:revision_id/publish", permission(standardauthorization.PermissionStandardMetricPublish), metricHandler.PublishMetricRevision)
			metrics.POST("/:id/revisions/:revision_id/withdraw", permission(standardauthorization.PermissionStandardMetricPublish), metricHandler.WithdrawMetricRevision)
			metrics.GET("/:id/revisions/:revision_id/published-reference", permission(standardauthorization.PermissionStandardMetricRead), metricHandler.GetPublishedReference)
			metrics.GET("/:id/documents", permission(standardauthorization.PermissionStandardMetricRead, standardauthorization.PermissionStandardDocumentRead), documentHandler.ListDocsByMetric)
			metrics.POST("/:id/documents", permission(standardauthorization.PermissionStandardMetricUpdate, standardauthorization.PermissionStandardDocumentCreate), documentHandler.CreateAndLinkMetric)
			metrics.POST("/:id/documents/link", permission(standardauthorization.PermissionStandardMetricUpdate, standardauthorization.PermissionStandardDocumentUpdate), documentHandler.LinkDocToMetric)
			metrics.DELETE("/:id/documents/:doc_id", permission(standardauthorization.PermissionStandardMetricUpdate, standardauthorization.PermissionStandardDocumentUpdate), documentHandler.UnlinkDocFromMetric)
		}

		documents := api.Group("/documents")
		{
			documents.GET("", permission(standardauthorization.PermissionStandardDocumentRead), documentHandler.ListDocuments)
			documents.POST("", permission(standardauthorization.PermissionStandardDocumentCreate), documentHandler.CreateDocument)
			documents.GET("/:id", permission(standardauthorization.PermissionStandardDocumentRead), documentHandler.GetDocument)
			documents.PUT("/:id", permission(standardauthorization.PermissionStandardDocumentUpdate), documentHandler.UpdateDocument)
			documents.DELETE("/:id", permission(standardauthorization.PermissionStandardDocumentDelete), documentHandler.DeleteDocument)
			documents.GET("/:id/revisions", permission(standardauthorization.PermissionStandardDocumentRead), documentHandler.ListRevisions)
			documents.POST("/:id/revisions", permission(standardauthorization.PermissionStandardDocumentUpdate), documentHandler.CreateRevision)
			documents.GET("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardDocumentRead), documentHandler.GetRevision)
			documents.PUT("/:id/revisions/:revision_id", permission(standardauthorization.PermissionStandardDocumentUpdate), documentHandler.UpdateRevision)
			documents.POST("/:id/revisions/:revision_id/submit", permission(standardauthorization.PermissionStandardDocumentUpdate), documentHandler.SubmitRevision)
			documents.POST("/:id/revisions/:revision_id/return", permission(standardauthorization.PermissionStandardDocumentPublish), documentHandler.ReturnRevision)
			documents.POST("/:id/revisions/:revision_id/publish", permission(standardauthorization.PermissionStandardDocumentPublish), documentHandler.PublishRevision)
			documents.POST("/:id/revisions/:revision_id/withdraw", permission(standardauthorization.PermissionStandardDocumentPublish), documentHandler.WithdrawRevision)
			documents.POST("/:id/revisions/:revision_id/file", permission(standardauthorization.PermissionStandardDocumentUpdate), documentHandler.UploadFile)
			documents.GET("/:id/revisions/:revision_id/file", permission(standardauthorization.PermissionStandardDocumentRead), documentHandler.DownloadFile)
			documents.POST("/:id/revisions/:revision_id/extractions", permission(standardauthorization.PermissionStandardDocumentExtractionCreate), documentHandler.ExtractCandidates)
			documents.GET("/:id/extraction-candidate-groups", permission(standardauthorization.PermissionStandardDocumentRead), documentHandler.ListCandidateGroups)
			documents.GET("/:id/mappings", permission(standardauthorization.PermissionStandardDocumentRead), documentHandler.GetMappings)
			documents.PUT("/:id/mappings", permission(standardauthorization.PermissionStandardDocumentUpdate), documentHandler.SetMappings)
		}
		api.PUT("/document-extraction-candidates/:candidate_id", permission(standardauthorization.PermissionStandardDocumentUpdate), documentHandler.UpdateCandidate)
		api.POST("/document-extraction-candidates/:candidate_id/formalization", permission(standardauthorization.PermissionStandardDocumentUpdate), documentHandler.FormalizeCandidate)

	}

	return router
}

func isStandardBrowserResourceRequest(c *gin.Context) bool {
	path := strings.TrimPrefix(c.Request.URL.Path, "/api/v1/standard")
	segments := strings.Split(strings.Trim(path, "/"), "/")
	return len(segments) == 5 && segments[0] == "documents" && segments[2] == "revisions" && segments[4] == "file"
}
