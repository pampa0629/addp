import assert from 'node:assert/strict'
import test from 'node:test'
import { applyApplicationParameterDefaults, configureSelectionListDraft, buildComponentConfiguration, buildQueryRequest, buildRendererConfig, componentDisplaySuggestions, createNamedParameterDraft, createParameterDraft, draftFromComponent, hasParameterValue, requiredParameterValuesPresent, synchronizeFieldPresentations } from '../src/utils/componentDraft.mjs'

const descriptor = {
  ref: { service_type: 'query', service_id: 9 },
  contract_fingerprint: `sha256:${'a'.repeat(64)}`,
  input_contract: { order: { stable_key: ['id'] } },
}

test('existing trial values follow application bindings without persisting temporary values or falling back from empty defaults', () => {
  const source = { ...descriptor, input_contract: {
    fields: [{ name: 'tags', type: 'string', operators: ['in'] }],
    named_parameters: [{ name: 'threshold', type: 'int', required: true }, { name: 'enabled', type: 'bool', required: true }],
    page: { default_limit: 20 }, order: { stable_key: ['id'] },
  }, output_contract: { fields: [{ name: 'id', type: 'string' }] } }
  const originalDraft = { name: 'Source', description: '', columns: ['id'], pageLimit: 20, rendererType: 'table', parameters: [
    { key: 'number', label: 'Count', bindingKind: 'named', name: 'threshold', fieldType: 'int', controlType: 'number', operator: 'eq', required: true, value: 88 },
    { key: 'flag', label: 'Enabled', bindingKind: 'named', name: 'enabled', fieldType: 'bool', controlType: 'select', operator: 'eq', required: true, value: true },
    { key: 'tags', label: 'Tags', bindingKind: 'filter', field: 'tags', fieldType: 'string', controlType: 'multiselect', operator: 'in', required: false, value: ['history'] },
  ] }
  const component = buildComponentConfiguration(source, originalDraft, 'target')
  const snapshot = { parameters: [
    { key: 'shared-number', control_type: 'number', default_value: 0 },
    { key: 'shared-flag', control_type: 'select', default_value: false },
    { key: 'shared-tags', control_type: 'multiselect', default_value: ['current'] },
  ], parameter_bindings: [
    { component_id: 'unrelated', component_parameter_key: 'number', application_parameter_key: 'shared-flag' },
    ...['number', 'flag', 'tags'].map(key => ({ component_id: 'target', component_parameter_key: key, application_parameter_key: `shared-${key}` })),
  ] }
  const draft = draftFromComponent(component, source)
  applyApplicationParameterDefaults(draft, snapshot, component.id)
  assert.deepEqual(draft.parameters.map(p => p.value), [0, false, ['current']])
  assert.deepEqual(buildQueryRequest(source, draft).parameters, { threshold: 0, enabled: false })
  assert.deepEqual(buildQueryRequest(source, draft).filter, { field: 'tags', op: 'in', value: ['current'] })
  draft.parameters[1].options = [{ value: true, labels: { 'zh-cn': '启用', en: 'Enabled' } }]
  assert.equal(requiredParameterValuesPresent(draft.parameters), false)
  assert.throws(() => buildQueryRequest(source, draft), /invalid-value/)
  draft.parameters[1].options = []
  draft.parameters[0].value = 12
  draft.parameters[2].value.push('trial')
  assert.deepEqual(snapshot.parameters.map(p => p.default_value), [0, false, ['current']])
  assert.deepEqual(buildComponentConfiguration(source, draft, component.id, component).default_parameter_values, component.default_parameter_values)
  assert.deepEqual(buildComponentConfiguration(source, draft, 'new').default_parameter_values, { number: 12, flag: false, tags: ['current', 'trial'] })

  delete snapshot.parameters[0].default_value
  snapshot.parameters[2].default_value = []
  applyApplicationParameterDefaults(draft, snapshot, component.id)
  assert.deepEqual(draft.parameters.map(p => p.value), [null, false, []])
  assert.equal(requiredParameterValuesPresent(draft.parameters), false)
  assert.equal(buildQueryRequest(source, draft).filter, null)

  draft.parameters[0].key = 'renamed'
  draft.parameters[0].value = 7
  draft.parameters[2].operator = 'eq'
  draft.parameters[2].value = 'changed'
  assert.deepEqual(buildComponentConfiguration(source, draft, component.id, component).default_parameter_values, { renamed: 7, flag: true, tags: 'changed' })
  const differentSource = { ...source, ref: { ...source.ref, service_id: 10 } }
  assert.equal(buildComponentConfiguration(differentSource, draft, component.id, component).default_parameter_values.flag, false)
})

