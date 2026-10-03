package api

import (
	"errors"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineAccessFulfillmentRecoveryRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessFulfillmentRecoveryHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil || handler == nil || handler.service == nil {
		return errors.New("fulfillment recovery route dependencies required")
	}
	tenant, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(engineaccess.FulfillmentReconcilePermission)
	if err != nil {
		return err
	}
	routes := api.Group("/runtime/engine-access-fulfillments/:request_id")
	routes.Use(runtime.Authentication, runtime.ServiceCredential, tenant, sharedauth.MustNewServiceClientGuard("addp-catalog"), permission)
	routes.POST("/resolve", handler.Resolve)
	routes.POST("/close", handler.Close)
	return nil
}

func RegisterEngineAccessFulfillmentAcceptanceRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessFulfillmentAcceptanceHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil || handler == nil || handler.service == nil {
		return errors.New("fulfillment acceptance dependencies required")
	}
	tenant, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(engineaccess.FulfillmentReconcilePermission)
	if err != nil {
		return err
	}
	api.POST("/runtime/engine-access-fulfillments/:request_id/accept", runtime.Authentication, runtime.ServiceCredential,
		tenant, sharedauth.MustNewServiceClientGuard("addp-catalog"), permission, handler.Accept)
	return nil
}
