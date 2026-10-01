package api

import (
	"errors"

	authorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineAccessDelegationRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessDelegationHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("engine delegation route dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	read, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessDelegationRead)
	if err != nil {
		return err
	}
	create, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessDelegationCreate)
	if err != nil {
		return err
	}
	revoke, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessDelegationRevoke)
	if err != nil {
		return err
	}
	routes := api.Group("/engines/:id/access_delegations")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.GET("", read, handler.List)
	routes.POST("", create, handler.Create)
	routes.GET("/:delegation_id", read, handler.Get)
	routes.POST("/:delegation_id/revoke", revoke, handler.Revoke)
	return nil
}
