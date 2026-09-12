import { normalizeFieldType } from '@addp/common-frontend/basic/src/utils/fieldTypes.js'

const NULL_OPERATORS = new Set(['is_null', 'is_not_null'])
const COMPARABLE_TYPES = new Set(['int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp'])
const FILTERABLE_TYPES = new Set(['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'uuid'])
const BINARY_SQL = {
  eq: '=',
  ne: '<>',
  gt: '>',
  gte: '>=',
  lt: '<',
  lte: '<='
}

export function createRelationalSQLQuery(sourcePath = [], sourceFields = []) {
  return {
    sourcePath: normalizeSourcePath(sourcePath),
    selectedFields: uniqueSourceFields(sourceFields).map(field => field.name),
    matchMode: 'all',
    filters: []
  }
}

export function relationalSQLFilterOperators(field, parameterTypes = []) {
  const type = normalizeFieldType(field, '')
  if (!FILTERABLE_TYPES.has(type)) return []
  if (!parameterTypeSupported(relationalSQLParameterType(field), parameterTypes)) {
    return ['is_null', 'is_not_null']
  }
  const operators = ['eq', 'ne', 'in', 'is_null', 'is_not_null']
  if (COMPARABLE_TYPES.has(type)) operators.splice(2, 0, 'gt', 'gte', 'lt', 'lte')
  return operators
}

export function createRelationalSQLFilter(field, sourceFields = [], parameterTypes = []) {
  const source = findSourceField(sourceFields, field)
  const operators = relationalSQLFilterOperators(source, parameterTypes)
  return {
    field: source?.name || '',
    operator: operators[0] || '',
    value: undefined
  }
}

export function compileRelationalSQLQuery(model, options = {}) {
  const normalized = normalizeModel(model, options.sourceFields)
  const issues = validateNormalizedModel(
    normalized,
    options.sourceFields,
    options.parametersSupported !== false,
    options.parameterTypes
  )
  if (issues.length > 0) {
    const error = new Error('invalid relational SQL query')
    error.issues = issues
    throw error
  }

  const quote = normalizeIdentifierQuote(options.identifierQuote)
  const projections = normalized.selectedFields.map(field => quoteSQLIdentifier(field, quote)).join(', ')
  const source = normalized.sourcePath.map(segment => quoteSQLIdentifier(segment, quote)).join('.')
  const parameters = {}
  let parameterIndex = 1
  const predicates = normalized.filters.map(filter => {
    const identifier = quoteSQLIdentifier(filter.field, quote)
    if (filter.operator === 'is_null') return `${identifier} IS NULL`
    if (filter.operator === 'is_not_null') return `${identifier} IS NOT NULL`
    const field = findSourceField(options.sourceFields, filter.field)
    if (filter.operator === 'in') {
      const references = filter.value.map(value => {
        const name = `p${parameterIndex++}`
        parameters[name] = coerceFilterValue(field, value)
        return `:${name}`
      })
      return `${identifier} IN (${references.join(', ')})`
    }
    const name = `p${parameterIndex++}`
    parameters[name] = coerceFilterValue(field, filter.value)
    return `${identifier} ${BINARY_SQL[filter.operator]} :${name}`
  })

  const conjunction = normalized.matchMode === 'any' ? ' OR ' : ' AND '
  return {
    statement: `SELECT ${projections} FROM ${source}${predicates.length > 0 ? ` WHERE ${predicates.join(conjunction)}` : ''}`,
    parameters
  }
}

export function parseRelationalSQLQuery(statement, parameters, options = {}) {
  const text = String(statement || '').trim()
  const sourceFields = uniqueSourceFields(options.sourceFields)
  const sourcePath = normalizeSourcePath(options.sourcePath)
  const quote = normalizeIdentifierQuote(options.identifierQuote)
  if (!text || sourceFields.length === 0 || sourcePath.length === 0 || !isPlainObject(parameters)) {
    return unsupported('incomplete_query')
  }

  const sourceSQL = sourcePath.map(segment => quoteSQLIdentifier(segment, quote)).join('.')
  const fromMarker = ` FROM ${sourceSQL}`
  if (!text.startsWith('SELECT ') || text.indexOf(fromMarker) <= 7 || text.indexOf(fromMarker) !== text.lastIndexOf(fromMarker)) {
    return unsupported('unsupported_statement')
  }
  const projectionSQL = text.slice(7, text.indexOf(fromMarker))
  const tail = text.slice(text.indexOf(fromMarker) + fromMarker.length)
  if (tail && !tail.startsWith(' WHERE ')) return unsupported('unsupported_statement')

  const selectedFields = splitOutsideQuotedText(projectionSQL, ', ', quote)
    .map(token => decodeKnownIdentifier(token, sourceFields, quote))
  if (selectedFields.length === 0 || selectedFields.some(field => !field)) {
    return unsupported('unsupported_projection')
  }
  if (new Set(selectedFields.map(field => field.toLowerCase())).size !== selectedFields.length) {
    return unsupported('unsupported_projection')
  }

  const whereSQL = tail ? tail.slice(7) : ''
  let matchMode = 'all'
  let predicateSQL = []
  if (whereSQL) {
    const allPredicates = splitOutsideQuotedText(whereSQL, ' AND ', quote)
    const anyPredicates = splitOutsideQuotedText(whereSQL, ' OR ', quote)
    if (allPredicates.length > 1 && anyPredicates.length > 1) return unsupported('nested_filter')
    if (anyPredicates.length > 1) {
      matchMode = 'any'
      predicateSQL = anyPredicates
    } else {
      predicateSQL = allPredicates
    }
  }

  const usedParameters = []
  const filters = []
  for (const predicate of predicateSQL) {
    const parsed = parsePredicate(predicate, parameters, sourceFields, quote, usedParameters)
    if (!parsed) return unsupported('unsupported_filter')
    filters.push(parsed)
  }
  const parameterKeys = Object.keys(parameters).sort(compareParameterNames)
  if (parameterKeys.length !== usedParameters.length || parameterKeys.some((name, index) => name !== usedParameters[index])) {
    return unsupported('noncanonical_parameters')
  }

  const model = { sourcePath, selectedFields, matchMode, filters }
  const issues = validateNormalizedModel(
    model,
    sourceFields,
    options.parametersSupported !== false,
    options.parameterTypes
  )
  if (issues.length > 0) return { supported: false, reason: 'invalid_structure', issues }
  const compiled = compileRelationalSQLQuery(model, { ...options, sourceFields })
  if (compiled.statement !== text || !parametersEqual(compiled.parameters, parameters)) {
    return unsupported('noncanonical_query')
  }
  return { supported: true, model }
}

export function relationalSQLOutputFields(model, sourceFields = []) {
  const selected = new Set(normalizeModel(model, sourceFields).selectedFields.map(name => name.toLowerCase()))
  return uniqueSourceFields(sourceFields).filter(field => selected.has(field.name.toLowerCase())).map(field => ({ ...field }))
}

export function validateRelationalSQLQuery(model, sourceFields = [], parametersSupported = true, parameterTypes = []) {
  return validateNormalizedModel(normalizeModel(model, sourceFields), sourceFields, parametersSupported, parameterTypes)
}

export function relationalSQLFieldKind(field) {
  const type = normalizeFieldType(field, '')
  if (['bigint', 'decimal'].includes(type)) return 'exact-number'
  if (['int', 'float', 'double'].includes(type)) return 'number'
  if (type === 'bool') return 'boolean'
  if (type === 'date') return 'date'
  if (type === 'time') return 'time'
  if (type === 'timestamp') return 'timestamp'
  return 'text'
}

export function relationalSQLParameterType(field) {
  const type = normalizeFieldType(field, '')
  if (type === 'int') return 'integer'
  if (['float', 'double'].includes(type)) return 'number'
  if (type === 'bool') return 'boolean'
  if (['string', 'bigint', 'decimal', 'date', 'time', 'timestamp', 'uuid'].includes(type)) return 'string'
  return ''
}

function normalizeModel(model = {}, sourceFields = []) {
  const fields = uniqueSourceFields(sourceFields)
  const selected = new Set((Array.isArray(model.selectedFields) ? model.selectedFields : [])
    .map(cleanText).filter(Boolean).map(name => name.toLowerCase()))
  return {
    sourcePath: normalizeSourcePath(model.sourcePath),
    selectedFields: fields.filter(field => selected.has(field.name.toLowerCase())).map(field => field.name),
    matchMode: model.matchMode === 'any' ? 'any' : 'all',
    filters: (Array.isArray(model.filters) ? model.filters : []).map(filter => ({
      field: cleanText(filter?.field),
      operator: cleanText(filter?.operator).toLowerCase(),
      value: Array.isArray(filter?.value) ? [...filter.value] : filter?.value
    }))
  }
}

function validateNormalizedModel(model, sourceFields, parametersSupported, parameterTypes) {
  const issues = []
  if (model.sourcePath.length === 0) issues.push(issue('sourcePath', 'source_required'))
  if (model.selectedFields.length === 0) issues.push(issue('selectedFields', 'projection_required'))
  if (!parametersSupported && model.filters.length > 0) issues.push(issue('filters', 'parameters_unsupported'))
  model.filters.forEach((filter, index) => {
    const field = findSourceField(sourceFields, filter.field)
    const operators = relationalSQLFilterOperators(field, parameterTypes)
    if (!field) issues.push(issue(`filters.${index}.field`, 'filter_field_required'))
    if (!operators.includes(filter.operator)) issues.push(issue(`filters.${index}.operator`, 'filter_operator_invalid'))
    if (NULL_OPERATORS.has(filter.operator)) return
    if (filter.operator === 'in') {
      if (!Array.isArray(filter.value) || filter.value.length === 0 || filter.value.some(value => !filterValueValid(field, value))) {
        issues.push(issue(`filters.${index}.value`, 'filter_value_required'))
      }
      return
    }
    if (!filterValueValid(field, filter.value)) issues.push(issue(`filters.${index}.value`, 'filter_value_required'))
  })
  return issues
}

function parsePredicate(predicate, parameters, sourceFields, quote, usedParameters) {
  const fields = [...sourceFields].sort((left, right) => right.name.length - left.name.length)
  for (const field of fields) {
    const identifier = quoteSQLIdentifier(field.name, quote)
    if (predicate === `${identifier} IS NULL`) return { field: field.name, operator: 'is_null', value: undefined }
    if (predicate === `${identifier} IS NOT NULL`) return { field: field.name, operator: 'is_not_null', value: undefined }
    if (predicate.startsWith(`${identifier} IN (`) && predicate.endsWith(')')) {
      const body = predicate.slice(identifier.length + 5, -1)
      const names = body.split(', ').map(parseParameterReference)
      if (names.length === 0 || names.some(name => !name || !(name in parameters))) return null
      if (!appendCanonicalParameterNames(names, usedParameters)) return null
      return { field: field.name, operator: 'in', value: names.map(name => parameters[name]) }
    }
    for (const [operator, sql] of Object.entries(BINARY_SQL)) {
      const prefix = `${identifier} ${sql} `
      if (!predicate.startsWith(prefix)) continue
      const name = parseParameterReference(predicate.slice(prefix.length))
      if (!name || !(name in parameters) || !appendCanonicalParameterNames([name], usedParameters)) return null
      return { field: field.name, operator, value: parameters[name] }
    }
  }
  return null
}

function appendCanonicalParameterNames(names, usedParameters) {
  for (const name of names) {
    const expected = `p${usedParameters.length + 1}`
    if (name !== expected) return false
    usedParameters.push(name)
  }
  return true
}

function parseParameterReference(value) {
  const match = /^:([A-Za-z_][A-Za-z0-9_]*)$/.exec(String(value || ''))
  return match?.[1] || ''
}

function compareParameterNames(left, right) {
  return Number(left.slice(1)) - Number(right.slice(1))
}

function parametersEqual(left, right) {
  const leftKeys = Object.keys(left).sort(compareParameterNames)
  const rightKeys = Object.keys(right).sort(compareParameterNames)
  return leftKeys.length === rightKeys.length && leftKeys.every((key, index) => (
    key === rightKeys[index] && JSON.stringify(left[key]) === JSON.stringify(right[key])
  ))
}

function coerceFilterValue(field, value) {
  const kind = relationalSQLFieldKind(field)
  if (kind === 'exact-number') return typeof value === 'string' ? value.trim() : value
  if (kind === 'number') {
    const number = Number(value)
    return Number.isFinite(number) ? number : value
  }
  if (kind === 'boolean') return value === true || String(value).toLowerCase() === 'true'
  return value
}

function filterValueValid(field, value) {
  if (value === undefined || value === null) return false
  const kind = relationalSQLFieldKind(field)
  if (kind === 'exact-number') {
    if (typeof value !== 'string') return false
    const text = value.trim()
    return normalizeFieldType(field, '') === 'bigint'
      ? /^[+-]?\d+$/.test(text)
      : /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/.test(text)
  }
  if (kind === 'number') {
    const number = Number(value)
    return normalizeFieldType(field, '') === 'int' ? Number.isSafeInteger(number) : Number.isFinite(number)
  }
  if (kind === 'boolean') return value === true || value === false || value === 'true' || value === 'false'
  return typeof value === 'string' || typeof value === 'number'
}

function parameterTypeSupported(requiredType, parameterTypes) {
  const supported = new Set((parameterTypes instanceof Set || Array.isArray(parameterTypes) ? [...parameterTypes] : [])
    .map(value => String(value || '').trim().toLowerCase())
    .filter(Boolean))
  if (requiredType === 'integer') return supported.has('integer') || supported.has('number')
  return Boolean(requiredType) && supported.has(requiredType)
}

function splitOutsideQuotedText(text, separator, quote) {
  if (!quote) return String(text || '').split(separator)
  const result = []
  let start = 0
  let quoted = false
  for (let index = 0; index < text.length; index += 1) {
    if (text[index] === quote) {
      if (quoted && text[index + 1] === quote) {
        index += 1
        continue
      }
      quoted = !quoted
      continue
    }
    if (!quoted && text.startsWith(separator, index)) {
      result.push(text.slice(start, index))
      index += separator.length - 1
      start = index + 1
    }
  }
  result.push(text.slice(start))
  return result
}

function decodeKnownIdentifier(token, sourceFields, quote) {
  return sourceFields.find(field => quoteSQLIdentifier(field.name, quote) === token)?.name || ''
}

function quoteSQLIdentifier(value, quote) {
  const text = cleanText(value)
  if (!quote) return text
  return `${quote}${text.replaceAll(quote, `${quote}${quote}`)}${quote}`
}

function normalizeIdentifierQuote(value) {
  const quote = String(value || '')
  return Array.from(quote).length === 1 ? quote : ''
}

function normalizeSourcePath(value) {
  return (Array.isArray(value) ? value : []).map(cleanText).filter(Boolean)
}

function uniqueSourceFields(sourceFields) {
  const seen = new Set()
  return (Array.isArray(sourceFields) ? sourceFields : []).flatMap(field => {
    const name = cleanText(field?.name)
    const key = name.toLowerCase()
    if (!name || seen.has(key)) return []
    seen.add(key)
    return [{ ...field, name }]
  })
}

function findSourceField(sourceFields, name) {
  const key = cleanText(name).toLowerCase()
  return uniqueSourceFields(sourceFields).find(field => field.name.toLowerCase() === key)
}

function cleanText(value) {
  return typeof value === 'string' ? value.trim() : ''
}

function isPlainObject(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function issue(path, code) {
  return { path, code }
}

function unsupported(reason) {
  return { supported: false, reason }
}
