import assert from 'node:assert/strict'
import test from 'node:test'
import { buildComponentConfiguration, buildQueryRequest, buildRendererConfig, createNamedParameterDraft, createParameterDraft, hasParameterValue, requiredParameterValuesPresent, synchronizeFieldPresentations } from '../src/utils/componentDraft.mjs'

const descriptor = {
  ref: { service_type: 'query', service_id: 9 },
  contract_fingerprint: `sha256:${'a'.repeat(64)}`,
  input_contract: { order: { stable_key: ['id'] } },
}

test('compiles a reusable application component without service or domain field assumptions', () => {
  const draft = {
    name: 'component', description: '', columns: ['id', 'amount'], pageLimit: 50,
    rendererType: 'table',
    fieldPresentations: [
      { field: 'id', label: '订单编号', fieldType: 'string', unit: '', precision: null, temporalFormat: '', width: 160 },
      { field: 'amount', label: '金额', fieldType: 'decimal', unit: '元', precision: 2, temporalFormat: '', width: null },
    ],
    parameters: [{ key: 'minimum', label: 'Minimum', controlType: 'number', required: false, field: 'amount', operator: 'gte', fieldType: 'decimal', value: '12.5' }],
  }
  assert.deepEqual(buildQueryRequest(descriptor, draft, 'cursor-2', 'csv'), {
    parameters: {},
    select: ['id', 'amount'], filter: { field: 'amount', op: 'gte', value: 12.5 },
    order_by: [{ field: 'id', direction: 'asc' }], page: { limit: 50, cursor: 'cursor-2' }, format: 'csv',
  })
  const component = buildComponentConfiguration(descriptor, draft, 'component-a')
  assert.equal(component.id, 'component-a')
  assert.notEqual(component.service_ref, descriptor.ref)
  assert.equal(component.default_parameter_values.minimum, 12.5)
  assert.deepEqual(component.renderer_config, {
    columns: ['id', 'amount'],
    field_presentations: [
      { field: 'id', label: '订单编号', width: 160 },
      { field: 'amount', label: '金额', unit: '元', precision: 2 },
    ],
  })
})

test('persists a typed chart renderer without changing the service request contract', () => {
  const draft = {
    name: 'chart', description: '', columns: ['city', 'amount'], pageLimit: 20,
    rendererType: 'chart', chartType: 'bar', dimension: 'city', measures: ['amount'], parameters: [],
    fieldPresentations: [
      { field: 'city', label: '城市', fieldType: 'string', unit: '', precision: null, temporalFormat: '', width: null },
      { field: 'amount', label: '金额', fieldType: 'decimal', unit: '元', precision: 2, temporalFormat: '', width: null },
    ],
  }
  assert.deepEqual(buildComponentConfiguration(descriptor, draft, 'component-chart').renderer_config, {
    chart_type: 'bar', dimension: 'city', measures: ['amount'],
    field_presentations: [{ field: 'city', label: '城市' }, { field: 'amount', label: '金额', unit: '元', precision: 2 }],
  })
})

test('keeps one renderer-config compiler and synchronizes only fields used by the renderer', () => {
  const fields = [
    { name: 'city', type: 'string', comment: '城市' },
    { name: 'amount', type: 'decimal', comment: '金额' },
    { name: 'created_at', type: 'timestamp', comment: '创建时间' },
  ]
  const draft = {
    rendererType: 'chart', chartType: 'bar', dimension: 'city', measures: ['amount'],
    fieldPresentations: [{ field: 'amount', label: '实付金额', fieldType: 'decimal', unit: '元', precision: 2, temporalFormat: '', width: null }],
  }
  draft.fieldPresentations = synchronizeFieldPresentations(draft, fields)

  assert.deepEqual(draft.fieldPresentations.map((item) => item.field), ['city', 'amount'])
  assert.equal(draft.fieldPresentations[0].label, '城市')
  assert.equal(draft.fieldPresentations[1].label, '实付金额')
  assert.deepEqual(buildRendererConfig(draft).field_presentations, [
    { field: 'city', label: '城市' },
    { field: 'amount', label: '实付金额', unit: '元', precision: 2 },
  ])
})

test('persists explicitly configured scalar values without domain field assumptions', () => {
  const draft = {
    name: 'summary', description: '', columns: ['amount'], pageLimit: 1,
    rendererType: 'value', valueItems: [{ field: 'amount', label: 'Total', unit: 'items', precision: 2 }], parameters: [],
  }
  assert.deepEqual(buildComponentConfiguration(descriptor, draft, 'component-value').renderer_config, {
    items: [{ field: 'amount', label: 'Total', unit: 'items', precision: 2 }],
  })
})

