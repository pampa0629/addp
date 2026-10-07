package api

import (
	_ "github.com/addp/catalog/docs"
	_ "github.com/addp/catalog/i18n"
	catalogauthorization "github.com/addp/catalog/internal/authorization"
	"github.com/addp/catalog/internal/service"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(systemURL string, lifecycle *modulelifecycle.Controller, entries *service.EntryService, governanceTasks *service.GovernanceTaskService, personal *service.PersonalCatalogService, collections *service.CollectionService, syncRunner *service.SourceSyncRunner, domains service.DomainOverviewReader) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery(), commoni18n.I18nMiddleware())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

	handler := NewHandler(entries, governanceTasks, personal, collections, syncRunner).WithDomainOverviewReader(domains)
	api := router.Group("/api/v1/catalog")
	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	readPermission := commonAuth.MustNewPermissionGuard(catalogauthorization.PermissionCatalogEntryRead)
	inventoryReadPermission := commonAuth.MustNewPermissionGuard(
		catalogauthorization.PermissionCatalogEntryRead,
		catalogauthorization.PermissionCatalogInventoryRead,
	)
	updatePermission := commonAuth.MustNewPermissionGuard(catalogauthorization.PermissionCatalogEntryUpdate)
	mappingReviewPermission := commonAuth.MustNewPermissionGuard(catalogauthorization.PermissionCatalogStandardMappingReview)
	batchGovernancePermission := commonAuth.MustNewPermissionGuard(
		catalogauthorization.PermissionCatalogInventoryRead,
		catalogauthorization.PermissionCatalogEntryUpdate,
	)
	rebindPermission := commonAuth.MustNewPermissionGuard(catalogauthorization.PermissionCatalogSourceRebind)
	historyPermission := commonAuth.MustNewPermissionGuard(
		catalogauthorization.PermissionCatalogEntryRead,
		catalogauthorization.PermissionCatalogAuditRead,
	)
	referenceResolvePermission := commonAuth.MustNewPermissionGuard(catalogauthorization.PermissionCatalogReferenceRead)
	collectionReadPermission := commonAuth.MustNewPermissionGuard(
		catalogauthorization.PermissionCatalogEntryRead,
		catalogauthorization.PermissionCatalogCollectionRead,
	)
	collectionUpdatePermission := commonAuth.MustNewPermissionGuard(
		catalogauthorization.PermissionCatalogEntryRead,
		catalogauthorization.PermissionCatalogCollectionRead,
		catalogauthorization.PermissionCatalogCollectionUpdate,
	)
	api.GET("/entries", readPermission, handler.ListEntries)
	api.GET("/domains", readPermission, handler.ListDomainOverviews)
	api.GET("/entries/facets", readPermission, handler.ListEntryFacets)
	api.POST("/entries/resolve-sources", readPermission, handler.ResolveSourceEntries)
	api.POST("/entries/batch_governance", batchGovernancePermission, handler.BatchGovernanceEntries)
	api.GET("/reference-candidates", updatePermission, handler.ListReferenceCandidates)
	api.GET("/entries/:id", readPermission, handler.GetEntry)
	sharingPermission := commonAuth.MustNewPermissionGuard(catalogauthorization.PermissionCatalogSharingDecisionCreate)
	api.GET("/entries/:id/sharing_recipient_candidates", readPermission, sharingPermission, handler.ListSharingRecipientCandidates)
	api.POST("/entries/:id/sharing_decisions", readPermission, sharingPermission, handler.CreateSharingDecision)
	api.GET("/entries/:id/sharing_decisions", readPermission, sharingPermission, handler.ListSharingDecisions)
	api.GET("/entries/:id/sharing_decisions/:decision_id", readPermission, sharingPermission, handler.GetSharingDecision)
	api.GET("/entries/:id/sharing_decisions/:decision_id/fulfillments", readPermission, sharingPermission, handler.ListSharingDecisionFulfillments)
	api.GET("/entries/:id/sharing_decision_candidates", readPermission,
		commonAuth.MustNewPermissionGuard("system.engine_access_fulfillment.create"), handler.ListSharingDecisionCandidates)
	api.POST("/entries/:id/sharing_fulfillments", readPermission,
		commonAuth.MustNewPermissionGuard("system.engine_access_fulfillment.create"), handler.PrepareSharingFulfillment)
	api.GET("/entries/:id/sharing_fulfillments", readPermission,
		commonAuth.MustNewPermissionGuard("system.engine_access_fulfillment.create"), handler.ListSharingFulfillments)
	api.GET("/entries/:id/sharing_fulfillments/:request_id", readPermission,
		commonAuth.MustNewPermissionGuard("system.engine_access_fulfillment.create"), handler.GetSharingFulfillment)
	api.POST("/runtime/sharing-fulfillments/:request_id/basis", commonAuth.MustNewServiceClientGuard("addp-system"),
		commonAuth.MustNewPermissionGuard("catalog.sharing_fulfillment.read"), handler.ReadSharingFulfillmentBasis)
	api.GET("/entries/:id/data-dictionary", readPermission, handler.GetEntryDataDictionary)
	api.GET("/entries/:id/data-dictionary/export", readPermission, handler.ExportEntryDataDictionary)
	api.PUT("/entries/:id", updatePermission, handler.UpdateEntry)
	api.PUT("/entries/:id/governance", readPermission, updatePermission, handler.UpdateEntryGovernance)
	api.PUT("/entries/:id/responsibilities", readPermission, updatePermission, handler.TransferEntryResponsibilities)
	api.GET("/standard-mappings", readPermission, handler.ListStandardMappings)
	api.POST("/standard-mappings", updatePermission, handler.CreateStandardMapping)
	api.GET("/standard-mappings/revision-options", updatePermission, handler.ListStandardMappingRevisionOptions)
	api.GET("/standard-mappings/:id", readPermission, handler.GetStandardMapping)
	api.PUT("/standard-mappings/:id", updatePermission, handler.UpdateStandardMapping)
	api.DELETE("/standard-mappings/:id", updatePermission, handler.DeleteStandardMapping)
	api.POST("/standard-mappings/:id/approve", mappingReviewPermission, handler.ApproveStandardMapping)
	api.POST("/standard-mappings/:id/reject", mappingReviewPermission, handler.RejectStandardMapping)
	api.POST("/standard-mappings/:id/withdraw", mappingReviewPermission, handler.WithdrawStandardMapping)
	api.POST("/entries/:id/rebind-source", rebindPermission, handler.RebindSource)
	api.GET("/entries/:id/history", historyPermission, handler.GetEntryHistory)
	api.GET("/governance/tasks", readPermission, updatePermission, handler.ListGovernanceTasks)
	api.GET("/governance/coverage", inventoryReadPermission, handler.GetGovernanceCoverage)
	api.GET("/me/entries", readPermission, handler.ListMyEntries)
	api.GET("/me/entries/:id/marks", readPermission, handler.GetMyEntryMarks)
	api.PUT("/me/entries/:id/marks", readPermission, handler.ReplaceMyEntryMarks)
	api.GET("/me/project-groups", collectionReadPermission, handler.ListMyProjectGroups)
	api.GET("/collections", collectionReadPermission, handler.ListCollections)
	api.POST("/collections", collectionUpdatePermission, handler.CreateCollection)
	api.GET("/collections/:id", collectionReadPermission, handler.GetCollection)
	api.PUT("/collections/:id", collectionUpdatePermission, handler.UpdateCollection)
	api.DELETE("/collections/:id", collectionUpdatePermission, handler.DeleteCollection)
	api.POST(
		"/runtime/references/resolve",
		commonAuth.MustNewServiceClientGuard("addp-asset"),
		referenceResolvePermission,
		handler.ResolveReferences,
	)
	return router
}
