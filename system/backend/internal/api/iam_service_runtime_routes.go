package api

import (
	"errors"

	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterIAMServiceRuntimeRoutes registers the Bearer-only System surface for
// first-party Service Principals. Tenant and platform contexts are explicit.
func RegisterIAMServiceRuntimeRoutes(
	api *gin.RouterGroup,
	runtime *IAMRuntime,
	moduleHandler *ModuleRegistryHandler,
	taskProviderHandler *TaskProviderHandler,
	engineHandler *EngineHandler,
) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil ||
		runtime.InternalAuditHandler == nil || runtime.ExecutionAuthorizationHandler == nil ||
		runtime.TaskAuthorizationSubjectHandler == nil || moduleHandler == nil ||
		taskProviderHandler == nil || engineHandler == nil {
		return errors.New("IAM Service Runtime 路由依赖不完整")
	}
	platformContext, err := middleware.NewIAMServiceContextGuard("platform")
	if err != nil {
		return err
	}
	tenantContext, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	runtimeRegistryUpdate, err := middleware.NewIAMPermissionGuard("system.runtime_registry.update")
	if err != nil {
		return err
	}
	runtimeRegistryRead, err := middleware.NewIAMPermissionGuard("system.runtime_registry.read")
	if err != nil {
		return err
	}
	tenantAuditCreate, err := middleware.NewIAMPermissionGuard("audit.tenant_event.create")
	if err != nil {
		return err
	}
	engineDescriptorRead, err := middleware.NewIAMPermissionGuard("system.engine_descriptor.read")
	if err != nil {
		return err
	}
	executionAuthorizationIssue, err := middleware.NewIAMPermissionGuard("system.execution_authorization.execute")
	if err != nil {
		return err
	}
	taskAuthorizationResolve, err := middleware.NewIAMPermissionGuard("system.task_authorization.execute")
	if err != nil {
		return err
	}

	runtimeRoutes := api.Group("/runtime")
	runtimeRoutes.Use(runtime.Authentication, runtime.ServiceCredential)
	platformRoutes := runtimeRoutes.Group("")
	platformRoutes.Use(platformContext, runtimeRegistryUpdate)
	{
		platformRoutes.POST("/modules", moduleHandler.RegisterService)
		platformRoutes.POST("/modules/heartbeat", moduleHandler.HeartbeatService)
		platformRoutes.POST("/task-providers", taskProviderHandler.RegisterOrUpdateService)
		platformRoutes.POST("/engines", engineHandler.RegisterRuntimeEngine)
	}
	platformReadRoutes := runtimeRoutes.Group("")
	platformReadRoutes.Use(platformContext, runtimeRegistryRead)
	{
		platformReadRoutes.GET("/task-providers", taskProviderHandler.ListService)
		platformReadRoutes.GET("/task-providers/:module_name", taskProviderHandler.GetService)
		platformReadRoutes.GET("/modules", moduleHandler.ListModulesService)
		platformReadRoutes.GET("/modules/:module_name", moduleHandler.GetModuleService)
	}
	tenantEngineRoutes := runtimeRoutes.Group("/engine-descriptors")
	tenantEngineRoutes.Use(tenantContext, engineDescriptorRead)
	{
		tenantEngineRoutes.GET("", engineHandler.ListRuntimeDescriptors)
		tenantEngineRoutes.GET("/:id", engineHandler.GetRuntimeDescriptor)
	}
	runtimeRoutes.POST("/execution-authorizations", tenantContext, executionAuthorizationIssue, runtime.ExecutionAuthorizationHandler.IssueFromExecution)
	runtimeRoutes.POST("/execution-authorizations/service-definitions", tenantContext, executionAuthorizationIssue, runtime.ExecutionAuthorizationHandler.IssueFromServiceDefinition)
	runtimeRoutes.POST("/task-authorization-subjects/:id/resolve", tenantContext, taskAuthorizationResolve, runtime.TaskAuthorizationSubjectHandler.Resolve)

	api.POST("/tenant/audit/events", runtime.Authentication, runtime.ServiceCredential, tenantContext, tenantAuditCreate, runtime.InternalAuditHandler.CreateService)
	return nil
}
