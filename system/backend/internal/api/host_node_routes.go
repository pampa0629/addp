package api

import (
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterHostNodeRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *HostNodeHandler) error {
	platform, err := middleware.NewIAMContextGuard("platform")
	if err != nil {
		return err
	}
	read, err := middleware.NewIAMPermissionGuard(authorization.PermissionPlatformHostNodeRead)
	if err != nil {
		return err
	}
	create, err := middleware.NewIAMPermissionGuard(authorization.PermissionPlatformHostNodeCreate)
	if err != nil {
		return err
	}
	update, err := middleware.NewIAMPermissionGuard(authorization.PermissionPlatformHostNodeUpdate)
	if err != nil {
		return err
	}
	nodes := api.Group("/platform/host_nodes")
	nodes.Use(runtime.Authentication, runtime.UserAccessCredential, platform)
	nodes.GET("", read, h.List)
	nodes.GET("/:node_id", read, h.Get)
	nodes.POST("", create, h.Create)
	nodes.PUT("/:node_id", update, h.Update)
	return nil
}
