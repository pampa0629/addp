const clarificationControls = new Set([
  'single_choice',
  'multiple_choice',
  'text',
  'resource_choice',
  'notice'
])

function normalizeClarification(clarification) {
  const key = String(clarification?.key || '').trim()
  const prompt = String(clarification?.prompt || '').trim()
  const control = String(clarification?.control || '').trim()
  if (!key || !prompt || !clarificationControls.has(control)) {
    throw new Error('invalid query clarification response')
  }
  const options = Array.isArray(clarification.options)
    ? clarification.options.map(option => {
        const value = String(option?.value || '').trim()
        const label = String(option?.label || '').trim()
        if (!value || !label) throw new Error('invalid query clarification option')
        return {
          value,
          label,
          description: String(option?.description || '').trim()
        }
      })
    : []
  const resourceCandidates = Array.isArray(clarification.resource_candidates)
    ? clarification.resource_candidates
    : []
  if (['single_choice', 'multiple_choice'].includes(control) && options.length === 0) {
    throw new Error('invalid query clarification options')
  }
  if (control === 'resource_choice' && resourceCandidates.length === 0) {
    throw new Error('invalid query resource clarification')
  }
  return {
    key,
    category: String(clarification.category || '').trim(),
    prompt,
    control,
    required: clarification.required !== false,
    options,
    resourceCandidates
  }
}

export function resolveQueryGenerationResult(result) {
  if (result?.status === 'need_clarification') {
    if (!Array.isArray(result.clarifications) || result.clarifications.length === 0) {
      throw new Error('invalid query clarification response')
    }
    return {
      query: null,
      queryLanguage: String(result.query_language || '').trim().toLowerCase(),
      resources: Array.isArray(result.resources) ? result.resources : [],
      warnings: [],
      queryParameters: [],
      explanation: '',
      clarifications: result.clarifications.map(normalizeClarification)
    }
  }

  if (result?.status !== 'success' || !String(result.query || '').trim()) {
    throw new Error('invalid query generation response')
  }
  if (!Array.isArray(result.query_parameters)) {
    throw new Error('invalid query generation response')
  }

  return {
    query: String(result.query).trim(),
    queryLanguage: String(result.query_language || '').trim().toLowerCase(),
    resources: Array.isArray(result.resources) ? result.resources : [],
    warnings: Array.isArray(result.warnings) ? result.warnings : [],
    queryParameters: result.query_parameters.map(parameter => ({
      name: parameter.name,
      type: parameter.type,
      default: parameter.default,
      ...(parameter.title ? { title: parameter.title } : {}),
      ...(parameter.description ? { description: parameter.description } : {})
    })),
    explanation: String(result.explanation || '').trim(),
    clarifications: []
  }
}
