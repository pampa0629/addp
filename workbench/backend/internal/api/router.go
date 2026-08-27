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

func SetupRouter(systemURL string, lifecycle *modulelifecycle.Controller, views *service.ViewService) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery(), requestidmiddleware.RequestIDMiddleware(), commoni18n.I18nMiddleware())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

	handler := NewHandler(views)
	api := router.Group("/api/v1/workbench")
	api.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}), commonAuth.MustNewContextGuard("tenant"))
	api.GET("/views", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewRead), handler.ListViews)
	api.POST("/views", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewCreate), handler.CreateView)
	api.GET("/views/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewRead), handler.GetView)
	api.PUT("/views/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewUpdate), handler.UpdateView)
	api.DELETE("/views/:id", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchViewDelete), handler.DeleteView)
	return router
}
