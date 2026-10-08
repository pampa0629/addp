package api

import (
	"errors"

	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineAccessGrantRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessGrantHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("engine grant route dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	revoke, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessGrantRevoke)
	if err != nil {
		return err
	}
	create, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessGrantCreate)
	if err != nil {
		return err
	}
	read, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessGrantRead)
	if err != nil {
		return err
	}
	routes := api.Group("/engines/:id/access_grants")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.POST("", create, handler.Create)
	routes.GET("", read, handler.List)
	routes.GET("/history", read, handler.History)
	routes.POST("/:request_id/revoke", revoke, handler.Revoke)
	return nil
}
