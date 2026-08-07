const TERMINAL_EXECUTION_STATUSES = new Set(['success', 'failed', 'timeout', 'cancelled'])

function parseCapabilities(value) {
  if (!value) return null
  if (typeof value === 'object') return value
  if (typeof value !== 'string') return null
  try {
    return JSON.parse(value)
  } catch {
    return null
  }
}

export function queryCapabilityForEngine(engine) {
  const query = parseCapabilities(engine?.capabilities)?.compute?.query
  if (!query?.supported) {
    return { languages: [], defaultLanguage: '', resultKinds: [], parameters: null }
  }
  const languages = Array.from(new Set((query.languages || []).map(value => String(value).trim().toLowerCase()).filter(Boolean)))
  const declaredDefault = String(query.default_language || '').trim().toLowerCase()
  return {
    languages,
    defaultLanguage: languages.includes(declaredDefault) ? declaredDefault : (languages[0] || ''),
    resultKinds: Array.from(new Set((query.result_kinds || []).map(value => String(value).trim().toLowerCase()).filter(Boolean))),
    parameters: normalizeQueryParameterCapability(query.parameters, languages)
  }
}

function normalizeQueryParameterCapability(value, queryLanguages) {
  if (!value?.supported) return null
  const languages = Array.from(new Set((value.languages || []).map(item => String(item).trim().toLowerCase()).filter(Boolean)))
  const types = Array.from(new Set((value.types || []).map(item => String(item).trim().toLowerCase()).filter(Boolean)))
  if (languages.length === 0 || types.length === 0 || languages.some(language => !queryLanguages.includes(language))) return null
  return { supported: true, languages, types }
}

export function queryParameterReference(language, name) {
  const normalizedLanguage = String(language || '').trim().toLowerCase()
  const normalizedName = String(name || '').trim()
  if (!normalizedName) return ''
  if (normalizedLanguage === 'sql') return `:${normalizedName}`
  if (normalizedLanguage === 'cypher') return `$${normalizedName}`
  if (normalizedLanguage === 'mql') return JSON.stringify({ $param: normalizedName })
  return ''
}

function isIdentifierStart(char) {
  return /[A-Za-z_]/.test(char || '')
}

function isIdentifierPart(char) {
  return /[A-Za-z0-9_]/.test(char || '')
}

function collectTextParameterReferences(query, prefix, { slashLineComments = false } = {}) {
  const references = []
  const seen = new Set()
  const text = String(query || '')
  for (let index = 0; index < text.length;) {
    const current = text[index]
    if (current === "'" || current === '"' || current === '`') {
      const quote = current
      index += 1
      while (index < text.length) {
        if (text[index] === '\\') {
          index += 2
          continue
        }
        if (text[index] === quote) {
          if (text[index + 1] === quote) {
            index += 2
            continue
          }
          index += 1
          break
        }
        index += 1
      }
      continue
    }
    if (current === '-' && text[index + 1] === '-') {
      const end = text.indexOf('\n', index + 2)
      index = end < 0 ? text.length : end + 1
      continue
    }
    if (slashLineComments && current === '/' && text[index + 1] === '/') {
      const end = text.indexOf('\n', index + 2)
      index = end < 0 ? text.length : end + 1
      continue
    }
    if (current === '/' && text[index + 1] === '*') {
      const end = text.indexOf('*/', index + 2)
      index = end < 0 ? text.length : end + 2
      continue
    }
    if (current === prefix && isIdentifierStart(text[index + 1])) {
      let end = index + 2
      while (end < text.length && isIdentifierPart(text[end])) end += 1
      const name = text.slice(index + 1, end)
      if (!seen.has(name)) {
        seen.add(name)
        references.push(name)
      }
      index = end
      continue
    }
    index += 1
  }
  return references
}

