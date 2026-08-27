import assert from 'node:assert/strict'
import test from 'node:test'
import { buildQueryRequest, buildViewPayload, hasParameterValue } from '../src/utils/viewDraft.mjs'

const descriptor = { ref: { service_type: 'query', service_id: 9 }, input_contract: { order: { stable_key: ['id'] } } }

test('compiles dynamic parameters without service or domain field assumptions', () => {
  const draft = {
    name: 'v', description: '', columns: ['id', 'amount'], pageLimit: 50,
    rendererType: 'table',
    parameters: [{ key: 'minimum', label: 'Minimum', controlType: 'number', required: false, field: 'amount', operator: 'gte', fieldType: 'decimal', value: '12.5' }]
  }
  assert.deepEqual(buildQueryRequest(descriptor, draft, 'cursor-2', 'csv'), {
    select: ['id', 'amount'], filter: { field: 'amount', op: 'gte', value: 12.5 },
    order_by: [{ field: 'id', direction: 'asc' }], page: { limit: 50, cursor: 'cursor-2' }, format: 'csv'
  })
  const payload = buildViewPayload(descriptor, draft)
  assert.equal(payload.default_parameter_values.minimum, 12.5)
  assert.deepEqual(payload.renderer_config, { columns: ['id', 'amount'] })
})

test('persists a typed chart renderer without changing the service request contract', () => {
  const draft = {
    name: 'chart', description: '', columns: ['city', 'amount'], pageLimit: 20,
    rendererType: 'chart', chartType: 'bar', dimension: 'city', measures: ['amount'], parameters: []
  }
  assert.deepEqual(buildViewPayload(descriptor, draft).renderer_config, {
    chart_type: 'bar', dimension: 'city', measures: ['amount']
  })
})

test('compiles descriptor operators with their typed runtime values', () => {
  const draft = {
    name: 'filters', description: '', columns: ['id'], pageLimit: 25, rendererType: 'table',
    parameters: [
      { key: 'statuses', label: 'Statuses', controlType: 'multiselect', required: false, field: 'status', operator: 'in', fieldType: 'string', value: ['paid', 'shipped'] },
      { key: 'missing', label: 'Missing', controlType: 'checkbox', required: false, field: 'shipped_at', operator: 'is_null', fieldType: 'timestamp', value: true },
      { key: 'bounds', label: 'Bounds', controlType: 'bbox', required: false, field: 'shape', operator: 'bbox_intersects', fieldType: 'geometry', value: ['100', '20', '110', '30'] }
    ]
  }
  assert.deepEqual(buildQueryRequest(descriptor, draft).filter, { and: [
    { field: 'status', op: 'in', value: ['paid', 'shipped'] },
    { field: 'shipped_at', op: 'is_null' },
    { field: 'shape', op: 'bbox_intersects', value: [100, 20, 110, 30] }
  ] })
  const defaults = buildViewPayload(descriptor, draft).default_parameter_values
  assert.deepEqual(defaults, { statuses: ['paid', 'shipped'], missing: true, bounds: [100, 20, 110, 30] })
  assert.equal(hasParameterValue({ operator: 'bbox_intersects', value: ['', 20, 110, 30] }), false)
  assert.equal(hasParameterValue({ operator: 'is_null', value: false }), false)
})

test('keeps an optional boolean parameter unset until the user chooses true or false', () => {
  const draft = {
    name: 'boolean', description: '', columns: ['id'], pageLimit: 25, rendererType: 'table',
    parameters: [
      { key: 'active', label: 'Active', controlType: 'select', required: false, field: 'active', operator: 'eq', fieldType: 'bool', value: '' }
    ]
  }
  assert.equal(buildQueryRequest(descriptor, draft).filter, null)
  draft.parameters[0].value = false
  assert.deepEqual(buildQueryRequest(descriptor, draft).filter, { field: 'active', op: 'eq', value: false })
  assert.deepEqual(buildViewPayload(descriptor, draft).default_parameter_values, { active: false })
})
