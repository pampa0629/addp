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
    graph_data: result.graph_data || null,
    error: success ? '' : (execution?.error_details?.message || execution?.error_details?.error || '')
  }
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
