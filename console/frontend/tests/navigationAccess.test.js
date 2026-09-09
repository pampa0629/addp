import { describe, expect, it } from 'vitest'
import { filterSidebarMenus, matchesNavigationAccess } from '../src/utils/navigationAccess'

const menus = {
  system: {
    items: [{
      index: '/system/iam',
      children: [
        {
          index: '/system/iam/accounts',
          access: [
            { context: 'platform', permissions: ['iam.user.read', 'iam.platform_identity_change.read'] },
            { context: 'tenant', permissions: ['iam.tenant_membership.read', 'iam.tenant_invitation.read'] }
          ]
        },
        {
          index: '/system/iam/roles',
          access: [{ context: 'tenant', permissions: ['iam.tenant_role.read'] }]
        },
        {
          index: '/system/iam/security',
          access: [{ context: 'platform', permissions: ['audit.event.read'] }]
        }
      ]
    }]
  }
}

describe('Console navigation access filtering', () => {
  it('matches access rules by both AuthContext type and any granted permission', () => {
    const entry = menus.system.items[0].children[0]
    expect(matchesNavigationAccess(entry, 'platform', ['iam.user.read'])).toBe(true)
    expect(matchesNavigationAccess(entry, 'tenant', ['iam.user.read'])).toBe(false)
    expect(matchesNavigationAccess(entry, 'tenant', ['iam.tenant_membership.read'])).toBe(true)
  })

  it('recursively keeps only IAM categories with management permission', () => {
    const filtered = filterSidebarMenus(menus, 'platform', ['iam.user.read'])
    expect(filtered.system.items[0].children.map(item => item.index)).toEqual([
      '/system/iam/accounts'
    ])
  })

  it('removes a nested parent when none of its children are available', () => {
    const restricted = {
      system: {
        items: [{
          index: '/system/iam',
          children: [{
            index: '/system/iam/roles',
            access: [{ context: 'tenant', permissions: ['iam.tenant_role.read'] }]
          }]
        }]
      }
    }
    expect(filterSidebarMenus(restricted, 'platform', []).system.items).toEqual([])
  })
})
