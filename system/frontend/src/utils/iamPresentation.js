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
