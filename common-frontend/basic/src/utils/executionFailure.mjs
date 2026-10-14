export const failureStages = Object.freeze(['validation', 'authorization', 'preparation', 'execution', 'result'])
export const failureReasons = Object.freeze([
  'configuration_missing', 'configuration_invalid', 'timeout_invalid', 'result_limit_invalid',
  'query_template_invalid', 'relation_binding_invalid', 'authorization_invalid',
  'engine_unavailable', 'engine_mismatch', 'query_syntax_invalid', 'column_not_found',
  'table_not_found', 'ambiguous_column', 'function_or_operator_not_found', 'division_by_zero',
  'type_mismatch', 'permission_denied', 'connection_failed',
  'timeout', 'cancelled', 'resource_exhausted', 'query_failed', 'result_invalid', 'database_required'
])

const failureCategories = new Set(['permission_denied', 'timeout', 'cancelled', 'connection_failed',
  'resource_exhausted', 'invalid_input', 'execution_failed', 'submission_uncertain',
  'coordinator_lost', 'child_failed', 'owner_unavailable'])

export function executionFailurePresentation(details, translate) {
  const diagnostic = details?.diagnostic
  const controlled = diagnostic?.schema_version === 'addp.execution-failure/v1'
    && Object.keys(diagnostic).length === 3
    && failureStages.includes(diagnostic.stage) && failureReasons.includes(diagnostic.reason)
  const category = failureCategories.has(details?.category) ? details.category : 'execution_failed'
  const rawCode = details?.code || details?.error_code || ''
  const code = typeof rawCode === 'string' && /^[A-Za-z0-9_.:-]{1,128}$/.test(rawCode) && !/secret/i.test(rawCode) ? rawCode : ''
  return {
    stage: controlled ? translate(`common.executionFailure.stages.${diagnostic.stage}`) : '',
    reason: translate(controlled ? `common.executionFailure.reasons.${diagnostic.reason}` : `common.executionFailure.${category}`),
    action: translate(controlled ? `common.executionFailure.actions.${diagnostic.reason}` : 'common.executionFailure.unrecordedAction'),
    code
  }
}

export function buildDevelopQueryExecutionRoute(execution) {
  if (execution?.module !== 'develop' || execution?.task_type !== 'query' || !execution?.execution_id) return ''
  return `/develop/executions/${encodeURIComponent(execution.execution_id)}`
}
