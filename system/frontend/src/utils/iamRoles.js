export const TENANT_ADMINISTRATOR_ROLE_KEY = 'tenant.administrator'

export function isValidTenantRoleKey(value) {
  return typeof value === 'string' && /^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$/.test(value.trim())
}

export const TENANT_ROLE_RECOMMENDATIONS = [
  {
    roleKey: 'tenant.infrastructure_administrator',
    labelKey: 'system.iam.roleAssignments.recommendations.infrastructure'
  },
  {
    roleKey: 'tenant.data_viewer',
    labelKey: 'system.iam.roleAssignments.recommendations.metaRead'
  },
  {
    roleKey: 'tenant.data_steward',
    labelKey: 'system.iam.roleAssignments.recommendations.metaManage'
  }
]

export function tenantRoleKeys(authContext) {
  if (authContext?.context?.type !== 'tenant') return []
  return [...new Set((authContext.authorization?.role_assignments || []).map((assignment) => assignment.role_key))]
}

export function hasTenantRole(authContext, roleKey, scopeType = '') {
  if (authContext?.context?.type !== 'tenant') return false
  return (authContext.authorization?.role_assignments || []).some((assignment) =>
    assignment.role_key === roleKey && (!scopeType || assignment.scope?.type === scopeType)
  )
}

export function needsTenantRoleSetup(authContext) {
  const roleKeys = tenantRoleKeys(authContext)
  return roleKeys.length === 1 && roleKeys[0] === TENANT_ADMINISTRATOR_ROLE_KEY
}

export function buildTenantRoleOptions(roles, assignments, selection) {
  const activeAssignments = (assignments || []).filter((assignment) => assignment.status === 'active')
  return (roles || [])
    .filter((role) =>
      (role.allowed_principal_types || []).includes(selection.principalType) &&
      (role.allowed_scope_types || []).includes('tenant')
    )
    .map((role) => {
      const roleAssignments = activeAssignments.filter((assignment) => assignment.role_id === role.id)
      const assigned = roleAssignments.length > 0
      return {
        ...role,
        assigned
      }
    })
    .sort((left, right) => Number(left.assigned) - Number(right.assigned) || left.role_key.localeCompare(right.role_key))
}

export function partitionTenantRoleOptions(roleOptions) {
  return {
    available: (roleOptions || []).filter((role) => !role.assigned),
    assigned: (roleOptions || []).filter((role) => role.assigned)
  }
}

export function summarizeEffectiveTenantRoles(assignments) {
  const roles = new Map()
  for (const assignment of assignments) {
    if (assignment.effective_state === 'effective') roles.set(assignment.role_id, assignment)
  }
  return [...roles.values()].sort((left, right) => left.role_key.localeCompare(right.role_key))
}
