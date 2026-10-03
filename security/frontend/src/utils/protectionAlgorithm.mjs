export const prefixAlgorithm = 'addp.mask.keep_prefix_suffix/v2'
export const constantAlgorithm = 'addp.mask.constant/v1'
export const sm3Algorithm = 'addp.mask.sm3/v1'

export function initialProtectionConfiguration() {
  return { algorithm: prefixAlgorithm, parameters: { prefix_runes: 3, suffix_runes: 4, mask_rune: '*' }, allowed_algorithms: [prefixAlgorithm] }
}

export function algorithmParameters(key) {
  if (key === prefixAlgorithm) return { prefix_runes: 3, suffix_runes: 4, mask_rune: '*' }
  if (key === constantAlgorithm) return { value: '' }
  return {}
}

export function validateProtectionConfiguration(configuration, catalog, { baseline, fieldType, defaults = false } = {}) {
  const algorithm = catalog.find(item => item.key === configuration.algorithm)
  if (!algorithm || (fieldType && !algorithm.supported_field_types.includes(fieldType))) return false
  const parameters = configuration.parameters || {}
  if (configuration.algorithm === prefixAlgorithm) {
    if (Object.keys(parameters).length !== 3 || !Number.isSafeInteger(parameters.prefix_runes) || parameters.prefix_runes < 0
      || !Number.isSafeInteger(parameters.suffix_runes) || parameters.suffix_runes < 0 || typeof parameters.mask_rune !== 'string' || [...parameters.mask_rune].length !== 1) return false
    if (baseline && (baseline.algorithm !== prefixAlgorithm || parameters.prefix_runes > baseline.parameters.prefix_runes || parameters.suffix_runes > baseline.parameters.suffix_runes)) return false
  } else if (configuration.algorithm === constantAlgorithm) {
    const value = parameters.value
    if (Object.keys(parameters).length !== 1 || !['string', 'number', 'boolean'].includes(typeof value)
      || (typeof value === 'number' && !Number.isFinite(value))
      || (typeof value === 'string' && new TextEncoder().encode(value).length > 4096)) return false
    if (fieldType === 'string' && typeof value !== 'string') return false
    if (fieldType === 'bool' && typeof value !== 'boolean') return false
    if (['int', 'bigint'].includes(fieldType) && !Number.isSafeInteger(value)) return false
    if (['float', 'double'].includes(fieldType) && typeof value !== 'number') return false
  } else if (Object.keys(parameters).length) return false
  if (baseline && !baseline.allowed_algorithms?.includes(configuration.algorithm)) return false
  if (defaults) {
    const allowed = configuration.allowed_algorithms || []
    if (!allowed.includes(configuration.algorithm) || new Set(allowed).size !== allowed.length || allowed.some(key => !catalog.some(item => item.key === key))) return false
    if (configuration.algorithm !== prefixAlgorithm && allowed.includes(prefixAlgorithm)) return false
  }
  return true
}

export function algorithmNameKey(key) {
  return key === prefixAlgorithm ? 'security.algorithms.prefix.name' : key === constantAlgorithm ? 'security.algorithms.constant.name' : key === sm3Algorithm ? 'security.algorithms.sm3.name' : 'security.common.notAvailable'
}