test('editing or duplicating preserves fixed predicates and explicit ordering in trial and saved queries', () => {
  const source = { ...descriptor, output_contract: { fields: [{ name: 'id', type: 'string' }] } }
  const component = {
    title: 'Filtered', description: '', renderer_type: 'table',
    query_template: { select: ['id'], page_limit: 20, fixed_filter: { field: 'id', op: 'neq', value: 'excluded' }, order_by: [{ field: 'id', direction: 'desc' }] },
    renderer_config: { columns: ['id'] },
  }
  const draft = draftFromComponent(component, source)
  const request = buildQueryRequest(source, draft)
  const saved = buildComponentConfiguration(source, draft, 'copy')
  assert.deepEqual(request.filter, component.query_template.fixed_filter)
  assert.deepEqual(saved.query_template.fixed_filter, request.filter)
  assert.deepEqual(request.order_by, component.query_template.order_by)
  assert.deepEqual(saved.query_template.order_by, request.order_by)
  saved.query_template.fixed_filter.value = 'changed'
  saved.query_template.order_by[0].direction = 'asc'
  assert.equal(component.query_template.fixed_filter.value, 'excluded')
  assert.equal(component.query_template.order_by[0].direction, 'desc')
})

test('value labels round trip through the sole renderer compiler without altering query values', () => {
  const source = { ...descriptor, output_contract: { fields: [{ name: 'direction', type: 'string' }] } }
  const component = {
    title: 'Direction', renderer_type: 'table', query_template: { select: ['direction'], page_limit: 20 },
    renderer_config: { columns: ['direction'], field_presentations: [{ field: 'direction', label: 'Direction', value_labels: [{ value: 'forward', label: 'A → B' }] }] },
  }
  const draft = draftFromComponent(component, source)
  assert.deepEqual(buildRendererConfig(draft), component.renderer_config)
  assert.deepEqual(buildQueryRequest(source, draft).select, ['direction'])
  draft.fieldPresentations[0].valueLabels[0].label = 'Changed'
  assert.equal(component.renderer_config.field_presentations[0].value_labels[0].label, 'A → B')
})

