const NUMERIC_TYPES = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const STATE_TONES = new Set(['info', 'success', 'warning', 'danger'])

export function fieldPresentationFor(field, presentations = []) {
  return (Array.isArray(presentations) ? presentations : []).find((item) => item?.field === field) || null
}

export function fieldPresentationLabel(field, presentations = [], fields = []) {
  const configured = fieldPresentationFor(field, presentations)?.label
  if (String(configured || '').trim()) return String(configured).trim()
  const descriptorField = (Array.isArray(fields) ? fields : []).find((item) => item?.name === field)
  return descriptorField?.comment || descriptorField?.name || field
}

export function formatFieldPresentationValue(value, presentation = null, locale = 'zh-CN', nullText = '—') {
  if (value === null || value === undefined) return nullText
  if (!presentation) return basicDisplayValue(value)

  let formatted
  if (Number.isInteger(presentation.precision)) {
    const numeric = Number(value)
    formatted = Number.isFinite(numeric)
      ? new Intl.NumberFormat(locale, {
          minimumFractionDigits: presentation.precision,
          maximumFractionDigits: presentation.precision,
        }).format(numeric)
      : basicDisplayValue(value)
  } else if (presentation.temporal_format) {
    formatted = formatTemporalValue(value, presentation.temporal_format, locale) || basicDisplayValue(value)
  } else {
    formatted = basicDisplayValue(value)
  }
  const unit = String(presentation.unit || '').trim()
  return unit ? `${formatted} ${unit}` : formatted
}

export function presentFieldValue(value, presentation = null, locale = 'zh-CN', nullText = '—') {
  return {
    text: formatFieldPresentationValue(value, presentation, locale, nullText),
    state: value === null || value === undefined ? null : matchingStateRule(value, presentation?.state_rules),
  }
}

export function formatFieldPresentationValueWithState(value, presentation = null, locale = 'zh-CN', nullText = '—') {
  const presented = presentFieldValue(value, presentation, locale, nullText)
  return presented.state ? `${presented.text} · ${presented.state.label}` : presented.text
}

export function defaultFieldPresentation(field) {
  const type = String(field?.type || '')
  return {
    field: field?.name || '',
    label: field?.comment || field?.name || '',
    fieldType: type,
    unit: '',
    precision: NUMERIC_TYPES.has(type) ? 0 : null,
    temporalFormat: type === 'date' ? 'date' : type === 'time' ? 'time' : type === 'timestamp' ? 'datetime' : '',
    width: null,
    stateRules: [],
  }
}

function matchingStateRule(value, rules) {
  for (const rule of Array.isArray(rules) ? rules : []) {
    if (!rule || !STATE_TONES.has(rule.tone) || !String(rule.label || '').trim()) continue
    if (stateRuleMatches(value, rule)) return { label: String(rule.label).trim(), tone: rule.tone }
  }
  return null
}

function stateRuleMatches(value, rule) {
  if (rule.operator === 'eq') {
    if (typeof rule.operand === 'number') {
      const numeric = Number(value)
      return Number.isFinite(numeric) && numeric === rule.operand
    }
    return value === rule.operand
  }
  if (!['lt', 'lte', 'gt', 'gte'].includes(rule.operator) || typeof rule.operand !== 'number') return false
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return false
  if (rule.operator === 'lt') return numeric < rule.operand
  if (rule.operator === 'lte') return numeric <= rule.operand
  if (rule.operator === 'gt') return numeric > rule.operand
  return numeric >= rule.operand
}

function basicDisplayValue(value) {
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value)
    } catch {
      return '[object]'
    }
  }
  return String(value)
}

function formatTemporalValue(value, format, locale) {
  const date = temporalDate(value, format)
  if (!date) return ''
  const options = format === 'date'
    ? { year: 'numeric', month: '2-digit', day: '2-digit' }
    : format === 'time'
      ? { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }
      : { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }
  try {
    return new Intl.DateTimeFormat(locale, options).format(date)
  } catch {
    return ''
  }
}

function temporalDate(value, format) {
  if (value instanceof Date) return Number.isNaN(value.getTime()) ? null : value
  const raw = String(value)
  const source = format === 'time' && /^\d{2}:\d{2}/.test(raw)
    ? `1970-01-01T${raw}`
    : format === 'date' && /^\d{4}-\d{2}-\d{2}$/.test(raw)
      ? `${raw}T00:00:00`
      : raw
  const parsed = new Date(source)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}
