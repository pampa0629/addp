import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildTableRelationRoute,
  resolveLogicalTableDetailRouteState,
  buildEntityListRouteQuery,
  buildERDiagramRouteQuery,
  buildLogicalTableListRouteQuery,
  resolveEntityListRouteState,
  resolveLogicalTableListRouteState,
  resolveERDiagramRouteState,
  buildDimensionalModelRouteQuery,
  resolveDimensionalModelRouteState,
  buildMetricImplementationListRouteQuery,
  resolveMetricImplementationListRouteState
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

test('ER overview and cross-domain expansion are explicit and recoverable', () => {
  assert.deepEqual(resolveERDiagramRouteState({ domain_id: 'all', related: '1' }).query, { domain_id: 'all' })
  assert.deepEqual(resolveERDiagramRouteState({ domain_id: '2', related: '1' }).query, { domain_id: '2', related: '1' })
  assert.deepEqual(resolveERDiagramRouteState({ related: '1' }).query, {})
})

test('dimensional modeling restores domain and fact selection without inventing defaults', () => {
  const state = resolveDimensionalModelRouteState({ domain_id: '2', table_id: '6' })
  assert.equal(state.domainId, 2)
  assert.equal(state.tableId, 6)
  assert.equal(state.changed, false)
  assert.deepEqual(buildDimensionalModelRouteQuery(state), { domain_id: '2', table_id: '6' })
  assert.deepEqual(buildDimensionalModelRouteQuery({ domainId: null, tableId: 6 }), { table_id: '6' })
  assert.deepEqual(buildDimensionalModelRouteQuery({ domainId: 2, tableId: null }), { domain_id: '2' })
  assert.deepEqual(resolveDimensionalModelRouteState().query, {})
})

test('dimensional modeling canonicalizes invalid IDs and removes unrelated query state', () => {
  assert.deepEqual(resolveDimensionalModelRouteState({ domain_id: '-2', table_id: 'invalid', tab: 'basic' }).query, {})
  const state = resolveDimensionalModelRouteState({ domain_id: '02', table_id: '06' })
  assert.equal(state.changed, true)
  assert.deepEqual(state.query, { domain_id: '2', table_id: '6' })
})

test('metric implementation list restores its source domain and fact table independently', () => {
  const state = resolveMetricImplementationListRouteState({ source_domain_id: '02', fact_table_id: '6', revision_id: '9' })
  assert.equal(state.sourceDomainId, 2)
  assert.equal(state.factTableId, 6)
  assert.equal(state.changed, true)
  assert.deepEqual(state.query, { source_domain_id: '2', fact_table_id: '6' })
  assert.deepEqual(buildMetricImplementationListRouteQuery({ sourceDomainId: null, factTableId: null }), {})
  assert.deepEqual(resolveMetricImplementationListRouteState({ source_domain_id: 'unassigned' }).query, {})
})

test('table relation links preserve source identity and recoverable selection', () => {
  assert.deepEqual(buildTableRelationRoute(3, 7, { domain_id: '2', table_id: '6' }), {
    path: '/logical-tables/3', query: { domain_id: '2', tab: 'relations', relation_id: '7' }
  })
  assert.deepEqual(resolveLogicalTableDetailRouteState({ tab: 'relations', relation_id: '7' }, 'dimension').relationId, 7)
  assert.deepEqual(resolveLogicalTableDetailRouteState({ tab: 'relations', relation_id: '7' }, 'entity').query, {})
  assert.deepEqual(resolveLogicalTableDetailRouteState({ tab: 'definition', relation_id: '7', domain_id: '2' }, 'fact').query, { domain_id: '2' })
  assert.deepEqual(resolveLogicalTableDetailRouteState({ tab: 'relations', relation_id: '-7' }, 'fact').query, { tab: 'relations' })
  assert.deepEqual(resolveLogicalTableDetailRouteState({ tab: 'concept-mappings', relation_id: '7' }, 'entity').query, { tab: 'concept-mappings' })
  assert.deepEqual(resolveLogicalTableDetailRouteState({ tab: 'physical-target', relation_id: '7' }, 'entity').query, { tab: 'physical-target' })
})
