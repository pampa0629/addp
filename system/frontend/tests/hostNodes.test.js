import { describe, expect, it } from 'vitest'
import { resolveHostNodesRouteState } from '../src/utils/routeState'

describe('host node route contract', () => {
  it('preserves literal search and bounded pagination while omitting defaults', () => {
    expect(resolveHostNodesRouteState({ search: '  host_%  ', page: '2', page_size: '50', legacy: 'x' })).toEqual({
      query: { search: 'host_%', page: '2', page_size: '50' }, search: 'host_%', nodeKind: '', enabled: '', page: 2, pageSize: 50, changed: true
    })
    expect(resolveHostNodesRouteState({ page: '1', page_size: '20' }).query).toEqual({})
  })
  it('preserves exact type and false state filters, omitting all and invalid values', () => {
    const query = { node_kind: 'physical', enabled: 'false', search: 'shared' }
    expect(resolveHostNodesRouteState(query)).toMatchObject({ query, nodeKind: 'physical', enabled: 'false', changed: false })
    for (const value of [{ node_kind: 'all', enabled: 'all' }, { node_kind: ['virtual'], enabled: ['true'] },
      { node_kind: 'VIRTUAL', enabled: '0' }, { node_kind: '', enabled: 'TRUE' }]) expect(resolveHostNodesRouteState(value).query).toEqual({})
  })
  it('rejects ambiguous, control-bearing and unbounded values', () => {
    for (const query of [{ search: ['A', 'B'] }, { search: 'a\u0000b' }, { search: 'a'.repeat(256) }, { page: '1000001' },
      { page: ['2', '3'] }, { page_size: '200' }, { page: '1.2' }]) expect(resolveHostNodesRouteState(query).query).toEqual({})
  })
})
