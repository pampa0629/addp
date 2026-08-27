export function projectGroupsForPermission(authContext, permission) {
  const memberships = authContext?.organization?.project_groups || []
  const scopes = []
  for (const assignment of authContext?.authorization?.role_assignments || []) {
    if ((assignment.permissions || []).includes(permission)) scopes.push(assignment.scope || {})
  }
  const tenantScope = scopes.some(scope => scope.type === 'tenant')
  const projectGroupIDs = new Set(scopes.filter(scope => scope.type === 'project_group').map(scope => String(scope.project_group_id || '')))
  return memberships.filter(membership => tenantScope || projectGroupIDs.has(String(membership.project_group_id)))
}

export function canAccessProjectGroup(authContext, permission, projectGroupID) {
  return projectGroupsForPermission(authContext, permission).some(membership => String(membership.project_group_id) === String(projectGroupID))
}
