export function isStandardOperatorMetadata(operator) {
  if (!operator || typeof operator.name !== 'string' || operator.name.trim() === '') return false
  if (typeof operator.display_name !== 'string' || operator.display_name.trim() === '') return false
  if (typeof operator.description !== 'string') return false
  if (
    !Array.isArray(operator.category_path)
    || operator.category_path.length === 0
    || operator.category_path.some(item => typeof item !== 'string' || item.trim() === '')
  ) return false
  if (!Array.isArray(operator.public_parameters)) return false
  if (!Array.isArray(operator.output_ports) || operator.output_ports.length === 0) return false
  return true
}

export function findInvalidOperatorMetadata(operators) {
  if (!Array.isArray(operators)) return null
  return operators.find(operator => !isStandardOperatorMetadata(operator)) || null
}
