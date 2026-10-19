package api

import (
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineDeploymentRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *EngineDeploymentHandler) error {
	platform, err := middleware.NewIAMContextGuard("platform")
	if err != nil {
		return err
	}
	read, err := middleware.NewIAMPermissionGuard(authorization.PermissionPlatformEngineDeploymentRead)
	if err != nil {
		return err
	}
	create, err := middleware.NewIAMPermissionGuard(authorization.PermissionPlatformEngineDeploymentCreate, authorization.PermissionPlatformHostNodeRead)
	if err != nil {
		return err
	}
	update, err := middleware.NewIAMPermissionGuard(authorization.PermissionPlatformEngineDeploymentUpdate, authorization.PermissionPlatformHostNodeRead)
	if err != nil {
		return err
	}
	routes := api.Group("/platform/engine_deployments")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, platform)
	routes.GET("", read, h.List)
	routes.GET("/discovery", read, h.Discovery)
	routes.GET("/:deployment_id", read, h.Get)
	routes.POST("", create, h.Create)
	routes.PUT("/:deployment_id", update, h.Update)
	return nil
}
