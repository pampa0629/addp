import assert from 'node:assert/strict'
import test from 'node:test'
import { applicationRefreshDelayMilliseconds, buildComponentQuery, buildSelectionUpdate, canHideApplicationParameters, canRunApplicationRefresh, initialApplicationParameterValues, runtimeGridStyle, runtimeLayoutStyle, runtimeSectionVisible } from '../src/utils/dataApplicationRuntime.mjs'

const component = {
  id: 'component-a',
  query_template: {
    select: ['id', 'amount'],
    fixed_filter: { field: 'active', op: 'eq', value: true },
    parameter_filters: [
      { parameter_key: 'minimum', field: 'amount', operator: 'gte' },
      { parameter_key: 'missing', field: 'deleted_at', operator: 'is_null' },
    ],
    order_by: [{ field: 'id', direction: 'asc' }],
    page_limit: 50,
    format: 'json',
  },
}

const snapshot = {
  parameters: [
    { key: 'minimum_amount', label: 'Minimum', control_type: 'number', required: true, default_value: 10 },
    { key: 'missing_rows', label: 'Missing', control_type: 'checkbox', required: false, default_value: false },
  ],
  parameter_bindings: [
    { application_parameter_key: 'minimum_amount', component_id: 'component-a', component_parameter_key: 'minimum' },
    { application_parameter_key: 'missing_rows', component_id: 'component-a', component_parameter_key: 'missing' },
  ],
  selection_bindings: [{
    source_component_id: 'component-source',
    assignments: [{ source_field: 'amount', application_parameter_key: 'minimum_amount' }],
  }],
}

test('builds a component query only from explicit application parameter bindings', () => {
  const values = initialApplicationParameterValues(snapshot)
  assert.deepEqual(values, { minimum_amount: 10, missing_rows: false })
  assert.deepEqual(buildComponentQuery(snapshot, component, values, 'next-page-cursor'), {
    select: ['id', 'amount'],
    filter: { and: [
      { field: 'active', op: 'eq', value: true },
      { field: 'amount', op: 'gte', value: 10 },
    ] },
    order_by: [{ field: 'id', direction: 'asc' }],
    page: { limit: 50, cursor: 'next-page-cursor' },
    format: 'json',
  })
})

test('maps a result selection atomically to application parameters and deduplicated component targets', () => {
  const linkedSnapshot = structuredClone(snapshot)
  linkedSnapshot.parameter_bindings.push({ application_parameter_key: 'minimum_amount', component_id: 'component-b', component_parameter_key: 'minimum' })
  linkedSnapshot.parameter_bindings.push({ application_parameter_key: 'minimum_amount', component_id: 'component-a', component_parameter_key: 'other-minimum' })
  assert.deepEqual(buildSelectionUpdate(
    linkedSnapshot,
    'component-source',
    { output_contract: { fields: [{ name: 'amount', type: 'decimal' }] } },
    [{ amount: 12.5 }],
    { row_index: 0 },
  ), {
    parameter_values: { minimum_amount: 12.5 },
    component_ids: ['component-a', 'component-b'],
  })
})

test('ignores components without a selection binding and rejects invalid selected values', () => {
  assert.equal(buildSelectionUpdate(snapshot, 'component-a', {}, [], { row_index: 0 }), null)
  assert.throws(() => buildSelectionUpdate(
    snapshot,
    'component-source',
    { output_contract: { fields: [{ name: 'amount', type: 'decimal' }] } },
    [{ amount: '12.5' }],
    { row_index: 0 },
  ), /invalid selection value/)
})

test('rejects a missing required application parameter and maps the twelve-column layout', () => {
  assert.throws(() => buildComponentQuery(snapshot, component, { minimum_amount: '' }), /missing required application parameter/)
  assert.deepEqual(runtimeLayoutStyle({ x: 2, y: 4, width: 6, height: 5 }), {
    gridColumn: '3 / span 6',
    gridRow: '5 / span 5',
  })
})

test('fits every wallboard placement row into the current runtime grid', () => {
  const placements = [
    { x: 0, y: 0, width: 6, height: 4 },
    { x: 6, y: 0, width: 6, height: 4 },
    { x: 0, y: 4, width: 12, height: 3 },
  ]
  assert.deepEqual(runtimeGridStyle({ display_mode: 'desktop', placements }), {})
  assert.deepEqual(runtimeGridStyle({ display_mode: 'wallboard', placements }), {
    gridTemplateRows: 'repeat(7, minmax(0, 1fr))',
  })
})

test('runs only supported wallboard refresh intervals while visible and idle', () => {
  const wallboard = { display_mode: 'wallboard', refresh_interval_seconds: 60 }
  assert.equal(applicationRefreshDelayMilliseconds(wallboard), 60_000)
  assert.equal(canRunApplicationRefresh(wallboard), true)
  assert.equal(canRunApplicationRefresh(wallboard, { hidden: true }), false)
  assert.equal(canRunApplicationRefresh(wallboard, { querying: true }), false)
  assert.equal(applicationRefreshDelayMilliseconds({ display_mode: 'desktop', refresh_interval_seconds: 60 }), 0)
  assert.equal(applicationRefreshDelayMilliseconds({ display_mode: 'wallboard', refresh_interval_seconds: 10 }), 0)
  assert.equal(applicationRefreshDelayMilliseconds({ display_mode: 'wallboard', refresh_interval_seconds: 0 }), 0)
})

test('uses explicit runtime sections and only hides required parameters with executable defaults', () => {
  const page = { visible_sections: ['title', 'query_actions'] }
  assert.equal(runtimeSectionVisible(page, 'title'), true)
  assert.equal(runtimeSectionVisible(page, 'parameters'), false)
  assert.equal(runtimeSectionVisible({}, 'title'), false)
  const presentationSnapshot = structuredClone(snapshot)
  presentationSnapshot.components = [component]
  assert.equal(canHideApplicationParameters(presentationSnapshot), true)
  const missingDefault = structuredClone(presentationSnapshot)
  delete missingDefault.parameters[0].default_value
  assert.equal(canHideApplicationParameters(missingDefault), false)
  const nullOperatorDefault = structuredClone(presentationSnapshot)
  nullOperatorDefault.parameters[1].required = true
  assert.equal(canHideApplicationParameters(nullOperatorDefault), false)
  nullOperatorDefault.parameters[1].default_value = true
  assert.equal(canHideApplicationParameters(nullOperatorDefault), true)
})
