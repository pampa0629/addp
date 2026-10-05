package api

import (
	"errors"

	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterObservabilityIdentityRoute(api *gin.RouterGroup, runtime *IAMRuntime, h *ModuleRegistryHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil || h == nil || h.service == nil {
		return errors.New("observability identity route dependencies are required")
	}
	platform, err := middleware.NewIAMServiceContextGuard("platform")
	if err != nil {
		return err
	}
	read, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemObservabilityIdentityRead)
	if err != nil {
		return err
	}
	client, err := middleware.NewIAMClientGuard("addp-monitor")
	if err != nil {
		return err
	}
	api.GET("/runtime/observability-identities", runtime.Authentication, runtime.ServiceCredential, platform, read, client, h.ObservabilityIdentities)
	return nil
}
