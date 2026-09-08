import { describe, expect, it } from 'vitest'
import {
  formatMemberOptionLabel,
  groupTenantMembers,
  groupPermissionsByNamespace,
  resolveIAMModuleName
} from './iamPresentation'

describe('IAM presentation helpers', () => {
  it('marks the current tenant membership without guessing from its username', () => {
    const member = { id: '7', display_name: 'Alice', username: 'alice', principal_id: '3' }
    expect(formatMemberOptionLabel(member, '7', 'Current account')).toBe('Current account · Alice (alice)')
    expect(formatMemberOptionLabel(member, '8', 'Current account')).toBe('Alice (alice)')
  })

  it('groups current, user, and service accounts without mixing their types', () => {
    const members = [
      { id: 3, display_name: 'Worker', service_principal_name: 'addp-worker', principal_type: 'service_principal', status: 'active' },
      { id: 2, display_name: 'Bob', username: 'bob', principal_type: 'user', status: 'suspended' },
      { id: 1, display_name: 'Alice', username: 'alice', principal_type: 'user', status: 'active' }
    ]
    expect(groupTenantMembers(members, '1').map((group) => ({
      key: group.key,
      ids: group.members.map((member) => member.id)
    }))).toEqual([
      { key: 'current', ids: [1] },
      { key: 'user', ids: [2] },
      { key: 'service_principal', ids: [3] }
    ])
    expect(groupTenantMembers(members, '1', { activeOnly: true, principalType: 'user' }).map((group) => group.key)).toEqual(['current'])
  })

  it('includes the account type in searchable option labels', () => {
    const member = { id: '7', display_name: 'Alice', username: 'alice', principal_type: 'user' }
    expect(formatMemberOptionLabel(member, '7', 'Current account', 'User account')).toBe('Current account · User account · Alice (alice)')
  })

  it('groups permissions by stable namespace and sorts the result', () => {
    expect(groupPermissionsByNamespace(['meta.scan', 'catalog.read', 'meta.read'])).toEqual([
      { namespace: 'catalog', permissions: ['catalog.read'] },
      { namespace: 'meta', permissions: ['meta.read', 'meta.scan'] }
    ])
  })

  it('uses localized module names and falls back to the stable identifier', () => {
    const t = (key) => ({ 'system.iam.modules.system': 'System Management' })[key] || key
    const te = (key) => key === 'system.iam.modules.system'
    expect(resolveIAMModuleName('system', t, te)).toBe('System Management')
    expect(resolveIAMModuleName('extension', t, te)).toBe('extension')
  })
})
