import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildDDLPreviewRequest,
  buildDWLayerUpdateRequest,
  buildEntityAttributeUpdateRequest,
  buildLogicalFieldUpdateRequest,
  buildLogicalTableUpdateRequest,
  canPerformDraftAction,
  isEditableDraft,
  resolvePositiveRouteId
} from '../src/utils/modelDetailState.js'

test('detail route IDs must be positive integers', () => {
  assert.equal(resolvePositiveRouteId('2'), 2)
  assert.equal(resolvePositiveRouteId('0'), null)
  assert.equal(resolvePositiveRouteId('abc'), null)
})

test('PUT payloads preserve complete nullable and zero-valued model state', () => {
  assert.deepEqual(buildLogicalTableUpdateRequest(
    { name: 'Order', domain_id: null, table_type: 'entity', layer: 'dwd' },
    { entity_id: 7 },
    { schema_name: '', table_name: '', partition_by: '', partition_type: 'range' }
  ), {
    name: 'Order', domain_id: null, entity_id: 7, table_type: 'entity', layer: 'dwd',
    materialization: { schema_name: '', table_name: '', partition_by: '', partition_type: 'range' }
  })

  assert.deepEqual(buildEntityAttributeUpdateRequest({
    name: 'ID', element_id: null, is_pk: false, nullable: false, sort_order: 0
  }), { name: 'ID', element_id: null, is_pk: false, nullable: false, sort_order: 0 })

  assert.deepEqual(buildLogicalFieldUpdateRequest({
    name: 'ID', element_id: null, length: null, nullable: false, is_pk: false,
    is_partition: false, sort_order: 0, hierarchy_id: null, hierarchy_level: 0
  }), {
    name: 'ID', element_id: null, length: null, nullable: false, is_pk: false,
    is_partition: false, sort_order: 0, hierarchy_id: null, hierarchy_level: 0
  })

  assert.deepEqual(buildDWLayerUpdateRequest({ layer_name: 'DWD', sort_order: 0 }, { quality_sla: null }), {
    layer_name: 'DWD', sort_order: 0, quality_sla: null
  })
})

test('only draft models with update permission are editable', () => {
  assert.equal(isEditableDraft('draft', true), true)
  assert.equal(isEditableDraft('approved', true), false)
  assert.equal(isEditableDraft('draft', false), false)
})

test('draft resource actions depend on their own permission instead of update permission', () => {
  assert.equal(canPerformDraftAction('draft', true), true)
  assert.equal(canPerformDraftAction('approved', true), false)
  assert.equal(canPerformDraftAction('draft', false), false)
})

test('DDL preview payload contains only current materialization fields', () => {
  assert.deepEqual(buildDDLPreviewRequest({
    schema_name: ' analytics ',
    table_name: 'fact_order',
    partition_by: '',
    partition_type: 'RANGE',
    extra_options: 'ignored'
  }), {
    materialization: {
      schema_name: 'analytics',
      table_name: 'fact_order',
      partition_by: '',
      partition_type: 'range'
    }
  })
})
