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
		runtime.CatalogReferenceHandler == nil || runtime.PlatformTenantHandler == nil ||
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
	platformTenantRead, err := middleware.NewIAMPermissionGuard("platform.tenant.read")
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
	catalogReferenceResolveRead, err := middleware.NewIAMPermissionGuard("iam.department.read", "iam.project_group.read", "iam.tenant_membership.read")
	if err != nil {
		return err
	}
	catalogReferenceCandidateRead, err := middleware.NewIAMPermissionGuard("iam.department.read", "iam.tenant_membership.read")
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
		platformRoutes.DELETE("/modules/:module_name/instances/:instance_id", moduleHandler.DeregisterService)
		platformRoutes.POST("/engines", engineHandler.RegisterRuntimeEngine)
	}
	platformReadRoutes := runtimeRoutes.Group("")
	platformReadRoutes.Use(platformContext, runtimeRegistryRead)
	{
		platformReadRoutes.GET("/task-providers", taskProviderHandler.ListService)
		platformReadRoutes.GET("/task-providers/:module_name", taskProviderHandler.GetService)
		platformReadRoutes.GET("/modules", moduleHandler.ListModulesService)
		platformReadRoutes.GET("/modules/watch", moduleHandler.WatchModulesService)
		platformReadRoutes.GET("/modules/:module_name", moduleHandler.GetModuleService)
	}
	platformTenantRoutes := runtimeRoutes.Group("")
	platformTenantRoutes.Use(platformContext, platformTenantRead)
	{
		platformTenantRoutes.GET("/tenants", runtime.PlatformTenantHandler.ListRuntime)
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
	runtimeRoutes.POST("/catalog-references/resolve", tenantContext, catalogReferenceResolveRead, runtime.CatalogReferenceHandler.Resolve)
	runtimeRoutes.GET("/catalog-references/candidates", tenantContext, catalogReferenceCandidateRead, runtime.CatalogReferenceHandler.ListCandidates)

	api.POST("/tenant/audit/events", runtime.Authentication, runtime.ServiceCredential, tenantContext, tenantAuditCreate, runtime.InternalAuditHandler.CreateService)
	return nil
}
