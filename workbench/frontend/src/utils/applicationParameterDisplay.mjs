import { buildComponentQuery } from './dataApplicationRuntime.mjs'
import { assertQueryOperation } from './serviceOperation.mjs'

export function parameterDisplayFields(snapshot, descriptors, parameterKey, sourceID) {
  const component = snapshot.components.find(c => c.id === sourceID)
  const descriptor = descriptors[sourceID]
  if (!component || !descriptor || descriptor.contract_fingerprint !== component.contract_fingerprint) return []
  const assignments = (snapshot.selection_bindings || []).filter(b => b.source_component_id === sourceID)
    .flatMap(b => b.assignments.filter(a => a.application_parameter_key === parameterKey))
  if (assignments.length !== 1) return []
  const selected = new Set(component.query_template.select)
  const valueField = assignments[0].source_field
  const input = descriptor.input_contract.fields.find(f => f.name === valueField)
  if (!selected.has(valueField) || !input?.selectable || !input.filterable || !input.operators?.includes('eq')) return []
  return descriptor.output_contract.fields.filter(f => selected.has(f.name) && f.type === 'string'
    && descriptor.input_contract.fields.some(input => input.name === f.name && input.type === 'string' && input.selectable))
}

export function assertParameterDisplaySources(snapshot, descriptors) {
  for (const parameter of snapshot.parameters) {
    const display = parameter.display_source
    if (display && !parameterDisplayFields(snapshot, descriptors, parameter.key, display.source_component_id).some(f => f.name === display.label_field)) throw new Error('invalid-display-source')
  }
}

export function buildParameterDisplayQuery(snapshot, descriptors, parameterKey, value, values) {
  const parameter = snapshot.parameters.find(p => p.key === parameterKey)
  const display = parameter?.display_source
  if (!display || value === null || value === undefined || value === '') return null
  if (!parameterDisplayFields(snapshot, descriptors, parameterKey, display.source_component_id).some(f => f.name === display.label_field)) throw new Error('invalid-display-source')
  const component = snapshot.components.find(c => c.id === display.source_component_id)
  const descriptor = descriptors[component.id]
  const assignment = snapshot.selection_bindings.find(b => b.source_component_id === component.id).assignments.find(a => a.application_parameter_key === parameterKey)
  const body = buildComponentQuery(snapshot, { ...component, query_template: {
    ...component.query_template, fixed_filter: component.query_template.fixed_filter ? JSON.parse(JSON.stringify(component.query_template.fixed_filter)) : null, parameter_filters: [], select: [...new Set([assignment.source_field, display.label_field])],
    page_limit: Math.min(2, descriptor.input_contract.page.max_limit),
  } }, JSON.parse(JSON.stringify(values)), '', 'json')
  const identityFilter = { field: assignment.source_field, op: 'eq', value }
  body.filter = body.filter ? { and: [body.filter, identityFilter] } : identityFilter
  return { operation: assertQueryOperation(descriptor.operations.find(op => op.key === 'query')), body,
    fingerprint: component.contract_fingerprint, valueField: assignment.source_field, labelField: display.label_field, value }
}

export function parameterDisplayResult(query, response) {
  const rows = response?.data
  if (!Array.isArray(rows) || !response.page || typeof response.page.has_more !== 'boolean') return { status: 'failed', label: '' }
  if (response.page.has_more || rows.length > 1) return { status: 'ambiguous', label: '' }
  if (!rows.length) return { status: 'missing', label: '' }
  if (rows[0]?.[query.valueField] !== query.value) return { status: 'failed', label: '' }
  const label = rows[0]?.[query.labelField]
  return typeof label === 'string' && label.trim() ? { status: 'resolved', label: label.trim() } : { status: 'unnamed', label: '' }
}
