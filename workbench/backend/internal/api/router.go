package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/workbench/docs"
	_ "github.com/addp/workbench/i18n"
	workbenchauthorization "github.com/addp/workbench/internal/authorization"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(systemURL string, lifecycle *modulelifecycle.Controller, views *service.ViewService, applications *service.DataApplicationService, catalogResources *service.CatalogResourceService, resourceGrants *service.ResourceGrantService) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery(), requestidmiddleware.RequestIDMiddleware(), commoni18n.I18nMiddleware())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

	handler := NewHandler(views, applications)
	catalogResourceHandler := NewCatalogResourceHandler(catalogResources)
	resourceGrantHandler := NewResourceGrantHandler(resourceGrants)
	api := router.Group("/api/v1/workbench")
	api.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}), commonAuth.MustNewContextGuard("tenant"))
	catalogAPI := api.Group("")
	catalogAPI.Use(commonAuth.MustNewServiceClientGuard("addp-catalog"))
	catalogAPI.GET("/catalog-resources/changes", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchCatalogRead), catalogResourceHandler.ListChanges)
	catalogAPI.POST("/runtime/catalog-references/resolve", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchCatalogRead), catalogResourceHandler.ResolveReferences)
	assetAPI := api.Group("/runtime/resource-grants")
	assetAPI.Use(commonAuth.MustNewServiceClientGuard("addp-asset"))
	assetAPI.PUT("/:source_identity", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchResourceGrantCreate), resourceGrantHandler.FulfillAssetGrant)
	assetAPI.DELETE("/:source_identity", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchResourceGrantRevoke), resourceGrantHandler.RevokeAssetGrant)
	api.GET("/views", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewRead), handler.ListViews)
	api.POST("/views", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewCreate), handler.CreateView)
	api.GET("/views/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewRead), handler.GetView)
	api.PUT("/views/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewUpdate), handler.UpdateView)
	api.DELETE("/views/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewDelete), handler.DeleteView)
	api.GET("/data_applications", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationRead), handler.ListDataApplications)
	api.POST("/data_applications", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationCreate), handler.CreateDataApplication)
	api.GET("/data_applications/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationRead), handler.GetDataApplication)
	api.PUT("/data_applications/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationUpdate), handler.UpdateDataApplication)
	api.DELETE("/data_applications/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationDelete), handler.DeleteDataApplication)
	api.POST("/data_applications/:id/publish", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationPublish), handler.PublishDataApplication)
	api.POST("/data_applications/:id/offline", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationPublish), handler.OfflineDataApplication)
	api.GET("/data_applications/:id/runtime", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchDataApplicationExecute), handler.GetDataApplicationRuntime)
	return router
}
