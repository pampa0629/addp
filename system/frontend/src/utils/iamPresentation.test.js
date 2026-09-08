import { describe, expect, it } from 'vitest'
import {
  formatMemberOptionLabel,
  groupPermissionsByNamespace,
  resolveIAMModuleName
} from './iamPresentation'

describe('IAM presentation helpers', () => {
  it('marks the current tenant membership without guessing from its username', () => {
    const member = { id: '7', display_name: 'Alice', username: 'alice', principal_id: '3' }
    expect(formatMemberOptionLabel(member, '7', 'Current account')).toBe('Current account · Alice (alice)')
    expect(formatMemberOptionLabel(member, '8', 'Current account')).toBe('Alice (alice)')
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
