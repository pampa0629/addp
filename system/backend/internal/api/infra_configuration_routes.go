package api

import (
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterInfraConfigurationRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *InfraConfigurationHandler) error {
	platformContext, err := middleware.NewIAMContextGuard("platform")
	if err != nil {
		return err
	}
	read, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemInfraConfigurationRead)
	if err != nil {
		return err
	}
	update, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemInfraConfigurationUpdate)
	if err != nil {
		return err
	}
	group := api.Group("/platform/infra_configuration")
	group.Use(runtime.Authentication, runtime.UserAccessCredential, platformContext, read)
	group.GET("", handler.Read)
	group.POST("/:component/parameters/:parameter/apply", update, handler.Apply)
	return nil
}
