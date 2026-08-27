export const MATERIALIZATION_GATE_SCHEMA_VERSION = 'addp.quality.materialization-gate/v1'
export const MATERIALIZATION_GATE_TYPES = ['not_null', 'unique_key', 'foreign_key', 'predicate_implication', 'row_count']
export const MATERIALIZATION_GATE_OPERATORS = ['eq', 'not_eq', 'is_null', 'is_not_null', 'is_true', 'is_false']

export const bindingAlias = (code, fallback) => {
  const normalized = String(code || '').trim().toLowerCase().replace(/[^a-z0-9_]/g, '_').replace(/_+/g, '_').replace(/^_+|_+$/g, '')
  if (/^[a-z]/.test(normalized)) return normalized
  return `table_${fallback}`
}

const blankCondition = () => ({ column: '', operator: 'eq', value: '', value_type: 'string' })

export const createMaterializationGateAssertion = (type = 'not_null', uuid = crypto.randomUUID()) => ({
  assertion_key: uuid,
  type,
  severity: 'error',
  params: type === 'not_null' ? { table: '', column: '' }
    : type === 'unique_key' ? { table: '', columns: [] }
      : type === 'foreign_key' ? { table: '', columns: [], reference_table: '', reference_columns: [] }
        : type === 'predicate_implication' ? { table: '', when: blankCondition(), then: blankCondition() }
          : { table: '', mode: 'range', exact: null, min: 1, max: null }
})

const conditionContract = condition => {
  const result = { column: condition.column, operator: condition.operator }
  if (condition.operator === 'eq' || condition.operator === 'not_eq') {
    if (condition.value_type === 'number') result.value = Number(condition.value)
    else if (condition.value_type === 'boolean') result.value = condition.value === true || condition.value === 'true'
    else result.value = String(condition.value)
  }
  return result
}

const assertionContract = assertion => {
  let params
  if (assertion.type === 'not_null') params = { table: assertion.params.table, column: assertion.params.column }
  else if (assertion.type === 'unique_key') params = { table: assertion.params.table, columns: [...assertion.params.columns] }
  else if (assertion.type === 'foreign_key') params = {
    table: assertion.params.table,
    columns: [...assertion.params.columns],
    reference_table: assertion.params.reference_table,
    reference_columns: [...assertion.params.reference_columns]
  }
  else if (assertion.type === 'predicate_implication') params = {
    table: assertion.params.table,
    when: conditionContract(assertion.params.when),
    then: conditionContract(assertion.params.then)
  }
  else if (assertion.params.mode === 'exact') params = { table: assertion.params.table, exact: assertion.params.exact }
  else {
    params = { table: assertion.params.table }
    if (assertion.params.min != null) params.min = assertion.params.min
    if (assertion.params.max != null) params.max = assertion.params.max
  }
  return { assertion_key: assertion.assertion_key, type: assertion.type, severity: assertion.severity, params }
}

export const buildMaterializationGateDocument = assertions => ({
  schema_version: MATERIALIZATION_GATE_SCHEMA_VERSION,
  assertions: assertions.map(assertionContract)
})

const inferValueType = value => typeof value === 'number' ? 'number' : typeof value === 'boolean' ? 'boolean' : 'string'
const editableCondition = condition => ({ ...condition, value: condition.value ?? '', value_type: inferValueType(condition.value) })

export const parseMaterializationGateDocument = document => {
  if (document?.schema_version !== MATERIALIZATION_GATE_SCHEMA_VERSION || !Array.isArray(document.assertions)) return []
  return document.assertions.map(assertion => {
    const params = structuredClone(assertion.params || {})
    if (assertion.type === 'predicate_implication') {
      params.when = editableCondition(params.when || blankCondition())
      params.then = editableCondition(params.then || blankCondition())
    } else if (assertion.type === 'row_count') {
      params.mode = Object.hasOwn(params, 'exact') ? 'exact' : 'range'
      params.exact ??= null
      params.min ??= null
      params.max ??= null
    }
    return { assertion_key: assertion.assertion_key, type: assertion.type, severity: assertion.severity, params }
  })
}
