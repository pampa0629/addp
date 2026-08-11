const clarificationKeys = {
  data_source_not_found: 'develop.query.dataSourceNotFound',
  data_source_confirmation_required: 'develop.query.dataSourceConfirmationRequired',
  data_source_ambiguous: 'develop.query.dataSourceConfirmationRequired',
  data_source_unverified: 'develop.query.dataSourceUnverified',
  query_language_unsupported: 'develop.query.queryLanguageUnsupported'
}

export function resolveQueryGenerationResult(result) {
  if (result?.status === 'need_clarification') {
    return {
      query: null,
      queryLanguage: result.query_language || '',
      resources: [],
      warnings: [],
      queryParameters: [],
      explanation: '',
      clarificationKey: clarificationKeys[result.clarification_reason]
        || 'develop.query.dataSourceClarificationRequired',
      clarificationReason: result.clarification_reason || null,
      candidates: Array.isArray(result.data_source_candidates)
        ? result.data_source_candidates
        : []
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
    clarificationKey: null,
    clarificationReason: null,
    candidates: []
  }
}
