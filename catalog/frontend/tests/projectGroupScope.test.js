import { describe, expect, it } from 'vitest'
import { canAccessProjectGroup, projectGroupsForPermission } from '../src/utils/projectGroupScope.js'

const context = {
  organization: { project_groups: [{ project_group_id: '9' }, { project_group_id: '10' }] },
  authorization: {
    role_assignments: [
      { scope: { type: 'project_group', project_group_id: '9' }, permissions: ['catalog.collection.update'] },
      { scope: { type: 'tenant', tenant_id: '7' }, permissions: ['catalog.collection.read'] }
    ]
  }
}

describe('project group scoped permissions', () => {
  it('expands tenant scope only across active memberships', () => {
    expect(projectGroupsForPermission(context, 'catalog.collection.read').map(item => item.project_group_id)).toEqual(['9', '10'])
  })

  it('keeps project group scope exact', () => {
    expect(projectGroupsForPermission(context, 'catalog.collection.update').map(item => item.project_group_id)).toEqual(['9'])
    expect(canAccessProjectGroup(context, 'catalog.collection.update', '10')).toBe(false)
  })
})
