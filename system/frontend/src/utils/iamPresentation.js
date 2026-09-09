export const AUDIT_MODULE_NAMES = Object.freeze([
  'system',
  'gateway',
  'manager',
  'meta',
  'transfer',
  'orchestrator',
  'develop',
  'service',
  'monitor',
  'standard',
  'model',
  'quality',
  'security',
  'catalog',
  'asset',
  'portal',
  'workbench',
  'agent',
  'copilot',
  'inference',
  'graph',
  'duckdb'
])

export function resolveIAMModuleName(moduleName, t, te) {
  const normalized = String(moduleName || '').trim()
  if (!normalized) return '-'
  const key = `system.iam.modules.${normalized}`
  return te(key) ? t(key) : normalized
}

export function findCurrentContextOption(options, context) {
  const contextType = String(context?.type || '')
  const tenantID = String(context?.tenant_id || '')
  const membershipID = String(context?.tenant_membership_id || '')
  return (options || []).find((option) => {
    if (option?.type !== contextType) return false
    if (option.current === true) return true
    if (contextType !== 'tenant') return true
    return String(option.tenant_id || '') === tenantID &&
      String(option.tenant_membership_id || '') === membershipID
  }) || null
}

export function assuranceLevelKey(level) {
  const normalized = String(level || '').toLowerCase()
  return ['aal1', 'aal2', 'aal3', 'not_applicable'].includes(normalized) ? normalized : 'unknown'
}

export function accountSessionIsLoading(authState) {
  if (authState?.sessionStatus === 'error') return false
  return authState?.sessionStatus === 'initializing' || (
    Boolean(authState?.isAuthenticated) && (!authState?.user || !authState?.authContext)
  )
}

export function tenantMemberDisplayName(member) {
  return member?.display_name || member?.service_principal_name || member?.username || member?.principal_id || '-'
}

export function tenantMemberIdentifier(member) {
  return member?.username || member?.service_principal_name || member?.principal_id || '-'
}

export function isCurrentTenantMembership(member, currentMembershipID) {
  return Boolean(currentMembershipID) && String(member?.id || '') === String(currentMembershipID)
}

export function formatMemberOptionLabel(member, currentMembershipID, currentAccountLabel, principalTypeLabel = '') {
  const displayName = tenantMemberDisplayName(member)
  const identifier = tenantMemberIdentifier(member)
  const identity = displayName === String(identifier) ? displayName : `${displayName} (${identifier})`
  const type = principalTypeLabel ? `${principalTypeLabel} · ` : ''
  const current = isCurrentTenantMembership(member, currentMembershipID) ? `${currentAccountLabel} · ` : ''
  return `${current}${type}${identity}`
}

export function groupTenantMembers(members, currentMembershipID, options = {}) {
  const principalType = String(options.principalType || '')
  const activeOnly = Boolean(options.activeOnly)
  const candidates = (members || [])
    .filter((member) => !activeOnly || member?.status === 'active')
    .filter((member) => !principalType || member?.principal_type === principalType)
    .sort((left, right) => {
      const displayOrder = tenantMemberDisplayName(left).localeCompare(tenantMemberDisplayName(right))
      return displayOrder || String(tenantMemberIdentifier(left)).localeCompare(String(tenantMemberIdentifier(right)))
    })
  const current = candidates.filter((member) => isCurrentTenantMembership(member, currentMembershipID))
  const grouped = candidates.filter((member) => !isCurrentTenantMembership(member, currentMembershipID))
  return [
    { key: 'current', members: current },
    { key: 'user', members: grouped.filter((member) => member?.principal_type === 'user') },
    { key: 'service_principal', members: grouped.filter((member) => member?.principal_type === 'service_principal') }
  ].filter((group) => group.members.length > 0)
}

export function roleSupportsPrincipalType(role, principalType) {
  if (!principalType) return true
  return (role?.allowed_principal_types || []).includes(principalType)
}

export function rolePrincipalTypeCounts(roles) {
  const items = roles || []
  return {
    all: items.length,
    user: items.filter((role) => roleSupportsPrincipalType(role, 'user')).length,
    service_principal: items.filter((role) => roleSupportsPrincipalType(role, 'service_principal')).length
  }
}

export function groupPermissionsByNamespace(items, getPermissionKey = (item) => item) {
  const groups = new Map()
  for (const item of items || []) {
    const permissionKey = String(getPermissionKey(item) || '').trim()
    if (!permissionKey) continue
    const namespace = permissionKey.split('.')[0]
    if (!groups.has(namespace)) groups.set(namespace, [])
    groups.get(namespace).push(item)
  }
  return [...groups.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([namespace, permissions]) => ({
      namespace,
      permissions: permissions.sort((left, right) =>
        String(getPermissionKey(left)).localeCompare(String(getPermissionKey(right)))
      )
    }))
}

export function permissionIdentity(permission) {
  const permissionKey = String(permission?.permission_key || permission || '').trim()
  const segments = permissionKey.split('.')
  return {
    permissionKey,
    ownerModule: String(permission?.owner_module || segments[0] || '').trim(),
    resource: segments.length >= 3 ? segments.slice(1, -1).join('.') : '',
    action: String(permission?.action || segments.at(-1) || '').trim()
  }
}

export function permissionResourceI18nKey(permission) {
  return `system.iam.roles.resources.${permissionIdentity(permission).resource}`
}

export function permissionMatchesScopes(permission, scopeTypes) {
  const allowed = new Set(permission?.allowed_scope_types || [])
  return (scopeTypes || []).every((scopeType) => allowed.has(scopeType))
}

export function buildPermissionGroups(items, options = {}) {
  const search = String(options.search || '').trim().toLocaleLowerCase()
  const scopeTypes = options.scopeTypes || []
  const selected = new Set(options.selectedKeys || [])
  const getSearchValues = options.getSearchValues || (() => [])
  return groupPermissionsByNamespace(
    (items || []).filter((permission) => {
      if (!permissionMatchesScopes(permission, scopeTypes)) return false
      if (!search) return true
      const identity = permissionIdentity(permission)
      return [
        identity.permissionKey,
        identity.ownerModule,
        identity.resource,
        identity.action,
        permission?.name_i18n_key,
        permission?.description_i18n_key,
        ...getSearchValues(permission)
      ].some((value) => String(value || '').toLocaleLowerCase().includes(search))
    }),
    (permission) => permissionIdentity(permission).ownerModule
  ).map((group) => ({
    ...group,
    selectedCount: group.permissions.filter((permission) => selected.has(permission.permission_key)).length
  }))
}
