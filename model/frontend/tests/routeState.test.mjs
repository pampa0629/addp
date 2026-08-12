import test from 'node:test'
import assert from 'node:assert/strict'
import {
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
