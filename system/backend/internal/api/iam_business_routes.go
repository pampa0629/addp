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
	apiConsumerHandler *APIConsumerHandler,
	cleanupHandler *CleanupHandler,
) error {
	if api == nil || runtime == nil || runtime.Authentication == nil ||
		runtime.UserAccessCredential == nil || runtime.BusinessCredential == nil ||
		engineHandler == nil || apiConsumerHandler == nil || cleanupHandler == nil {
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

	apiConsumerPermissions := make(map[string]gin.HandlerFunc)
	for _, key := range []string{
		"iam.api_consumer.create",
		"iam.api_consumer.read",
		"iam.api_consumer.update",
		"iam.api_consumer.delete",
		"iam.api_consumer_credential.create",
		"iam.api_consumer_credential.read",
		"iam.api_consumer_credential.revoke",
	} {
		guard, err := permission(key)
		if err != nil {
			return err
		}
		apiConsumerPermissions[key] = guard
	}
	apiConsumers := api.Group("/tenant/api-consumers")
	apiConsumers.Use(runtime.Authentication, runtime.UserAccessCredential)
	{
		apiConsumers.POST("", apiConsumerPermissions["iam.api_consumer.create"], apiConsumerHandler.Create)
		apiConsumers.GET("", apiConsumerPermissions["iam.api_consumer.read"], apiConsumerHandler.List)
		apiConsumers.GET("/:id", apiConsumerPermissions["iam.api_consumer.read"], apiConsumerHandler.Get)
		apiConsumers.PUT("/:id", apiConsumerPermissions["iam.api_consumer.update"], apiConsumerHandler.Update)
		apiConsumers.DELETE("/:id", apiConsumerPermissions["iam.api_consumer.delete"], apiConsumerHandler.Delete)
		apiConsumers.POST("/:id/credentials", apiConsumerPermissions["iam.api_consumer_credential.create"], apiConsumerHandler.CreateCredential)
		apiConsumers.GET("/:id/credentials", apiConsumerPermissions["iam.api_consumer_credential.read"], apiConsumerHandler.ListCredentials)
		apiConsumers.DELETE("/:id/credentials/:credential_id", apiConsumerPermissions["iam.api_consumer_credential.revoke"], apiConsumerHandler.RevokeCredential)
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
