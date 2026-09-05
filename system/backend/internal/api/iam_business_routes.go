package api

import (
	"errors"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterIAMMigratedBusinessRoutes(
	api *gin.RouterGroup,
	runtime *IAMRuntime,
	engineHandler *EngineHandler,
	applicationHandler *ApplicationHandler,
	cleanupHandler *CleanupHandler,
) error {
	if api == nil || runtime == nil || runtime.Authentication == nil ||
		runtime.UserAccessCredential == nil || runtime.BusinessCredential == nil ||
		engineHandler == nil || applicationHandler == nil || cleanupHandler == nil {
		return errors.New("IAM 业务路由依赖不完整")
	}
	permission := func(keys ...string) (gin.HandlerFunc, error) {
		return middleware.NewIAMPermissionGuard(keys...)
	}

	engineListPermission, err := permission("system.engine.read")
	if err != nil {
		return err
	}
	engineDetailCredential, err := middleware.NewIAMCredentialGuard(
		middleware.IAMTokenTypeFirstPartyAccess,
		middleware.IAMTokenTypeOAuthAccess,
		middleware.IAMTokenTypeServiceAccess,
	)
	if err != nil {
		return err
	}
	engineListDelegation, err := sharedauth.NewDelegatedRouteGuard(sharedauth.DelegatedRouteGuardConfig{
		Audience:            "system",
		RequiredScopes:      []string{"engine.list"},
		RequiredPermissions: []string{"system.engine.read"},
	})
	if err != nil {
		return err
	}
	enginePermissions := make(map[string]gin.HandlerFunc)
	for _, key := range []string{
		"system.engine.create",
		"system.engine.read",
		"system.engine.update",
		"system.engine.delete",
		"system.engine.execute",
	} {
		guard, err := permission(key)
		if err != nil {
			return err
		}
		enginePermissions[key] = guard
	}
	engineTypes := api.Group("/engine-types")
	engineTypes.Use(runtime.Authentication, runtime.UserAccessCredential)
	engineTypes.GET("", enginePermissions["system.engine.read"], engineHandler.ListEngineTypes)

	engines := api.Group("/engines")
	engines.Use(runtime.Authentication)
	{
		engines.POST("", runtime.UserAccessCredential, enginePermissions["system.engine.create"], engineHandler.Create)
		engines.GET("", runtime.BusinessCredential, engineListDelegation, engineListPermission, engineHandler.List)
		engines.GET("/:id", engineDetailCredential, enginePermissions["system.engine.read"], engineHandler.GetByID)
		engines.PUT("/:id", runtime.UserAccessCredential, enginePermissions["system.engine.update"], engineHandler.Update)
		engines.POST("/:id/restore", runtime.UserAccessCredential, enginePermissions["system.engine.update"], engineHandler.Restore)
		engines.POST("/:id/deletion-assessments", runtime.UserAccessCredential, enginePermissions["system.engine.delete"], engineHandler.CreateDeletionAssessment)
		engines.GET("/:id/deletion-assessments/:assessment_id", runtime.UserAccessCredential, enginePermissions["system.engine.delete"], engineHandler.GetDeletionAssessment)
		engines.DELETE("/:id", runtime.UserAccessCredential, enginePermissions["system.engine.delete"], engineHandler.Delete)
		engines.POST("/:id/test", runtime.UserAccessCredential, enginePermissions["system.engine.execute"], engineHandler.TestConnection)
		engines.POST("/test-connection", runtime.UserAccessCredential, enginePermissions["system.engine.execute"], engineHandler.TestConnectionBeforeCreate)
		engines.POST("/:id/catalog/children", engineDetailCredential, enginePermissions["system.engine.read"], engineHandler.ListEngineCatalogChildren)
		engines.POST("/:id/catalog/facts", engineDetailCredential, enginePermissions["system.engine.read"], engineHandler.DescribeEngineCatalogFacts)
		engines.POST("/:id/spatial-workspaces/:ecosystem/:kind/enable",
			runtime.UserAccessCredential,
			enginePermissions["system.engine.execute"],
			engineHandler.EnableSpatialWorkspace,
		)
	}

	applicationPermissions := make(map[string]gin.HandlerFunc)
	for _, key := range []string{
		"system.application.create",
		"system.application.read",
		"system.application.update",
		"system.application.delete",
		"system.api_key.create",
		"system.api_key.read",
		"system.api_key.revoke",
	} {
		guard, err := permission(key)
		if err != nil {
			return err
		}
		applicationPermissions[key] = guard
	}
	applications := api.Group("/applications")
	applications.Use(runtime.Authentication, runtime.UserAccessCredential)
	{
		applications.POST("", applicationPermissions["system.application.create"], applicationHandler.CreateApplication)
		applications.GET("", applicationPermissions["system.application.read"], applicationHandler.ListApplications)
		applications.GET("/:id", applicationPermissions["system.application.read"], applicationHandler.GetApplication)
		applications.PUT("/:id", applicationPermissions["system.application.update"], applicationHandler.UpdateApplication)
		applications.DELETE("/:id", applicationPermissions["system.application.delete"], applicationHandler.DeleteApplication)
		applications.POST("/:id/keys", applicationPermissions["system.api_key.create"], applicationHandler.GenerateAPIKey)
		applications.GET("/:id/keys", applicationPermissions["system.api_key.read"], applicationHandler.ListAPIKeys)
		applications.DELETE("/:id/keys/:key_id", applicationPermissions["system.api_key.revoke"], applicationHandler.RevokeAPIKey)
	}

	cleanupRead, err := permission("system.cleanup.read")
	if err != nil {
		return err
	}
	cleanupExecute, err := permission("system.cleanup.execute")
	if err != nil {
		return err
	}
	cleanup := api.Group("/admin/cleanup")
	cleanup.Use(runtime.Authentication, runtime.UserAccessCredential)
	{
		cleanup.POST("/scan", cleanupExecute, cleanupHandler.CreateScanTask)
		cleanup.GET("/tasks/:task_id", cleanupRead, cleanupHandler.GetTaskStatus)
		cleanup.POST("/execute", cleanupExecute, cleanupHandler.CreateExecuteTask)
		cleanup.GET("/history", cleanupRead, cleanupHandler.GetTaskHistory)
	}
	return nil
}