export function extractQueryParameterReferences(language, query) {
  const normalizedLanguage = String(language || '').trim().toLowerCase()
  if (normalizedLanguage === 'mql') {
    try {
      const value = JSON.parse(String(query || ''))
      const references = []
      const seen = new Set()
      const visit = current => {
        if (Array.isArray(current)) {
          current.forEach(visit)
          return
        }
        if (!current || typeof current !== 'object') return
        if (Object.prototype.hasOwnProperty.call(current, '$param')) {
          const name = String(current.$param || '').trim()
          if (name && !seen.has(name)) {
            seen.add(name)
            references.push(name)
          }
          return
        }
        Object.values(current).forEach(visit)
      }
      visit(value)
      return references
    } catch {
      return []
    }
  }
  if (normalizedLanguage === 'sql') return collectTextParameterReferences(query, ':')
  if (normalizedLanguage === 'cypher') return collectTextParameterReferences(query, '$', { slashLineComments: true })
  return []
}

function inferScalarParameter(value) {
  if (typeof value === 'string') return { type: 'string', default: value }
  if (typeof value === 'boolean') return { type: 'boolean', default: value }
  if (typeof value === 'number' && Number.isFinite(value)) {
    return Number.isInteger(value)
      ? { type: 'integer', default: value }
      : { type: 'number', default: value }
  }
  return null
}

export function parameterizeSelection(language, selection, name) {
  const normalizedLanguage = String(language || '').trim().toLowerCase()
  const selected = String(selection || '').trim()
  const parameterName = String(name || '').trim()
  if (!selected || !parameterName) return null

  if (normalizedLanguage === 'mql') {
    let value
    try {
      value = JSON.parse(selected)
    } catch {
      return null
    }
    const scalar = inferScalarParameter(value)
    if (!scalar) return null
    return { reference: JSON.stringify({ $param: parameterName }), ...scalar }
  }

  let value
  if ((normalizedLanguage === 'sql' && selected.startsWith("'") && selected.endsWith("'")) ||
      (normalizedLanguage === 'cypher' && ((selected.startsWith("'") && selected.endsWith("'")) || (selected.startsWith('"') && selected.endsWith('"'))))) {
    const quote = selected[0]
    value = selected.slice(1, -1)
      .replaceAll(`${quote}${quote}`, quote)
      .replaceAll('\\\\', '\\')
  } else if (/^[+-]?\d+$/.test(selected)) {
    value = Number(selected)
  } else if (/^[+-]?(?:\d+\.\d*|\d*\.\d+)(?:[eE][+-]?\d+)?$/.test(selected)) {
    value = Number(selected)
  } else if (/^(true|false)$/i.test(selected)) {
    value = selected.toLowerCase() === 'true'
  } else {
    return null
  }
  const scalar = inferScalarParameter(value)
  if (!scalar) return null
  const reference = queryParameterReference(normalizedLanguage, parameterName)
  return reference ? { reference, ...scalar } : null
}

export function buildQueryExecutionContract(definitions = []) {
  const properties = {}
  const inputDefaults = {}
  const inputUISchema = {}
  definitions.forEach((definition, index) => {
    const name = String(definition?.name || '').trim()
    if (!name) return
    properties[name] = {
      type: definition.type,
      ...(definition.title ? { title: definition.title } : {}),
      ...(definition.description ? { description: definition.description } : {})
    }
    inputDefaults[name] = definition.default
    inputUISchema[name] = { order: index }
  })
  return {
    input_schema: { type: 'object', additionalProperties: false, properties },
    input_defaults: inputDefaults,
    input_ui_schema: inputUISchema,
    output_schema: { type: 'object', additionalProperties: false, properties: {} },
    output_defaults: {},
    output_ui_schema: {}
  }
}

export function monacoLanguageForQuery(language) {
  const normalized = String(language || '').trim().toLowerCase()
  return normalized || 'plaintext'
}

export function formatterLanguageForQuery(language) {
  return String(language || '').trim().toLowerCase() === 'sql' ? 'sql' : ''
}

