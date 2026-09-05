export const SENSITIVE_PLACEHOLDER = '********'

export const isMaskedSensitiveValue = value => typeof value === 'string' && /\*{4,}/.test(value)

export function matchesFieldCondition(condition, connectionInfo = {}) {
  if (!condition) return true
  return (condition.values || []).includes(String(connectionInfo[condition.field] ?? ''))
}

export function visibleConnectionFields(connectionSpec, connectionInfo = {}) {
  let currentGroup = ''
  return (connectionSpec?.fields || [])
    .filter(field => matchesFieldCondition(field.visible_when, connectionInfo))
    .map(field => {
      const showGroupDivider = Boolean(field.group_key && field.group_key !== currentGroup)
      currentGroup = field.group_key || currentGroup
      return { ...field, showGroupDivider }
    })
}

export function applyConnectionSpecDefaults(connectionSpec, connectionInfo = {}) {
  const original = { ...(connectionInfo || {}) }
  const normalized = {}
  for (const field of connectionSpec?.fields || []) {
    const value = original[field.key]
    if (value !== undefined && value !== null) {
      normalized[field.key] = value
    } else if (field.default !== undefined && field.default !== null) {
      normalized[field.key] = field.default
    } else if (field.input === 'boolean') {
      normalized[field.key] = false
    } else {
      normalized[field.key] = ''
    }
    const storedFlag = `_has_${field.key}`
    if (field.sensitive && (original[storedFlag] === true || isMaskedSensitiveValue(value))) {
      normalized[storedFlag] = true
      normalized[field.key] = SENSITIVE_PLACEHOLDER
    }
  }
  return normalized
}

export function buildConnectionRules(connectionSpec, connectionInfo, translate) {
  const rules = {}
  const visibleFields = visibleConnectionFields(connectionSpec, connectionInfo)
  for (const field of visibleFields) {
    if (!field.required) continue
    rules[`connection_info.${field.key}`] = [{
      required: true,
      message: translate('storageEngine.valid.requiredField', { field: translate(field.label_key) }),
      trigger: field.input === 'select' || field.input === 'number' || field.input === 'boolean' ? 'change' : 'blur'
    }]
  }
  for (const constraint of connectionSpec?.constraints || []) {
    if (constraint.kind !== 'all_or_none' || !matchesFieldCondition(constraint.when, connectionInfo)) continue
    const validate = (_rule, _value, callback) => {
      const populated = constraint.fields.filter(key => String(connectionInfo?.[key] || '').trim() !== '').length
      if (populated !== 0 && populated !== constraint.fields.length) {
        callback(new Error(translate(constraint.message_key)))
        return
      }
      callback()
    }
    for (const key of constraint.fields) {
      rules[`connection_info.${key}`] = [{ validator: validate, trigger: 'blur' }]
    }
  }
  return rules
}
