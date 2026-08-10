const clarificationKeys = {
  data_source_not_found: 'develop.workflow.dataSourceNotFound',
  data_source_confirmation_required: 'develop.workflow.dataSourceConfirmationRequired',
  data_source_ambiguous: 'develop.workflow.dataSourceAmbiguous',
  data_source_unverified: 'develop.workflow.dataSourceUnverified',
  resource_facts_required: 'develop.workflow.resourceFactsRequired',
  resource_facts_unverified: 'develop.workflow.resourceFactsUnverified',
  resource_crs_required: 'develop.workflow.resourceCrsRequired'
}

export function resolveWorkflowGenerationResult(result) {
  if (result?.status === 'need_clarification') {
    return {
      workflow: null,
      clarificationKey: clarificationKeys[result.clarification_reason]
        || 'develop.workflow.dataSourceClarificationRequired',
      clarificationReason: result.clarification_reason || null,
      candidates: Array.isArray(result.data_source_candidates)
        ? result.data_source_candidates
        : []
    }
  }

  if (result?.status !== 'success' || !Array.isArray(result.workflow?.tasks)) {
    throw new Error('invalid workflow generation response')
  }

  return {
    workflow: result.workflow,
    clarificationKey: null,
    clarificationReason: null,
    candidates: []
  }
}