const QUERY_DIAGNOSTIC_KEYWORDS = new Set([
  'SELECT', 'FROM', 'WHERE', 'JOIN', 'LEFT', 'RIGHT', 'FULL', 'INNER', 'OUTER', 'CROSS', 'ON', 'AS',
  'AND', 'OR', 'NOT', 'IN', 'IS', 'NULL', 'TRUE', 'FALSE', 'BETWEEN', 'LIKE', 'ILIKE', 'EXISTS',
  'GROUP', 'BY', 'ORDER', 'HAVING', 'LIMIT', 'OFFSET', 'UNION', 'ALL', 'DISTINCT', 'ASC', 'DESC',
  'CASE', 'WHEN', 'THEN', 'ELSE', 'END', 'INSERT', 'INTO', 'VALUES', 'UPDATE', 'SET', 'DELETE',
  'CREATE', 'ALTER', 'DROP', 'TABLE', 'VIEW', 'INDEX', 'RETURNING', 'WITH', 'OVER', 'PARTITION'
])

const QUERY_DIAGNOSTIC_TABLE_CONTEXT = new Set(['FROM', 'JOIN', 'UPDATE', 'INTO', 'TABLE'])
const SQL_RESERVED_IDENTIFIERS = new Set([
  'all', 'alter', 'and', 'as', 'asc', 'by', 'case', 'column', 'create', 'database', 'default',
  'delete', 'desc', 'distinct', 'drop', 'else', 'from', 'group', 'having', 'in', 'index', 'insert',
  'into', 'is', 'join', 'key', 'limit', 'not', 'null', 'on', 'or', 'order', 'primary', 'references',
  'returning', 'role', 'schema', 'select', 'set', 'table', 'then', 'type', 'union', 'update', 'user',
  'using', 'value', 'values', 'when', 'where', 'with'
])

