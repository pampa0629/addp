package api

import (
	"errors"

	authorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineAccessApprovalRequirementRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessApprovalRequirementHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("engine approval requirement route dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	read, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessApprovalRequirementRead)
	if err != nil {
		return err
	}
	initialize, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessApprovalRequirementInitialize)
	if err != nil {
		return err
	}
	routes := api.Group("/engines/:id/access_approval_requirements")
	handling, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessFulfillmentCreate)
	if err != nil {
		return err
	}
	api.POST("/engines/:id/access_handling_requirement", runtime.Authentication, runtime.UserAccessCredential, tenant, handling, handler.HandlingRequirement)
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.GET("", read, handler.List)
	routes.GET("/:requirement_id", read, handler.Get)
	routes.POST("", initialize, handler.Initialize)
	return nil
}
