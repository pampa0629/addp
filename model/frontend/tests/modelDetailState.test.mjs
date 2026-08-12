import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildDDLPreviewRequest,
  isEditableDraft,
  resolvePositiveRouteId
} from '../src/utils/modelDetailState.js'

test('detail route IDs must be positive integers', () => {
  assert.equal(resolvePositiveRouteId('2'), 2)
  assert.equal(resolvePositiveRouteId('0'), null)
  assert.equal(resolvePositiveRouteId('abc'), null)
})

test('only draft models with update permission are editable', () => {
  assert.equal(isEditableDraft('draft', true), true)
  assert.equal(isEditableDraft('approved', true), false)
  assert.equal(isEditableDraft('draft', false), false)
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
