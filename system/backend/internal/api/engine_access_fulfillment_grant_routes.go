package api

import (
	"errors"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineAccessFulfillmentGrantRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessFulfillmentGrantHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil || handler == nil || handler.service == nil {
		return errors.New("fulfillment grant route dependencies required")
	}
	tenant, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(engineaccess.FulfillmentReconcilePermission)
	if err != nil {
		return err
	}
	routes := api.Group("/runtime/engine-access-fulfillments/:request_id/grant")
	routes.Use(runtime.Authentication, runtime.ServiceCredential, tenant, sharedauth.MustNewServiceClientGuard("addp-catalog"), permission)
	routes.POST("", handler.Issue)
	routes.POST("/resolve", handler.Resolve)
	return nil
}
