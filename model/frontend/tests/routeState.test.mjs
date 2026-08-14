import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildEntityListRouteQuery,
  buildERDiagramRouteQuery,
  buildLogicalTableListRouteQuery,
  resolveEntityListRouteState,
  resolveLogicalTableListRouteState,
  resolveERDiagramRouteState
} from '../src/utils/routeState.js'

test('entity detail return state preserves business-domain filter', () => {
  const state = resolveEntityListRouteState({ domain_id: '2', page: '3', tab: 'attributes' })
  assert.deepEqual(state.query, { domain_id: '2', page: '3' })
})

test('logical table route state keeps layer and domain filters', () => {
  const state = resolveLogicalTableListRouteState({ domain_id: '1', layer: 'dwd', tab: 'fields' })
  assert.deepEqual(state.query, { domain_id: '1', layer: 'dwd' })
})

test('ER diagram route state canonicalizes domain filter', () => {
  assert.deepEqual(resolveERDiagramRouteState({ domain_id: '4', extra: 'ignored' }).query, { domain_id: '4' })
})

test('list and ER route builders preserve selected business domain in detail navigation', () => {
  assert.deepEqual(buildEntityListRouteQuery({ domainId: 2, page: 1, pageSize: 20 }), { domain_id: '2' })
  assert.deepEqual(buildLogicalTableListRouteQuery({ domainId: 2, page: 1, pageSize: 20 }), { domain_id: '2' })
  assert.deepEqual(buildERDiagramRouteQuery({ domainId: 2 }), { domain_id: '2' })
})

test('invalid business-domain route values are removed from canonical URL state', () => {
  assert.equal(resolveEntityListRouteState({ domain_id: '0' }).domainId, null)
  assert.deepEqual(resolveEntityListRouteState({ domain_id: '0' }).query, {})
  assert.equal(resolveERDiagramRouteState({ domain_id: 'outside' }).domainId, null)
  assert.deepEqual(resolveERDiagramRouteState({ domain_id: 'outside' }).query, {})
})
