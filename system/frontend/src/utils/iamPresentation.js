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

export function formatMemberOptionLabel(member, currentMembershipID, currentAccountLabel) {
  const displayName = member?.display_name || member?.service_principal_name || member?.principal_id || '-'
  const identifier = member?.username || member?.service_principal_name || member?.principal_id || '-'
  const label = `${displayName} (${identifier})`
  return member?.id === currentMembershipID ? `${currentAccountLabel} · ${label}` : label
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
