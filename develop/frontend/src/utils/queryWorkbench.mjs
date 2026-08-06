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
  if (normalized === 'mql') return 'json'
  return normalized || 'plaintext'
}

export function formatterLanguageForQuery(language) {
  return String(language || '').trim().toLowerCase() === 'sql' ? 'sql' : ''
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
