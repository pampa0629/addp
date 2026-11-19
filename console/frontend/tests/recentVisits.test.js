import { describe, expect, it } from 'vitest'
import { buildRecentVisitEntry, prependRecentVisit, recentVisitStorageKey } from '../src/utils/recentVisits'

const menuConfig = {
  label: 'console.menus.graph.label',
  items: [{ index: '/graph/graphs', label: 'console.menus.graph.graphs' }]
}

describe('Console recent visits', () => {
  it('records fixed menu routes and preserves recoverable query state', () => {
    expect(buildRecentVisitEntry({
      module: 'graph',
      fullPath: '/graph/graphs?status=active',
      menuConfig
    })).toMatchObject({
      key: '/graph/graphs',
      route: '/graph/graphs?status=active',
      label: 'console.menus.graph.graphs'
    })
  })

  it('records a filtered Monitor execution view as the canonical execution history entry', () => {
    expect(buildRecentVisitEntry({
      module: 'monitor',
      fullPath: '/monitor/executions?module=transfer&task_type=sync',
      menuConfig: {
        label: 'console.menus.monitor.label',
        items: [{
          index: '/monitor/executions',
          label: 'console.menus.monitor.executions',
          recentLabel: 'console.menus.monitor.recentExecutions'
        }]
      }
    })).toMatchObject({
      key: '/monitor/executions',
      route: '/monitor/executions?module=transfer&task_type=sync',
      label: 'console.menus.monitor.recentExecutions'
    })
  })

  it('records fixed routes nested under a business category', () => {
    expect(buildRecentVisitEntry({
      module: 'system',
      fullPath: '/system/iam/roles?tab=role-assignments',
      menuConfig: {
        label: 'console.menus.system.label',
        items: [{
          index: '/system/iam',
          label: 'console.menus.system.iam',
          children: [{
            index: '/system/iam/roles',
            label: 'console.menus.system.iamRoles'
          }]
        }]
      }
    })).toMatchObject({
      key: '/system/iam/roles',
      route: '/system/iam/roles?tab=role-assignments',
      label: 'console.menus.system.iamRoles'
    })
  })

  it('ignores unknown dynamic routes until the module provides a descriptor', () => {
    expect(buildRecentVisitEntry({
      module: 'graph',
      fullPath: '/graph/graphs/1/browse',
      menuConfig
    })).toBeNull()

    expect(buildRecentVisitEntry({
      module: 'graph',
      fullPath: '/graph/graphs/1/browse?tab=relations',
      menuConfig,
      descriptor: { title: '图谱浏览', subject: '员工就业图', recent: true }
    })).toMatchObject({
      key: '/graph/graphs/1/browse',
      route: '/graph/graphs/1/browse?tab=relations',
      title: '图谱浏览',
      subject: '员工就业图'
    })
  })

  it('updates one page entry instead of duplicating query variants', () => {
    const oldEntry = { key: '/graph/graphs/1/browse', route: '/graph/graphs/1/browse?tab=entities' }
    const nextEntry = { key: '/graph/graphs/1/browse', route: '/graph/graphs/1/browse?tab=relations' }
    expect(prependRecentVisit([oldEntry], nextEntry)).toEqual([nextEntry])
  })

  it('gives each user and tenant or platform context its own storage key', () => {
    const tenant = (id, tenant_id) => ({
      principal: { type: 'user', id }, context: { type: 'tenant', tenant_id }
    })
    const platform = id => ({ principal: { type: 'user', id }, context: { type: 'platform' } })
    const keys = [tenant('42', '7'), tenant('43', '7'), tenant('42', '8'), platform('42'), platform('43')]
      .map(recentVisitStorageKey)
    expect(new Set(keys).size).toBe(keys.length)
    expect(keys.every(Boolean)).toBe(true)
    expect(recentVisitStorageKey(null)).toBe('')
    expect(recentVisitStorageKey(tenant('42', undefined))).toBe('')
    expect(recentVisitStorageKey({ ...platform('42'), principal: { type: 'service', id: '42' } })).toBe('')
  })

  it('retains only the most recent five entries in the supplied scope', () => {
    const items = Array.from({ length: 6 }, (_, index) => ({ key: `/agent/sessions/${index}` }))
    expect(items.reduce((entries, item) => prependRecentVisit(entries, item), [])).toEqual(items.slice(1).reverse())
  })
})
