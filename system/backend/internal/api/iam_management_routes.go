package api

import (
	"errors"

	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterIAMManagementRoutes(api *gin.RouterGroup, runtime *IAMRuntime, moduleHandler *ModuleRegistryHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.FirstPartyCredential == nil ||
		runtime.PlatformTenantHandler == nil || runtime.PlatformUserHandler == nil ||
		runtime.TenantMembershipHandler == nil || runtime.AuditHandler == nil ||
		runtime.TenantInvitationHandler == nil ||
		runtime.TenantRoleHandler == nil ||
		runtime.PrivilegedIdentityChangeHandler == nil || runtime.SecurityPolicyHandler == nil || moduleHandler == nil {
		return errors.New("IAM 管理路由依赖不完整")
	}
	platformContext, err := middleware.NewIAMContextGuard("platform")
	if err != nil {
		return err
	}
	tenantContext, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	permission := func(key string) (gin.HandlerFunc, error) {
		return middleware.NewIAMPermissionGuard(key)
	}

	platformTenantPermissions, err := permissionGuards(permission, []string{
		"platform.tenant.close", "platform.tenant.create", "platform.tenant.read",
		"platform.tenant.initialize", "platform.tenant.restore", "platform.tenant.suspend", "platform.tenant.update",
	})
	if err != nil {
		return err
	}
	platformUserPermissions, err := permissionGuards(permission, []string{
		"iam.local_account.reset", "iam.mfa_credential.reset", "iam.user.create", "iam.user.read",
		"iam.user.reactivate", "iam.user.suspend", "iam.user.update",
	})
	if err != nil {
		return err
	}
	auditEventRead, err := permission("audit.event.read")
	if err != nil {
		return err
	}
	auditEventExport, err := permission("audit.event.export")
	if err != nil {
		return err
	}
	identityChangePermissions, err := permissionGuards(permission, []string{
		"iam.platform_identity_change.approve", "iam.platform_identity_change.create",
		"iam.platform_identity_change.read", "iam.platform_identity_change.reject",
	})
	if err != nil {
		return err
	}
	securityPolicyRead, err := permission("iam.security_policy.read")
	if err != nil {
		return err
	}
	securityPolicyUpdate, err := permission("iam.security_policy.update")
	if err != nil {
		return err
	}
	platformModuleRead, err := permission("platform.module.read")
	if err != nil {
		return err
	}
	platformModuleUpdate, err := permission("platform.module.update")
	if err != nil {
		return err
	}

	platform := api.Group("/platform")
	platform.Use(runtime.Authentication, runtime.FirstPartyCredential, platformContext)
	{
		platform.GET("/security_policy", securityPolicyRead, runtime.SecurityPolicyHandler.Get)
		platform.PUT("/security_policy", securityPolicyUpdate, runtime.SecurityPolicyHandler.Update)
		modules := platform.Group("/modules")
		{
			modules.GET("", platformModuleRead, moduleHandler.ListModulesPlatform)
			modules.GET("/:module_name", platformModuleRead, moduleHandler.GetModulePlatform)
			modules.PUT("/:module_name", platformModuleUpdate, moduleHandler.UpdateModulePlatform)
		}
		platform.GET("/tenant_administrator_candidates", platformTenantPermissions["platform.tenant.create"], runtime.PlatformTenantHandler.ListAdministratorCandidates)
		tenants := platform.Group("/tenants")
		{
			tenants.GET("", platformTenantPermissions["platform.tenant.read"], runtime.PlatformTenantHandler.List)
			tenants.POST("", platformTenantPermissions["platform.tenant.create"], runtime.PlatformTenantHandler.Create)
			tenants.POST("/:id/initialization", platformTenantPermissions["platform.tenant.initialize"], runtime.PlatformTenantHandler.Initialize)
			tenants.GET("/:id", platformTenantPermissions["platform.tenant.read"], runtime.PlatformTenantHandler.Get)
			tenants.PUT("/:id", platformTenantPermissions["platform.tenant.update"], runtime.PlatformTenantHandler.Update)
			tenants.POST("/:id/suspend", platformTenantPermissions["platform.tenant.suspend"], runtime.PlatformTenantHandler.Suspend)
			tenants.POST("/:id/restore", platformTenantPermissions["platform.tenant.restore"], runtime.PlatformTenantHandler.Restore)
			tenants.POST("/:id/close", platformTenantPermissions["platform.tenant.close"], runtime.PlatformTenantHandler.Close)
		}
		users := platform.Group("/users")
		{
			users.GET("", platformUserPermissions["iam.user.read"], runtime.PlatformUserHandler.List)
			users.POST("", platformUserPermissions["iam.user.create"], runtime.PlatformUserHandler.Create)
			users.GET("/:id", platformUserPermissions["iam.user.read"], runtime.PlatformUserHandler.Get)
			users.PUT("/:id", platformUserPermissions["iam.user.update"], runtime.PlatformUserHandler.Update)
			users.POST("/:id/reset-password", platformUserPermissions["iam.local_account.reset"], runtime.PlatformUserHandler.ResetLocalAccountPassword)
			users.POST("/:id/reset-mfa", platformUserPermissions["iam.mfa_credential.reset"], runtime.PlatformUserHandler.ResetMFACredential)
			users.POST("/:id/suspend", platformUserPermissions["iam.user.suspend"], runtime.PlatformUserHandler.Suspend)
			users.POST("/:id/reactivate", platformUserPermissions["iam.user.reactivate"], runtime.PlatformUserHandler.Reactivate)
		}
		identityChanges := platform.Group("/identity_changes")
		{
			identityChanges.GET("", identityChangePermissions["iam.platform_identity_change.read"], runtime.PrivilegedIdentityChangeHandler.List)
			identityChanges.POST("", identityChangePermissions["iam.platform_identity_change.create"], runtime.PrivilegedIdentityChangeHandler.Create)
			identityChanges.GET("/:id", identityChangePermissions["iam.platform_identity_change.read"], runtime.PrivilegedIdentityChangeHandler.Get)
			identityChanges.POST("/:id/approve", identityChangePermissions["iam.platform_identity_change.approve"], runtime.PrivilegedIdentityChangeHandler.Approve)
			identityChanges.POST("/:id/reject", identityChangePermissions["iam.platform_identity_change.reject"], runtime.PrivilegedIdentityChangeHandler.Reject)
		}
		auditEvents := platform.Group("/audit/events")
		{
			auditEvents.GET("", auditEventRead, runtime.AuditHandler.PlatformList)
			auditEvents.GET("/summary", auditEventRead, runtime.AuditHandler.PlatformSummary)
			auditEvents.GET("/trends", auditEventRead, runtime.AuditHandler.PlatformTrends)
			auditEvents.GET("/export", auditEventExport, runtime.AuditHandler.PlatformExport)
			auditEvents.GET("/:id", auditEventRead, runtime.AuditHandler.PlatformGet)
		}
	}

	membershipPermissions, err := permissionGuards(permission, []string{
		"iam.tenant_membership.close", "iam.tenant_membership.read", "iam.tenant_membership.restore",
		"iam.tenant_membership.suspend", "iam.tenant_membership.update",
	})
	if err != nil {
		return err
	}
	invitationPermissions, err := permissionGuards(permission, []string{
		"iam.tenant_invitation.create", "iam.tenant_invitation.read", "iam.tenant_invitation.revoke",
	})
	if err != nil {
		return err
	}
	tenantRolePermissions, err := permissionGuards(permission, []string{
		"iam.tenant_role.create", "iam.tenant_role.delete", "iam.tenant_role.read", "iam.tenant_role.update",
		"iam.tenant_role_assignment.create", "iam.tenant_role_assignment.read", "iam.tenant_role_assignment.revoke",
	})
	if err != nil {
		return err
	}
	tenantAuditRead, err := permission("audit.tenant_event.read")
	if err != nil {
		return err
	}
	tenantAuditExport, err := permission("audit.tenant_event.export")
	if err != nil {
		return err
	}
	tenant := api.Group("/tenant")
	tenant.Use(runtime.Authentication, runtime.FirstPartyCredential, tenantContext)
	{
		tenant.GET("/role_permissions", tenantRolePermissions["iam.tenant_role.read"], runtime.TenantRoleHandler.ListAssignablePermissions)
		roles := tenant.Group("/roles")
		{
			roles.GET("", tenantRolePermissions["iam.tenant_role.read"], runtime.TenantRoleHandler.ListRoles)
			roles.POST("", tenantRolePermissions["iam.tenant_role.create"], runtime.TenantRoleHandler.CreateRole)
			roles.PUT("/:id", tenantRolePermissions["iam.tenant_role.update"], runtime.TenantRoleHandler.UpdateRole)
			roles.DELETE("/:id", tenantRolePermissions["iam.tenant_role.delete"], runtime.TenantRoleHandler.DeleteRole)
		}
		roleAssignments := tenant.Group("/role_assignments")
		{
			roleAssignments.GET("", tenantRolePermissions["iam.tenant_role_assignment.read"], runtime.TenantRoleHandler.ListAssignments)
			roleAssignments.POST("", tenantRolePermissions["iam.tenant_role_assignment.create"], runtime.TenantRoleHandler.CreateAssignment)
			roleAssignments.POST("/:id/revoke", tenantRolePermissions["iam.tenant_role_assignment.revoke"], runtime.TenantRoleHandler.RevokeAssignment)
		}
		invitations := tenant.Group("/invitations")
		{
			invitations.GET("", invitationPermissions["iam.tenant_invitation.read"], runtime.TenantInvitationHandler.List)
			invitations.POST("", invitationPermissions["iam.tenant_invitation.create"], runtime.TenantInvitationHandler.Create)
			invitations.GET("/:id", invitationPermissions["iam.tenant_invitation.read"], runtime.TenantInvitationHandler.Get)
			invitations.POST("/:id/revoke", invitationPermissions["iam.tenant_invitation.revoke"], runtime.TenantInvitationHandler.Revoke)
		}
		memberships := tenant.Group("/memberships")
		{
			memberships.GET("", membershipPermissions["iam.tenant_membership.read"], runtime.TenantMembershipHandler.List)
			memberships.GET("/:id", membershipPermissions["iam.tenant_membership.read"], runtime.TenantMembershipHandler.Get)
			memberships.PUT("/:id", membershipPermissions["iam.tenant_membership.update"], runtime.TenantMembershipHandler.Update)
			memberships.POST("/:id/suspend", membershipPermissions["iam.tenant_membership.suspend"], runtime.TenantMembershipHandler.Suspend)
			memberships.POST("/:id/restore", membershipPermissions["iam.tenant_membership.restore"], runtime.TenantMembershipHandler.Restore)
			memberships.POST("/:id/close", membershipPermissions["iam.tenant_membership.close"], runtime.TenantMembershipHandler.Close)
		}
		auditEvents := tenant.Group("/audit/events")
		{
			auditEvents.GET("", tenantAuditRead, runtime.AuditHandler.TenantList)
			auditEvents.GET("/summary", tenantAuditRead, runtime.AuditHandler.TenantSummary)
			auditEvents.GET("/trends", tenantAuditRead, runtime.AuditHandler.TenantTrends)
			auditEvents.GET("/export", tenantAuditExport, runtime.AuditHandler.TenantExport)
			auditEvents.GET("/:id", tenantAuditRead, runtime.AuditHandler.TenantGet)
		}
	}
	return nil
}

func permissionGuards(
	build func(string) (gin.HandlerFunc, error),
	keys []string,
) (map[string]gin.HandlerFunc, error) {
	guards := make(map[string]gin.HandlerFunc, len(keys))
	for _, key := range keys {
		guard, err := build(key)
		if err != nil {
			return nil, err
		}
		guards[key] = guard
	}
	return guards, nil
}
