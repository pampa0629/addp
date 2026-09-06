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

test('logical table details render executable metric implementations against published definitions', async () => {
  const source = await readFile(new URL('../src/views/LogicalTableDetail.vue', import.meta.url), 'utf8')
  const api = await readFile(new URL('../src/api/model.js', import.meta.url), 'utf8')
  assert.match(source, /@click="openMetricDialog\(\)"/)
  assert.match(source, /Promise\.all\(\[loadFields\(\), loadMetrics\(\), loadAvailableMetrics\(\)\]\)/)
  assert.match(source, /const metricNameMap = computed\(\(\) => \{/)
  assert.match(source, /metric_definition_revision_id/)
  assert.match(source, /v-for="f in metricSourceFields"/)
  assert.match(source, /const metricSourceFields = computed\(\(\) => fields\.value\)/)
  assert.doesNotMatch(source, /const measureFields = computed\(\(\) =>\s*fields\.value\.filter/)
  assert.match(source, /source_config: \{ field_ids: metricForm\.field_ids \}/)
  assert.match(source, /expression_config: \{ engine:/)
  assert.match(api, /metric-implementations/)
  assert.doesNotMatch(api, /logical-tables\/\$\{tableId\}\/metrics/)
})

test('logical table materialization binds a schema locator and target name', async () => {
  const source = await readFile(new URL('../src/views/LogicalTableDetail.vue', import.meta.url), 'utf8')
  assert.match(source, /mode="node"/)
  assert.match(source, /:selectable-filter="isSchemaSelection"/)
  assert.match(source, /@update:model-value="handleTargetParentSelect"/)
  assert.match(source, /target_parent_locator/)
  assert.match(source, /clearMaterializationConfig/)
  assert.match(source, /model\.materialization\.clear_config/)
  assert.doesNotMatch(source, /materializationForm\.schema_name/)
  assert.doesNotMatch(source, /materializationForm\.table_name/)
})

test('materialized target decommission uses the exact persisted target and a dedicated permission', async () => {
  const view = await readFile(new URL('../src/views/LogicalTableDetail.vue', import.meta.url), 'utf8')
  const api = await readFile(new URL('../src/api/model.js', import.meta.url), 'utf8')
  assert.match(view, /model\.materialized_target\.delete/)
  assert.match(view, /decommissionConfirmation\.value !== decommissionTarget\.value\.confirmation/)
  assert.match(view, /target_parent_locator: decommissionTarget\.value\.locator/)
  assert.match(view, /target_name: decommissionTarget\.value\.targetName/)
  assert.match(api, /logical-tables\/\$\{id\}\/materialized-target/)
})

test('PUT payloads preserve complete nullable and zero-valued model state', () => {
  assert.deepEqual(buildLogicalTableUpdateRequest(
    { name: 'Order', domain_id: null, table_type: 'entity', layer: 'dwd' },
    { entity_id: 7, version: 3 },
    { target_parent_locator: '', target_name: '', partition_by: '', partition_type: 'range' }
  ), {
    name: 'Order', domain_id: null, entity_id: 7, version: 3, table_type: 'entity', layer: 'dwd',
    materialization: {}
  })

  assert.deepEqual(buildEntityAttributeUpdateRequest({
    name: 'ID', element_id: null, is_pk: false, nullable: false, sort_order: 0
  }, 4), { name: 'ID', element_id: null, is_pk: false, nullable: false, sort_order: 0, version: 4 })

  assert.deepEqual(buildLogicalFieldUpdateRequest({
    name: 'ID', element_id: null, length: null, nullable: false, is_pk: false,
    is_partition: false, sort_order: 0
  }, 5), {
    name: 'ID', element_id: null, length: null, nullable: false, is_pk: false,
    is_partition: false, sort_order: 0, version: 5
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
    target_parent_locator: ' addp://engine/2/path/analytics?type=schema ',
    target_name: ' fact_order ',
    partition_by: '',
    partition_type: 'RANGE',
    extra_options: 'ignored'
  }), {
    materialization: {
      target_parent_locator: 'addp://engine/2/path/analytics?type=schema',
      target_name: 'fact_order'
    }
  })
})

test('DDL preview payload normalizes absent materialization without leaking undefined fields', () => {
  assert.deepEqual(buildDDLPreviewRequest(), {
    materialization: {}
  })
})

test('materialization payload keeps normalized partition design only when partition field is present', () => {
  assert.deepEqual(buildDDLPreviewRequest({
    target_parent_locator: 'addp://engine/2/path/analytics?type=schema',
    target_name: 'fact_order',
    partition_by: ' occurred_at ',
    partition_type: 'RANGE'
  }), {
    materialization: {
      target_parent_locator: 'addp://engine/2/path/analytics?type=schema',
      target_name: 'fact_order',
      partition_by: 'occurred_at',
      partition_type: 'range'
    }
  })
})
