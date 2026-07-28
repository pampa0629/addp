export const TENANT_ADMINISTRATOR_ROLE_KEY = 'tenant.administrator'

const TENANT_SCOPE_I18N_KEYS = Object.freeze({
  tenant: 'system.iam.roles.scope.tenant',
  department: 'system.iam.roles.scope.department',
  project_group: 'system.iam.roles.scope.project_group'
})

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

export function resolveRoleName(role, t, te) {
  if (role?.name) return role.name
  if (role?.name_i18n_key && te(role.name_i18n_key)) return t(role.name_i18n_key)
  if (role?.role_name) return role.role_name
  if (role?.role_name_i18n_key && te(role.role_name_i18n_key)) return t(role.role_name_i18n_key)
  return role?.role_key || ''
}

export function resolveRoleDescription(role, t, te) {
  if (role?.description) return role.description
  if (role?.description_i18n_key && te(role.description_i18n_key)) return t(role.description_i18n_key)
  const fallbackKey = role?.role_key ? `roles.${role.role_key}.description` : ''
  return fallbackKey && te(fallbackKey) ? t(fallbackKey) : ''
}

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

function normalizedScopeValue(value) {
  return value == null ? '' : String(value).trim()
}

export function resolveTenantScopeLabel(scope, t) {
  const normalizedScope = normalizedScopeValue(scope)
  const i18nKey = TENANT_SCOPE_I18N_KEYS[normalizedScope]
  return i18nKey ? t(i18nKey) : normalizedScope || '-'
}

export function formatTenantAssignmentScope(assignment, t) {
  const scopeType = normalizedScopeValue(assignment?.scope_type)
  const label = resolveTenantScopeLabel(scopeType, t)
  if (scopeType === 'department') return `${label} #${normalizedScopeValue(assignment?.department_id)}`
  if (scopeType === 'project_group') return `${label} #${normalizedScopeValue(assignment?.project_group_id)}`
  return label
}

function assignmentMatchesScope(assignment, selection) {
  if (assignment.scope_type !== selection.scopeType) return false
  if (selection.scopeType === 'department') {
    return normalizedScopeValue(assignment.department_id) === normalizedScopeValue(selection.departmentId)
  }
  if (selection.scopeType === 'project_group') {
    return normalizedScopeValue(assignment.project_group_id) === normalizedScopeValue(selection.projectGroupId)
  }
  return true
}

export function buildTenantRoleOptions(roles, assignments, selection) {
  const activeAssignments = (assignments || []).filter((assignment) => assignment.status === 'active')
  return (roles || [])
    .filter((role) => (role.allowed_scope_types || []).includes(selection.scopeType))
    .map((role) => {
      const roleAssignments = activeAssignments.filter((assignment) => assignment.role_id === role.id)
      const assigned = roleAssignments.some((assignment) => assignmentMatchesScope(assignment, selection))
      return {
        ...role,
        assigned,
        assignedElsewhere: !assigned && roleAssignments.length > 0
      }
    })
    .sort((left, right) => Number(left.assigned) - Number(right.assigned) || left.role_key.localeCompare(right.role_key))
}
