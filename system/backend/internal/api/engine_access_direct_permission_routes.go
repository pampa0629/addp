package api

import (
	"errors"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineAccessDirectPermissionRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessDirectPermissionHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("direct permission route dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	read, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessDirectPermissionRead)
	if err != nil {
		return err
	}
	manage, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessDirectPermissionUpdate)
	if err != nil {
		return err
	}
	routes := api.Group("/engines/:id")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.GET("/direct-permissions", read, handler.List)
	routes.GET("/data-items/:fingerprint/direct-permission", read, handler.Get)
	routes.PUT("/data-items/:fingerprint/direct-permission", manage, handler.Set)
	return nil
}
