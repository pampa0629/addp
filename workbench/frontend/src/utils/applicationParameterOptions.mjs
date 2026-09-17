import { intersectParameterOptions, parameterOptionsAllow, validParameterOptions } from '../../../../common-frontend/basic/src/utils/parameterInput.mjs'
import { compatibleSelectionParameters, selectionSourceFields } from './dataApplicationSelection.mjs'

export function newComponentParameterContext(snapshot, descriptors, component, descriptor, reusedParameters = {}) {
  return {
    snapshot: { ...snapshot, components: [...snapshot.components, component], parameter_bindings: [
      ...snapshot.parameter_bindings,
      ...component.parameter_definitions.map(parameter => ({ component_id: component.id, component_parameter_key: parameter.key, application_parameter_key: reusedParameters[parameter.key] || '' })),
    ] },
    descriptors: { ...descriptors, [component.id]: descriptor },
  }
}

// Evaluate an explicit editor mapping against the same domains used at runtime.
// Do not silently strand a selection target or change a saved default/preset.
export function canBindApplicationParameter(snapshot, descriptors, binding, parameter) {
  const component = snapshot.components.find(c => c.id === binding.component_id)
  const definition = component?.parameter_definitions?.find(p => p.key === binding.component_parameter_key)
  if (definition?.control_type !== parameter.control_type) return false
  const next = { ...snapshot, parameter_bindings: snapshot.parameter_bindings.map(b =>
    b.component_id === binding.component_id && b.component_parameter_key === binding.component_parameter_key
      ? { ...b, application_parameter_key: parameter.key } : b) }
  try {
    assertApplicationOptionValues(next, descriptors, { [parameter.key]: parameter.default_value })
    for (const preset of next.parameter_presets || []) assertApplicationOptionValues(next, descriptors, preset.parameter_values, [parameter.key])
    for (const selection of next.selection_bindings || []) {
      const fields = selectionSourceFields(next, selection.source_component_id, descriptors[selection.source_component_id])
      for (const assignment of selection.assignments) {
        if (![binding.application_parameter_key, parameter.key].includes(assignment.application_parameter_key)) continue
        if (!compatibleSelectionParameters(next, descriptors, fields.find(f => f.name === assignment.source_field))
          .some(p => p.key === assignment.application_parameter_key)) return false
      }
    }
    return true
  } catch { return false }
}

export function applicationParameterOptions(snapshot, descriptors, key) {
  let options = [], type = ''
  const bindings = (snapshot?.parameter_bindings || []).filter((b) => b.application_parameter_key === key)
  const components = []
  try {
    if (!bindings.length) throw new Error('missing-binding')
    for (const binding of bindings) {
      const component = snapshot.components.find((c) => c.id === binding.component_id)
      components.push(component?.title || binding.component_id)
      const descriptor = descriptors[binding.component_id]
      if (!descriptor || descriptor.contract_fingerprint !== component?.contract_fingerprint) throw new Error('descriptor-unavailable')
      const named = component.query_template.named_parameter_bindings?.find((b) => b.parameter_key === binding.component_parameter_key)
      const filter = component.query_template.parameter_filters?.find((b) => b.parameter_key === binding.component_parameter_key)
      const target = named ? descriptor.input_contract.named_parameters?.find((p) => p.name === named.name)
        : descriptor.input_contract.fields?.find((f) => f.name === filter?.field)
      if (!target || (type && type !== target.type)) throw new Error('parameter-type-conflict')
      type = target.type
      const candidate = named ? target.options || [] : []
      if (!validParameterOptions(candidate, type)) throw new Error('invalid-options')
      options = intersectParameterOptions(options, candidate)
    }
    return { options, type, ready: true, error: '', components }
  } catch (error) {
    return { options: [], type, ready: false, error: error.message, components }
  }
}

export function assertApplicationOptionValues(snapshot, descriptors, values, keys = Object.keys(values || {})) {
  for (const key of keys) {
    const domain = applicationParameterOptions(snapshot, descriptors, key)
    if (!domain.ready) throw new Error(`parameter-options: ${key}: ${domain.error}`)
    const value = values?.[key]
    if (value !== '' && value !== null && value !== undefined && !parameterOptionsAllow(domain.options, value)) throw new Error(`parameter-options: ${key}: invalid-value`)
  }
}
