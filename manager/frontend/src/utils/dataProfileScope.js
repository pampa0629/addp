const NUMERIC_TYPES = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const TEMPORAL_TYPES = new Set(['date', 'time', 'timestamp'])

const COMMON_OPERATORS = ['eq', 'ne', 'is_null', 'is_not_null']

export const operatorsForProfileField = field => {
  const type = String(field?.type || 'unknown')
  if (NUMERIC_TYPES.has(type) || TEMPORAL_TYPES.has(type)) {
    return [...COMMON_OPERATORS, 'gt', 'gte', 'lt', 'lte', 'between', 'in', 'not_in']
  }
  if (type === 'string') {
    return [...COMMON_OPERATORS, 'contains', 'starts_with', 'in', 'not_in']
  }
  if (type === 'uuid' || type === 'bool') {
    return [...COMMON_OPERATORS, 'in', 'not_in']
  }
  if (type === 'unknown' || type === 'mixed') return COMMON_OPERATORS
  return ['is_null', 'is_not_null']
}

export const newProfileCondition = fields => {
  const selected = fields?.[0]
  const operator = operatorsForProfileField(selected)[0] || 'is_null'
  const value = selected?.type === 'bool' ? true : (NUMERIC_TYPES.has(selected?.type) ? null : '')
  return { field: selected?.name || '', operator, value, values: [] }
}

export const buildProfileDataScope = (fields, logic, conditions) => {
  if (!['and', 'or'].includes(logic)) throw new Error('invalid_logic')
  if (!Array.isArray(conditions) || conditions.length === 0 || conditions.length > 8) {
    throw new Error('invalid_condition_count')
  }
  const fieldsByName = new Map((fields || []).map(field => [field.name, field]))
  return {
    kind: 'condition',
    logic,
    conditions: conditions.map(condition => {
      const field = fieldsByName.get(condition.field)
      if (!field) throw new Error('missing_field')
      const operators = operatorsForProfileField(field)
      if (!operators.includes(condition.operator)) throw new Error('invalid_operator')
      const result = { field: condition.field, operator: condition.operator }
      if (['is_null', 'is_not_null'].includes(condition.operator)) return result
      if (['between', 'in', 'not_in'].includes(condition.operator)) {
        const sourceValues = condition.values || []
        if (sourceValues.some(value => value === '' || value == null) ||
          (condition.operator === 'between' && sourceValues.length !== 2) ||
          (condition.operator !== 'between' && sourceValues.length === 0)) {
          throw new Error('missing_value')
        }
        const values = sourceValues.map(value => typedValue(value, field.type))
        result.values = values
        return result
      }
      if (condition.value === '' || condition.value == null) throw new Error('missing_value')
      result.value = typedValue(condition.value, field.type)
      return result
    })
  }
}

const typedValue = (value, type) => {
  if (NUMERIC_TYPES.has(type)) {
    const number = Number(value)
    if (!Number.isFinite(number)) throw new Error('invalid_number')
    return number
  }
  if (type === 'bool') {
    if (value !== true && value !== false) throw new Error('invalid_boolean')
    return value
  }
  return String(value)
}

export const isNumericProfileType = type => NUMERIC_TYPES.has(type)
export const isTemporalProfileType = type => TEMPORAL_TYPES.has(type)