function fieldNameParts(value) {
  return String(value || '').split('.').map(part => part.trim().replace(/^['"`]|['"`]$/g, '')).filter(Boolean)
}

function fieldLookup(fields = []) {
  const exact = new Set()
  const folded = new Map()
  fields.forEach(field => {
    const name = String(typeof field === 'string' ? field : field?.label || field?.name || '').trim()
    if (!name) return
    exact.add(name)
    const key = name.toLocaleLowerCase()
    if (!folded.has(key)) folded.set(key, name)
  })
  return { exact, folded }
}

function fieldDiagnostic(name, lookup, seen, engineType) {
  const rawName = String(name || '').trim()
  const parts = fieldNameParts(name)
  if (parts.length === 0) return null
  const candidate = parts[parts.length - 1]
  if (seen.has(candidate)) return null
  seen.add(candidate)
  if (lookup.exact.has(candidate)) {
    const quote = engineType === 'mysql' ? '`' : '"'
    const simpleLowerIdentifier = /^[a-z_][a-z0-9_$]*$/.test(candidate)
    const requiresQuote = !simpleLowerIdentifier || SQL_RESERVED_IDENTIFIERS.has(candidate.toLocaleLowerCase())
    if (engineType && !/^["`]/.test(rawName) && requiresQuote) {
      return { code: 'field_requires_quote', severity: 'warning', field: candidate, suggested: `${quote}${candidate}${quote}` }
    }
    return null
  }
  const suggested = lookup.folded.get(candidate.toLocaleLowerCase())
  if (suggested && suggested !== candidate) {
    const quote = engineType === 'mysql' ? '`' : '"'
    const simpleLowerIdentifier = /^[a-z_][a-z0-9_$]+$/.test(suggested)
    const requiresQuote = !simpleLowerIdentifier || SQL_RESERVED_IDENTIFIERS.has(suggested.toLocaleLowerCase())
    if (engineType && requiresQuote) {
      return { code: 'field_requires_quote', severity: 'warning', field: candidate, suggested: `${quote}${suggested}${quote}` }
    }
    return { code: 'field_case_mismatch', severity: 'warning', field: candidate, suggested }
  }
  return { code: 'field_unknown', severity: 'warning', field: candidate }
}

function collectMQLFieldCandidates(query) {
  try {
    const root = JSON.parse(String(query || ''))
    const candidates = []
    const visit = (value, path = []) => {
      if (Array.isArray(value)) {
        value.forEach(item => visit(item, path))
        return
      }
      if (!value || typeof value !== 'object') return
      Object.entries(value).forEach(([key, child]) => {
        if (key.startsWith('$')) {
          visit(child, path)
          return
        }
        const nextPath = [...path, ...fieldNameParts(key)]
        candidates.push(nextPath.join('.'))
        visit(child, nextPath)
      })
    }
    visit(root)
    return { candidates, parseError: false }
  } catch {
    return { candidates: [], parseError: true }
  }
}

function collectCypherFieldCandidates(query) {
  const candidates = []
  const pattern = /\.\s*(`[^`]+`|[A-Za-z_][A-Za-z0-9_]*)/g
  let match
  while ((match = pattern.exec(String(query || '')))) candidates.push(match[1])
  return candidates
}

function collectSQLFieldCandidates(query) {
  const candidates = []
  const tokenPattern = /(?:"(?:""|[^"])+"|`(?:``|[^`])+`|[A-Za-z_][A-Za-z0-9_$]*)|[(),.;:]/g
  const tokenMatches = Array.from(String(query || '').matchAll(tokenPattern))
  const tokens = tokenMatches.map(match => match[0])
  const tableIdentifiers = new Set()
  let tableReferenceContext = false
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index]
    const upper = token.toUpperCase()
    if (QUERY_DIAGNOSTIC_TABLE_CONTEXT.has(upper)) {
      tableReferenceContext = true
      continue
    }
    if (['SELECT', 'WHERE', 'ON', 'SET', 'HAVING', 'RETURNING', 'LIMIT', 'ORDER', 'GROUP'].includes(upper)) {
      tableReferenceContext = false
      continue
    }
    if (!tableReferenceContext || !isSQLIdentifierToken(token)) continue
    tableIdentifiers.add(index)
    let qualifiedIndex = index + 1
    while (tokens[qualifiedIndex] === '.' && isSQLIdentifierToken(tokens[qualifiedIndex + 1])) {
      tableIdentifiers.add(qualifiedIndex + 1)
      qualifiedIndex += 2
    }
    tableReferenceContext = false
  }

  let tableContext = false
  let expressionContext = false
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index]
    const upper = token.toUpperCase()
    if (tableIdentifiers.has(index)) continue
    if (token === ',' || token === ';') {
      if (tableContext) continue
      expressionContext = true
      continue
    }
    if (QUERY_DIAGNOSTIC_TABLE_CONTEXT.has(upper)) {
      tableContext = true
      expressionContext = false
      continue
    }
    if (['SELECT', 'WHERE', 'ON', 'SET', 'HAVING', 'RETURNING'].includes(upper)) {
      tableContext = false
      expressionContext = true
      continue
    }
    if (token === '(' || token === ')' || token === ':' || QUERY_DIAGNOSTIC_KEYWORDS.has(upper)) continue
    const next = tokens[index + 1]
    if (next === '.') continue
    if (next === '(') continue
    if (tableContext) {
      tableContext = false
      continue
    }
    if (tokens[index - 1] === ':') continue
    if (expressionContext || tokens[index - 1] === '.') {
      const start = tokenMatches[index].index
      candidates.push({ name: token, start, end: start + token.length })
    }
  }
  return candidates
}

function isSQLIdentifierToken(token) {
  return /^"(?:""|[^"])+"$/.test(token) || /^`(?:``|[^`])+`$/.test(token) || /^[A-Za-z_][A-Za-z0-9_$]*$/.test(token)
}