test('persists explicit map labels and controlled thematic style without raw colors', () => {
  const draft = {
    name: 'map', description: '', columns: ['id', 'amount', 'shape'], pageLimit: 100,
    rendererType: 'map', geometryField: 'shape', mapLabelField: 'id', tooltipFields: ['amount'],
    mapStyleMode: 'continuous', mapColorField: 'amount', mapPalette: 'primary', mapLegendTitle: 'Amount', parameters: [],
  }
  assert.deepEqual(buildComponentConfiguration(descriptor, draft, 'component-map').renderer_config, {
    geometry_field: 'shape', label_field: 'id', tooltip_fields: ['amount'],
    style: { mode: 'continuous', field: 'amount', palette: 'primary', legend_title: 'Amount' },
  })
})

test('compiles descriptor operators with their typed runtime values', () => {
  const draft = {
    name: 'filters', description: '', columns: ['id'], pageLimit: 25, rendererType: 'table',
    parameters: [
      { key: 'statuses', label: 'Statuses', controlType: 'multiselect', required: false, field: 'status', operator: 'in', fieldType: 'string', value: ['paid', 'shipped'] },
      { key: 'missing', label: 'Missing', controlType: 'checkbox', required: false, field: 'shipped_at', operator: 'is_null', fieldType: 'timestamp', value: true },
      { key: 'bounds', label: 'Bounds', controlType: 'bbox', required: false, field: 'shape', operator: 'bbox_intersects', fieldType: 'geometry', value: ['100', '20', '110', '30'] },
    ],
  }
  assert.deepEqual(buildQueryRequest(descriptor, draft).filter, { and: [
    { field: 'status', op: 'in', value: ['paid', 'shipped'] },
    { field: 'shipped_at', op: 'is_null' },
    { field: 'shape', op: 'bbox_intersects', value: [100, 20, 110, 30] },
  ] })
  const defaults = buildComponentConfiguration(descriptor, draft, 'component-filters').default_parameter_values
  assert.deepEqual(defaults, { statuses: ['paid', 'shipped'], missing: true, bounds: [100, 20, 110, 30] })
  assert.equal(hasParameterValue({ operator: 'bbox_intersects', value: ['', 20, 110, 30] }), false)
  assert.equal(hasParameterValue({ operator: 'is_null', value: false }), false)
})

test('keeps an optional boolean parameter unset until the user chooses true or false', () => {
  const draft = {
    name: 'boolean', description: '', columns: ['id'], pageLimit: 25, rendererType: 'table',
    parameters: [{ key: 'active', label: 'Active', controlType: 'select', required: false, field: 'active', operator: 'eq', fieldType: 'bool', value: '' }],
  }
  assert.equal(buildQueryRequest(descriptor, draft).filter, null)
  draft.parameters[0].value = false
  assert.deepEqual(buildQueryRequest(descriptor, draft).filter, { field: 'active', op: 'eq', value: false })
  assert.deepEqual(buildComponentConfiguration(descriptor, draft, 'component-boolean').default_parameter_values, { active: false })
})

test('creates a parameter only from a descriptor field with an executable operator', () => {
  assert.deepEqual(createParameterDraft({ name: 'person_id', type: 'string', operators: ['eq', 'in'] }, 2), {
    key: 'parameter_3', label: 'person_id', controlType: 'text', required: false,
    bindingKind: 'filter',
    field: 'person_id', operator: 'eq', fieldType: 'string', value: '',
  })
  assert.equal(createParameterDraft({ name: 'opaque', type: 'string', operators: [] }), null)
  assert.equal(createParameterDraft({ name: 'broken', type: 'string', operators: null }), null)
})

test('compiles service named parameters into the same structured request and component snapshot', () => {
  const named = createNamedParameterDraft({ name: 'person_id_a', type: 'string', required: true, description: 'First person' })
  named.value = 'person-1'
  const draft = {
    name: 'overlap', description: '', columns: ['overlap_count'], pageLimit: 1, rendererType: 'table',
    parameters: [named],
  }
  assert.deepEqual(buildQueryRequest(descriptor, draft), {
    parameters: { person_id_a: 'person-1' },
    select: ['overlap_count'], filter: null,
    order_by: [{ field: 'id', direction: 'asc' }], page: { limit: 1, cursor: '' }, format: 'json',
  })
  const component = buildComponentConfiguration(descriptor, draft, 'overlap-component')
  assert.deepEqual(component.query_template.parameter_filters, [])
  assert.deepEqual(component.query_template.named_parameter_bindings, [{ parameter_key: 'person_id_a', name: 'person_id_a' }])
  assert.equal(component.default_parameter_values.person_id_a, 'person-1')
})

test('keeps required parameters without component defaults saveable but not previewable', () => {
  const named = createNamedParameterDraft({ name: 'person_id', type: 'string', required: true, description: 'Person' })
  const draft = {
    name: 'runtime-bound', description: '', columns: ['metric'], pageLimit: 50, rendererType: 'table',
    parameters: [named],
  }

  assert.equal(requiredParameterValuesPresent(draft.parameters), false)
  assert.deepEqual(buildComponentConfiguration(descriptor, draft, 'runtime-bound').default_parameter_values, {})

  named.value = 'person-1'
  assert.equal(requiredParameterValuesPresent(draft.parameters), true)
})
