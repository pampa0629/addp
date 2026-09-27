import { describe, expect, it, vi } from 'vitest'
import { filterSidebarMenus, matchesNavigationAccess } from '../src/utils/navigationAccess'
import { consoleRouteAccess } from '@common-ui'

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
  it('requires every permission when an entry declares all mode', () => {
    const entry = { permissions: ['quality.plan.read', 'quality.issue.read', 'monitor.execution.read'], permissionMode: 'all' }
    for (const missing of entry.permissions) {
      expect(matchesNavigationAccess(entry, 'tenant', entry.permissions.filter(p => p !== missing))).toBe(false)
    }
    expect(matchesNavigationAccess(entry, 'tenant', entry.permissions)).toBe(true)
  })
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

  it('keeps Console sidebars as unique page destinations without duplicate create actions', async () => {
    vi.stubGlobal('window', { location: { protocol: 'http:', hostname: 'localhost', origin: 'http://localhost' } })
    try {
      const { SIDEBAR_MENUS } = await import('../src/config/portalConfig.js')
      for (const [module, menu] of Object.entries(SIDEBAR_MENUS)) {
        const items = menu.items?.flatMap(item => item.children || [item]) || [menu]
        const indexes = items.map(item => item.index)
        const labels = items.map(item => item.label)
        expect(new Set(indexes).size, `${module} has duplicate destinations`).toBe(indexes.length)
        expect(new Set(labels).size, `${module} has duplicate labels`).toBe(labels.length)

        for (const item of items) {
          expect(consoleRouteAccess(item.index), item.index).toBeTruthy()
          if (!/\/(?:create|new|edit)(?:\/|$)/.test(item.index)) continue
          expect(item.fallbackFor, `${item.index} needs a parent page`).toBeTruthy()
          expect(indexes, `${item.index} needs a visible parent`).toContain(item.fallbackFor)
          const permissions = [item.index, item.fallbackFor]
            .flatMap(path => consoleRouteAccess(path) || [])
            .flatMap(rule => rule.permissions || [])
          const visible = filterSidebarMenus({ [module]: menu }, 'tenant', permissions)[module].items
          expect(visible.map(entry => entry.index)).not.toContain(item.index)
        }
      }
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
