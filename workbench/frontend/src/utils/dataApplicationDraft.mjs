export function normalizedApplicationSnapshot(snapshot) {
  const normalized = JSON.parse(JSON.stringify(snapshot))
  const used = new Set(normalized.parameter_bindings.map((binding) => binding.application_parameter_key))
  normalized.parameters = normalized.parameters.filter((parameter) => used.has(parameter.key))
  return normalized
}

export async function confirmDataApplicationAction(confirm, message) {
  try {
    await confirm(message)
    return true
  } catch (error) {
    if (error === 'cancel' || error === 'close') return false
    throw error
  }
}
