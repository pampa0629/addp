package api

import (
	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterIAMEmergencySessionRoutes(api *gin.RouterGroup, r *IAMRuntime) error {
	if api == nil || r == nil || r.EmergencySessionHandler == nil || r.Authentication == nil || r.FirstPartyCredential == nil {
		return commonapi.ErrBadRequest
	}
	h := r.EmergencySessionHandler
	platformGuard, err := middleware.NewIAMContextGuard("platform")
	if err != nil {
		return err
	}
	tenantGuard, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	guards, err := permissionGuards(func(key string) (gin.HandlerFunc, error) { return middleware.NewIAMPermissionGuard(key) }, []string{
		"iam.emergency_session_revocation.read", "iam.emergency_session_revocation.create", "iam.emergency_session_revocation.approve", "iam.emergency_session_revocation.reject", "iam.emergency_session_revocation.revoke", "iam.emergency_session_revocation.cancel", "iam.emergency_session_revocation.execute"})
	if err != nil {
		return err
	}
	platform := api.Group("/platform/emergency_session_requests")
	platform.Use(r.Authentication, r.FirstPartyCredential, platformGuard)
	platform.GET("", guards["iam.emergency_session_revocation.read"], h.List)
	platform.GET("/:id", guards["iam.emergency_session_revocation.read"], h.Get)
	platform.POST("/:id/approve", guards["iam.emergency_session_revocation.approve"], h.Approve)
	platform.POST("/:id/reject", guards["iam.emergency_session_revocation.reject"], h.Reject)
	platform.POST("/:id/revoke", guards["iam.emergency_session_revocation.revoke"], h.Revoke)
	tenant := api.Group("/tenant/emergency_session_requests")
	tenant.Use(r.Authentication, r.FirstPartyCredential, tenantGuard)
	tenant.GET("", guards["iam.emergency_session_revocation.read"], h.List)
	tenant.GET("/:id", guards["iam.emergency_session_revocation.read"], h.Get)
	tenant.POST("", guards["iam.emergency_session_revocation.create"], h.Create)
	tenant.POST("/:id/cancel", guards["iam.emergency_session_revocation.cancel"], h.Cancel)
	tenant.POST("/:id/execute", guards["iam.emergency_session_revocation.execute"], h.Execute)
	return nil
}
