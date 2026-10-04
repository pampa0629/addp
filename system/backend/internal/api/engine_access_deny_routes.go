package api

import (
	"errors"

	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineAccessDenyRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessDenyHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("engine deny route dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	create, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessDenyCreate)
	if err != nil {
		return err
	}
	release, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessDenyRelease)
	if err != nil {
		return err
	}
	routes := api.Group("/engines/:id/access_denies")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.POST("", create, handler.Create)
	routes.POST("/:deny_id/release", release, handler.Release)
	return nil
}
