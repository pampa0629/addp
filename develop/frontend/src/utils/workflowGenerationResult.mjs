const clarificationKeys = {
  data_source_not_found: 'develop.workflow.dataSourceNotFound',
  data_source_ambiguous: 'develop.workflow.dataSourceAmbiguous',
  data_source_unverified: 'develop.workflow.dataSourceUnverified'
}

export function resolveWorkflowGenerationResult(result) {
  if (result?.status === 'need_clarification') {
    return {
      workflow: null,
      clarificationKey: clarificationKeys[result.clarification_reason]
        || 'develop.workflow.dataSourceClarificationRequired'
    }
  }

  if (result?.status !== 'success' || !Array.isArray(result.workflow?.tasks)) {
    throw new Error('invalid workflow generation response')
  }

  return {
    workflow: result.workflow,
    clarificationKey: null
  }
}
