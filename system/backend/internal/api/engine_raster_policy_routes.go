package api

import (
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterEngineRasterPolicyRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *EngineRasterPolicyHandler) error {
	read, err := middleware.NewIAMPermissionGuard("system.engine_raster_policy.read")
	if err != nil {
		return err
	}
	update, err := middleware.NewIAMPermissionGuard("system.engine_raster_policy.update")
	if err != nil {
		return err
	}
	platformContext, err := middleware.NewIAMContextGuard("platform")
	if err != nil {
		return err
	}
	tenantContext, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	platform := api.Group("/platform/engine-raster-policies")
	platform.Use(runtime.Authentication, runtime.UserAccessCredential, platformContext)
	platform.GET("/engines", read, h.ListPlatform)
	platform.GET("/:id", read, h.GetPlatform)
	platform.PUT("/:id", update, h.PutPlatform)
	tenant := api.Group("/tenant/engine-raster-policies")
	tenant.Use(runtime.Authentication, runtime.UserAccessCredential, tenantContext)
	tenant.GET("/engines", read, h.ListTenant)
	tenant.GET("/:id", read, h.GetTenant)
	tenant.PUT("/:id", update, h.PutTenant)
	serviceContext, err := middleware.NewIAMServiceContextGuard("platform")
	if err != nil {
		return err
	}
	consume, err := middleware.NewIAMPermissionGuard("system.engine_raster_policy_runtime.read")
	if err != nil {
		return err
	}
	api.POST("/runtime/engine-raster-policy", runtime.Authentication, runtime.ServiceCredential, serviceContext, consume, sharedauth.MustNewServiceClientGuard("addp-geopython"), h.Resolve)
	return nil
}
