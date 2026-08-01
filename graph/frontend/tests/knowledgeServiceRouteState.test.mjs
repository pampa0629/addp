import { describe, expect, it } from 'vitest'

import { resolveKnowledgeServiceRouteState } from '../src/utils/knowledgeServiceRouteState.js'

const graphs = [{ id: 7 }, { id: 12 }]

describe('knowledge service recoverable route state', () => {
  it('preserves a validated graph and non-default tab', () => {
    expect(resolveKnowledgeServiceRouteState({
      routeQuery: { graph_id: '7', tab: 'docs' },
      graphs
    })).toEqual({
      graphId: 7,
      tab: 'docs',
      query: { graph_id: '7', tab: 'docs' },
      changed: false
    })
  })

  it('removes an unknown graph, invalid tab, and unknown query', () => {
    expect(resolveKnowledgeServiceRouteState({
      routeQuery: { graph_id: '999', tab: 'legacy', source: 'old' },
      graphs
    })).toEqual({
      graphId: null,
      tab: 'config',
      query: {},
      changed: true
    })
  })

  it('canonicalizes duplicate values and the default tab', () => {
    expect(resolveKnowledgeServiceRouteState({
      routeQuery: { graph_id: ['07', '12'], tab: ['config', 'test'] },
      graphs
    })).toEqual({
      graphId: 7,
      tab: 'config',
      query: { graph_id: '7' },
      changed: true
    })
  })
})
