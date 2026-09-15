import { intersectParameterOptions, parameterOptionsAllow, validParameterOptions } from '../../../../common-frontend/basic/src/utils/parameterInput.mjs'

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
