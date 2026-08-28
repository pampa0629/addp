export const ELEMENT_DATA_TYPES = [
  'string',
  'text',
  'int',
  'bigint',
  'float',
  'decimal',
  'date',
  'datetime',
  'bool',
  'json'
]

const NUMERIC_DATA_TYPES = new Set(['int', 'bigint', 'float', 'decimal'])
const LENGTH_DATA_TYPES = new Set(['string', 'text'])
const FORMAT_DATA_TYPES = new Set(['string', 'text', 'date', 'datetime'])

export const isNumericDataType = dataType => NUMERIC_DATA_TYPES.has(dataType)
export const supportsLength = dataType => LENGTH_DATA_TYPES.has(dataType)
export const supportsFormat = dataType => FORMAT_DATA_TYPES.has(dataType)

export function isCodeSetCompatible(elementDataType, codeSetValueType) {
  if (codeSetValueType === 'string') return elementDataType === 'string' || elementDataType === 'text'
  if (codeSetValueType === 'int') return elementDataType === 'int'
  return codeSetValueType === 'bigint' && (elementDataType === 'bigint' || elementDataType === 'int')
}

export function resetIncompatibleElementConstraints(revision, dataType) {
  if (!supportsLength(dataType)) revision.length = null
  if (dataType !== 'decimal') {
    revision.precision_num = null
    revision.scale = null
  }
  if (!supportsFormat(dataType)) revision.format = ''
  if (revision.value_domain_kind === 'range' && !isNumericDataType(dataType)) {
    revision.value_domain_kind = 'unrestricted'
    revision.range_constraint = null
  }
  revision.code_set_revision_id = null
}

export function buildElementRevisionPayload(revision, version, uniqueRuleKey, uniqueEnabled) {
  return {
    version,
    name: revision.name,
    definition: revision.definition,
    data_type: revision.data_type,
    length: revision.length ?? null,
    precision_num: revision.precision_num ?? null,
    scale: revision.scale ?? null,
    nullable: Boolean(revision.nullable),
    default_value: revision.default_value || '',
    format: revision.format || '',
    value_domain_kind: revision.value_domain_kind,
    range_constraint: revision.value_domain_kind === 'range' ? revision.range_constraint : null,
    code_set_revision_id: revision.value_domain_kind === 'enumeration' ? revision.code_set_revision_id : null,
    unit_id: revision.unit_id ?? null,
    security_level: revision.security_level || '',
    classification_id: revision.classification_id ?? null,
    example_values: revision.example_values || [],
    extra_quality_rules: {
      schema_version: 'addp.quality.rules/v1',
      rules: uniqueEnabled
        ? [{ rule_key: uniqueRuleKey, type: 'unique', enabled: true, severity: 'error', message: '', params: {} }]
        : []
    },
    change_summary: revision.change_summary,
    effective_from: revision.effective_from || null,
    effective_to: revision.effective_to || null
  }
}

export function buildCodeSetRevisionPayload(revision, version) {
  return {
    version,
    name: revision.name,
    description: revision.description,
    value_type: revision.value_type,
    change_summary: revision.change_summary,
    effective_from: revision.effective_from || null,
    effective_to: revision.effective_to || null
  }
}

export function listReplacementItems(items, currentItemID) {
  return (items || []).filter(item => item.id !== currentItemID && item.status === 'active')
}