test('compiles a reusable application component without service or domain field assumptions', () => {
  const draft = {
    name: 'component', description: '', columns: ['id', 'amount'], pageLimit: 50,
    rendererType: 'table',
    fieldPresentations: [
      { field: 'id', label: '订单编号', fieldType: 'string', unit: '', precision: null, temporalFormat: '', width: 160 },
      { field: 'amount', label: '金额', fieldType: 'decimal', unit: '元', precision: 2, temporalFormat: '', width: null, stateRules: [{ operator: 'gt', operand: 100, label: '高额', tone: 'warning' }] },
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
      { field: 'amount', label: '金额', unit: '元', precision: 2, state_rules: [{ operator: 'gt', operand: 100, label: '高额', tone: 'warning' }] },
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

test('persists explicitly configured scalar values and state rules without domain field assumptions', () => {
  const draft = {
    name: 'summary', description: '', columns: ['amount'], pageLimit: 1,
    rendererType: 'value', valueItems: [{ field: 'amount', label: 'Total', unit: 'items', precision: 2, stateRules: [{ operator: 'gte', operand: 100, label: 'Target', tone: 'success' }] }], parameters: [],
  }
  assert.deepEqual(buildComponentConfiguration(descriptor, draft, 'component-value').renderer_config, {
    items: [{ field: 'amount', label: 'Total', unit: 'items', precision: 2, state_rules: [{ operator: 'gte', operand: 100, label: 'Target', tone: 'success' }] }],
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

test('text contains follows descriptor and binds a literal without wildcard rewriting', () => {
  const filter = createParameterDraft({ name: 'nickname', type: 'string', operators: ['contains'] })
  assert.equal(filter.controlType, 'text')
  filter.value = "苏%_\\'"
  const draft = { columns: ['id'], pageLimit: 50, parameters: [filter] }
  const request = buildQueryRequest(descriptor, draft)
  assert.deepEqual(request.filter, { field: 'nickname', op: 'contains', value: "苏%_\\'" })
  filter.value = ''
  assert.equal(buildQueryRequest(descriptor, draft).filter, null)
  assert.equal(createParameterDraft({ name: 'nickname', type: 'string', operators: [] }), null)
})


test('display suggestions use selectable output facts, default order, stable time and explicit geometry', () => {
  const fields = [
    { name: 'opaque_count', type: 'int' }, { name: 'category_x', type: 'string' },
    { name: 'when_x', type: 'date' }, { name: 'amount_x', type: 'decimal' },
    { name: 'shape_x', type: 'geometry' }, { name: 'private_x', type: 'string' },
  ]
  const source = {
    input_contract: { fields: fields.map(f => ({ ...f, selectable: f.name !== 'private_x' })), default_selection: ['amount_x', 'category_x', 'when_x'], order: { stable_key: ['when_x'] } },
    output_contract: { fields, spatial: { primary_geometry_field: 'shape_x' } },
  }
  const before = structuredClone(source)
  const suggestions = componentDisplaySuggestions(source)
  assert.deepEqual(suggestions.map(s => s.key), ['table', 'bar', 'line', 'map'])
  assert.deepEqual(suggestions.find(s => s.key === 'bar').columns, ['category_x', 'amount_x'])
  assert.deepEqual(suggestions.find(s => s.key === 'line').columns, ['when_x', 'amount_x'])
  assert.deepEqual(suggestions.find(s => s.key === 'map').columns, ['shape_x'])
  assert.deepEqual(suggestions[0].columns, source.input_contract.default_selection)
  assert.deepEqual(source, before)
  suggestions[0].columns.push('changed')
  assert.deepEqual(source, before)
})

test('display suggestions never infer aggregation, time roles from names or undeclared geometry', () => {
  const fields = [{ name: 'date', type: 'int' }, { name: 'total', type: 'double' }, { name: 'geom', type: 'string' }]
  const source = { input_contract: { fields: fields.map(f => ({ ...f, selectable: true })), order: { stable_key: ['date'] } }, output_contract: { fields } }
  assert.deepEqual(componentDisplaySuggestions(source).map(s => s.key), ['table', 'bar'])
  source.output_contract.fields = fields.slice(0, 2)
  assert.deepEqual(componentDisplaySuggestions(source).map(s => s.key), ['table'])
  source.output_contract.fields = [{ name: 'date', type: 'date' }, { name: 'total', type: 'double' }]
  source.input_contract.order.stable_key = ['total']
  assert.deepEqual(componentDisplaySuggestions(source).map(s => s.key), ['table'])
  source.input_contract.fields = []
  assert.deepEqual(componentDisplaySuggestions(source), [])
  assert.deepEqual(componentDisplaySuggestions(null), [])
})


test('search-list composition preserves named inputs and compiles literal contains through the existing query path', () => {
  const fields = [{ name: 'code', type: 'string', selectable: true }, { name: 'caption', type: 'string', selectable: true, filterable: true, operators: ['eq', 'contains'] }]
  const source = { ...descriptor, input_contract: { ...descriptor.input_contract, fields }, output_contract: { fields } }
  const named = { key: 'parameter_2', name: 'scope', label: 'Scope', bindingKind: 'named', fieldType: 'string', required: true, value: 'current' }
  const draft = { name: 'Selection', description: '', pageLimit: 10, parameters: [named], fieldPresentations: [] }
  configureSelectionListDraft(draft, source, { searchField: 'caption', labelField: 'caption', valueField: 'code', searchLabel: 'Find' })
  assert.equal(draft.parameters[0], named)
  assert.equal(new Set(draft.parameters.map(p => p.key)).size, 2)
  draft.parameters[1].value = 'A_%'
  const query = buildQueryRequest(source, draft)
  assert.deepEqual(query.parameters, { scope: 'current' })
  assert.deepEqual(query.filter, { field: 'caption', op: 'contains', value: 'A_%' })
  const component = buildComponentConfiguration(source, draft, 'picker')
  assert.deepEqual(component.query_template.select, ['caption', 'code'])
  assert.deepEqual(component.renderer_config.columns, ['caption', 'code'])
  assert.equal(component.query_template.parameter_filters[0].operator, 'contains')
  configureSelectionListDraft(draft, source, { searchField: '', labelField: 'code', valueField: 'code', searchLabel: 'Find' })
  assert.deepEqual(draft.columns, ['code'])
  assert.deepEqual(draft.parameters, [named])
})
