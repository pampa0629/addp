import assert from 'node:assert/strict'
import test from 'node:test'
import { buildComponentQuery, initialApplicationParameterValues, runtimeLayoutStyle } from '../src/utils/dataApplicationRuntime.mjs'

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

test('rejects a missing required application parameter and maps the twelve-column layout', () => {
  assert.throws(() => buildComponentQuery(snapshot, component, { minimum_amount: '' }), /missing required application parameter/)
  assert.deepEqual(runtimeLayoutStyle({ x: 2, y: 4, width: 6, height: 5 }), {
    gridColumn: '3 / span 6',
    gridRow: '5 / span 5',
  })
})
