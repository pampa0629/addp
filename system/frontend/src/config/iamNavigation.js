export const IAM_PAGES = [
  {
    key: 'organization',
    path: '/iam/organization',
    routeName: 'IAMOrganization',
    label: 'system.iam.pages.organization'
  },
  {
    key: 'accounts',
    path: '/iam/accounts',
    routeName: 'IAMAccounts',
    label: 'system.iam.pages.accounts'
  },
  {
    key: 'roles',
    path: '/iam/roles',
    routeName: 'IAMRoles',
    label: 'system.iam.pages.roles'
  },
  {
    key: 'application-access',
    path: '/iam/application-access',
    routeName: 'IAMApplicationAccess',
    label: 'system.iam.pages.applicationAccess'
  },
  {
    key: 'security',
    path: '/iam/security',
    routeName: 'IAMSecurity',
    label: 'system.iam.pages.security'
  }
]

export const IAM_TABS = [
  { page: 'organization', key: 'tenants', context: 'platform', permission: 'platform.tenant.read', label: 'system.iam.tabs.tenants', panel: 'tenants' },
  { page: 'organization', key: 'departments', context: 'tenant', permission: 'iam.department.read', label: 'system.iam.tabs.departments', panel: 'departments' },
  { page: 'organization', key: 'project-groups', context: 'tenant', permission: 'iam.project_group.read', label: 'system.iam.tabs.projectGroups', panel: 'project-groups' },

  { page: 'accounts', key: 'users', context: 'platform', permission: 'iam.user.read', label: 'system.iam.tabs.userAccounts', panel: 'users' },
  { page: 'accounts', key: 'identity-changes', context: 'platform', permission: 'iam.platform_identity_change.read', label: 'system.iam.tabs.identityChanges', panel: 'identity-changes' },
  { page: 'accounts', key: 'user-accounts', context: 'tenant', permission: 'iam.tenant_membership.read', label: 'system.iam.tabs.userAccounts', panel: 'user-accounts' },
  { page: 'accounts', key: 'invitations', context: 'tenant', permission: 'iam.tenant_invitation.read', label: 'system.iam.tabs.userInvitations', panel: 'invitations' },
  { page: 'accounts', key: 'account-security', context: 'any', label: 'system.iam.tabs.accountSecurity', panel: 'account-security' },

  { page: 'roles', key: 'role-definitions', context: 'tenant', permission: 'iam.tenant_role.read', label: 'system.iam.tabs.roleDefinitions', panel: 'roles' },
  { page: 'roles', key: 'role-assignments', context: 'tenant', permission: 'iam.tenant_role_assignment.read', label: 'system.iam.tabs.roleAssignments', panel: 'role-assignments' },

  { page: 'application-access', key: 'service-accounts', context: 'tenant', permission: 'iam.service_account.read', label: 'system.iam.tabs.serviceAccounts', panel: 'service-accounts' },
  { page: 'application-access', key: 'oauth-clients', context: 'tenant', permission: 'iam.oauth_client.read', label: 'system.iam.tabs.externalApplications', panel: 'oauth-clients' },

  { page: 'security', key: 'security-policy', context: 'platform', permission: 'iam.security_policy.read', label: 'system.iam.tabs.securityPolicy', panel: 'security-policy' },
  { page: 'security', key: 'audit', context: 'platform', permission: 'audit.event.read', label: 'system.iam.tabs.audit', panel: 'audit', props: { scope: 'platform' } },
  { page: 'security', key: 'audit', context: 'tenant', permission: 'audit.tenant_event.read', label: 'system.iam.tabs.audit', panel: 'audit', props: { scope: 'tenant' } }
]

export function iamTabIsAvailable(tab, contextType, hasPermission) {
  if (tab.context !== 'any' && tab.context !== contextType) return false
  if (tab.permission && !hasPermission(tab.permission)) return false
  if (tab.permissionsAny?.length && !tab.permissionsAny.some(hasPermission)) return false
  return true
}

export function availableIAMTabs(pageKey, contextType, hasPermission) {
  return IAM_TABS.filter(tab => tab.page === pageKey && iamTabIsAvailable(tab, contextType, hasPermission))
}

export function availableIAMPages(contextType, hasPermission) {
  return IAM_PAGES.filter(page => availableIAMTabs(page.key, contextType, hasPermission).length > 0)
}

export function findIAMPage(pageKey) {
  return IAM_PAGES.find(page => page.key === pageKey) || null
}
