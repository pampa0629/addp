const FIELD_ORDER = {
  glossary: ['name', 'definition'],
  element: ['name', 'definition', 'data_type', 'value_domain_kind', 'code_set_code', 'unit'],
  code_set: ['name', 'definition', 'data_type', 'items'],
  metric: ['name', 'definition', 'calculation_formula', 'statistical_scope', 'aggregation', 'dimensions', 'unit']
}

const normalizeText = value => typeof value === 'string' ? value.trim().replace(/\s+/g, ' ') : ''

const compareText = (left, right) => left < right ? -1 : left > right ? 1 : 0

const normalizeDimensions = values => [...new Set((values || []).map(normalizeText).filter(Boolean))].sort(compareText)

const normalizeItems = values => (values || [])
  .map(item => ({
    code: normalizeText(item?.code),
    name: normalizeText(item?.name),
    definition: normalizeText(item?.definition)
  }))
  .sort((left, right) => compareText(left.code, right.code) || compareText(left.name, right.name) || compareText(left.definition, right.definition))

const candidateFieldValue = (candidate, field) => {
  if (field === 'name' || field === 'definition') return normalizeText(candidate?.[field])
  if (field === 'dimensions') return normalizeDimensions(candidate?.payload?.dimensions)
  if (field === 'items') return normalizeItems(candidate?.payload?.items)
  return normalizeText(candidate?.payload?.[field])
}

const comparableValue = value => JSON.stringify(value)

export function buildCandidateVariantDifferences(family) {
  const variants = family?.variants || []
  if (variants.length < 2) return []

  return (FIELD_ORDER[family.candidate_type] || FIELD_ORDER.glossary)
    .map(field => ({
      field,
      values: variants.map((group, index) => ({
        variant_index: index + 1,
        semantic_fingerprint: group.semantic_fingerprint,
        value: candidateFieldValue(group.candidate, field)
      }))
    }))
    .filter(difference => new Set(difference.values.map(item => comparableValue(item.value))).size > 1)
}