/** 基于当前资源字段和查询参数做高置信度静态诊断。 */
export function diagnoseQuery({ language, engineType = '', query, fields = [], targetLocator = '', referencedParameters = [], definedParameters = [] } = {}) {
  const normalizedLanguage = String(language || '').trim().toLowerCase()
  const normalizedEngineType = String(engineType || '').trim().toLowerCase()
  const diagnostics = []
  const text = String(query || '').trim()
  if (!text) return [{ code: 'query_empty', severity: 'error' }]
  if (!targetLocator) diagnostics.push({ code: 'target_missing', severity: 'warning' })

  const defined = new Set(definedParameters.map(value => String(value || '').trim()).filter(Boolean))
  referencedParameters.forEach(name => {
    if (!defined.has(name)) diagnostics.push({ code: 'parameter_undefined', severity: 'error', name })
  })

  const lookup = fieldLookup(fields)
  if (lookup.exact.size === 0) return diagnostics

  let result
  if (normalizedLanguage === 'mql') {
    result = collectMQLFieldCandidates(text)
    if (result.parseError) return diagnostics
  } else if (normalizedLanguage === 'cypher') {
    result = { candidates: collectCypherFieldCandidates(text) }
  } else if (normalizedLanguage === 'sql') {
    result = { candidates: collectSQLFieldCandidates(text) }
  } else {
    return diagnostics
  }

  const seen = new Set()
  result.candidates.forEach(candidate => {
    const candidateName = typeof candidate === 'string' ? candidate : candidate.name
    const parts = fieldNameParts(candidateName)
    if (parts.length > 1 && lookup.exact.has(parts[0])) return
    const diagnostic = fieldDiagnostic(candidateName, lookup, seen, normalizedEngineType)
    if (diagnostic && typeof candidate === 'object' && diagnostic.suggested) {
      diagnostic.start = candidate.start
      diagnostic.end = candidate.end
      diagnostic.replacement = diagnostic.suggested
    }
    if (diagnostic) diagnostics.push(diagnostic)
  })
  return diagnostics
}

export function isTerminalExecutionStatus(status) {
  return TERMINAL_EXECUTION_STATUSES.has(String(status || '').trim().toLowerCase())
}

export function queryResultFromExecution(execution) {
  const result = execution?.metadata?.result || {}
  const summary = result.summary || {}
  const status = String(execution?.status || '')
  const success = status === 'success'
  return {
    success,
    status,
    progress: Number(execution?.progress || 0),
    execution_id: execution?.execution_id || '',
    columns: Array.isArray(result.columns) ? result.columns : [],
    rows: Array.isArray(summary.preview_rows) ? summary.preview_rows : [],
    rows_count: Number(result.rows_count || 0),
    rows_affected: result.rows_affected,
    execution_time_ms: execution?.execution_time_ms,
    result_kind: result.result_kind || 'table',
    result_limit: result.result_limit,
    truncated: result.truncated === true,
    diagnostics: Array.isArray(result.diagnostics) ? result.diagnostics : [],
    graph_data: result.graph_data || null,
    error_code: success ? '' : (execution?.error_details?.error_code || ''),
    error: success ? '' : (execution?.error_details?.message || execution?.error_details?.error || '')
  }
}

export function queryErrorMessage(errorCode, fallback, translate) {
  if (errorCode === 'mongodb_database_required' && typeof translate === 'function') {
    return translate('develop.queryResult.mongodbDatabaseRequired')
  }
  const undefinedColumn = /column\s+"([^"]+)"\s+does not exist[\s\S]*SQLSTATE\s+42703/i.exec(String(fallback || ''))
  if (undefinedColumn && typeof translate === 'function') {
    return translate('develop.queryResult.postgresqlUndefinedColumn', { field: undefinedColumn[1] })
  }
  return fallback || ''
}

export function csvCell(value) {
  if (value === null) return 'NULL'
  if (value === undefined) return ''
  const text = typeof value === 'object' ? JSON.stringify(value) : String(value)
  return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text
}

export function buildQueryResultCSV(columns = [], rows = []) {
  const header = columns.map(csvCell).join(',')
  const body = rows.map(row => columns.map(column => csvCell(row?.[column])).join(','))
  return [header, ...body].join('\r\n')
}
