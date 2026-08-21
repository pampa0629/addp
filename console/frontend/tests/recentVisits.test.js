import { describe, expect, it } from 'vitest'
import { buildRecentVisitEntry, prependRecentVisit } from '../src/utils/recentVisits'

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

  it('prefers a globally self-contained recent label over the contextual sidebar label', () => {
    expect(buildRecentVisitEntry({
      module: 'transfer',
      fullPath: '/transfer/executions',
      menuConfig: {
        label: 'console.menus.transfer.label',
        items: [{
          index: '/transfer/executions',
          label: 'console.menus.transfer.executions',
          recentLabel: 'console.menus.transfer.recentExecutions'
        }]
      }
    })).toMatchObject({
      label: 'console.menus.transfer.recentExecutions'
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
})
