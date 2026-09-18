// Only protocol adaptation lives here; CEL evaluation belongs to the backend.
export function trialInputs(rule, properties) {
  return rule.inputs.map((input) => {
    const property = properties.find((item) => item.id === input.property_id)
    if (!property) throw new Error('missing_property')
    return {
      ...input,
      property,
      state: 'unknown',
      value: property.kind === 'bool' ? false : ''
    }
  })
}

export function trialRequest(binding, inputs) {
  return {
    revision: binding.revision,
    generation: binding.generation,
    activation_version: binding.activation_version,
    inputs: Object.fromEntries(
      inputs.map(({ variable, state, value }) => [
        variable,
        state === 'known' ? { state, value } : { state }
      ])
    )
  }
}

export function sameBinding(left, right) {
  return [
    'ontology_id',
    'revision',
    'generation',
    'activation_version',
    'digest',
    'knowledge_kind'
  ].every((key) => left?.[key] !== undefined && left[key] === right?.[key])
}

export function trialOptions(property) {
  return property.enum.map((value) => ({
    value,
    labels: { 'zh-cn': value, en: value }
  }))
}

// Cache identity only, never an authorization decision or a token-derived identity.
export function trialOwner(authContext) {
  const { principal, context } = authContext || {}
  if (principal?.type !== 'user' || context?.type !== 'tenant') return ''
  const ids = [principal.id, context.tenant_id, context.tenant_membership_id]
  if (!ids.every((id) => typeof id === 'string' && /^[1-9][0-9]*$/.test(id)))
    return ''
  return JSON.stringify(ids)
}
