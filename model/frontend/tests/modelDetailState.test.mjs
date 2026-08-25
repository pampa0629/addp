import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import {
  buildDDLPreviewRequest,
  buildDWLayerUpdateRequest,
  buildEntityAttributeUpdateRequest,
  buildLogicalFieldUpdateRequest,
  buildLogicalTableUpdateRequest,
  canPerformDraftAction,
  isEditableDraft,
  resolvePositiveRouteId,
  snapshotUnsavedState
} from '../src/utils/modelDetailState.js'

test('detail route IDs must be positive integers', () => {
  assert.equal(resolvePositiveRouteId('2'), 2)
  assert.equal(resolvePositiveRouteId('0'), null)
  assert.equal(resolvePositiveRouteId('abc'), null)
})

test('unsaved state snapshots change only when editable state changes', () => {
  const saved = snapshotUnsavedState({
    form: { name: 'Order', domain_id: null },
    dialog: null
  })

  assert.equal(snapshotUnsavedState({ form: { name: 'Order', domain_id: null }, dialog: null }), saved)
  assert.notEqual(snapshotUnsavedState({ form: { name: 'Order updated', domain_id: null }, dialog: null }), saved)
  assert.notEqual(snapshotUnsavedState({ form: { name: 'Order', domain_id: null }, dialog: { name: 'ID' } }), saved)
})

test('entity and logical table details preserve unsaved drafts across navigation and conflicts', async () => {
  for (const filename of ['EntityDetail.vue', 'LogicalTableDetail.vue']) {
    const source = await readFile(new URL(`../src/views/${filename}`, import.meta.url), 'utf8')
    assert.match(source, /useUnsavedChanges\(\{ state: unsavedState, t \}\)/)
    assert.match(source, /v-if="isDirty"/)
    assert.match(source, /confirmDiscardChanges\(\)/)
    assert.match(source, /markSaved\(\)/)
    assert.match(source, /model\.common\.save_before_action/)
  }
})

test('logical table details load metric names before rendering persisted mappings', async () => {
  const source = await readFile(new URL('../src/views/LogicalTableDetail.vue', import.meta.url), 'utf8')
  assert.match(source, /Promise\.all\(\[loadFields\(\), loadMetrics\(\), loadAvailableMetrics\(\)\]\)/)
  assert.match(source, /const metricNameMap = computed\(\(\) => \{/)
})

test('PUT payloads preserve complete nullable and zero-valued model state', () => {
  assert.deepEqual(buildLogicalTableUpdateRequest(
    { name: 'Order', domain_id: null, table_type: 'entity', layer: 'dwd' },
    { entity_id: 7, version: 3 },
    { schema_name: '', table_name: '', partition_by: '', partition_type: 'range' }
  ), {
    name: 'Order', domain_id: null, entity_id: 7, version: 3, table_type: 'entity', layer: 'dwd',
    materialization: { schema_name: '', table_name: '', partition_by: '', partition_type: 'range' }
  })

  assert.deepEqual(buildEntityAttributeUpdateRequest({
    name: 'ID', element_id: null, is_pk: false, nullable: false, sort_order: 0
  }, 4), { name: 'ID', element_id: null, is_pk: false, nullable: false, sort_order: 0, version: 4 })

  assert.deepEqual(buildLogicalFieldUpdateRequest({
    name: 'ID', element_id: null, length: null, nullable: false, is_pk: false,
    is_partition: false, sort_order: 0, hierarchy_id: null, hierarchy_level: 0
  }, 5), {
    name: 'ID', element_id: null, length: null, nullable: false, is_pk: false,
    is_partition: false, sort_order: 0, hierarchy_id: null, hierarchy_level: 0, version: 5
  })

  assert.deepEqual(buildDWLayerUpdateRequest({ layer_name: 'DWD', sort_order: 0 }, { quality_sla: null, version: 6 }), {
    layer_name: 'DWD', sort_order: 0, quality_sla: null, version: 6
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

test('DDL preview payload normalizes absent materialization without leaking undefined fields', () => {
  assert.deepEqual(buildDDLPreviewRequest(), {
    materialization: {
      schema_name: '',
      table_name: '',
      partition_by: '',
      partition_type: ''
    }
  })
})
