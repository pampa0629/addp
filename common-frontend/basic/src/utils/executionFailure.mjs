export const failureStages = Object.freeze(['validation', 'authorization', 'preparation', 'execution', 'result'])
export const failureReasons = Object.freeze([
  'configuration_missing', 'configuration_invalid', 'timeout_invalid', 'result_limit_invalid',
  'query_template_invalid', 'relation_binding_invalid', 'authorization_invalid',
  'engine_unavailable', 'engine_mismatch', 'query_syntax_invalid', 'column_not_found',
  'table_not_found', 'ambiguous_column', 'function_or_operator_not_found', 'division_by_zero',
  'type_mismatch', 'permission_denied', 'connection_failed',
  'timeout', 'cancelled', 'resource_exhausted', 'query_failed', 'result_invalid', 'database_required', 'data_protection_required'
])

const failureCategories = new Set(['permission_denied', 'timeout', 'cancelled', 'connection_failed',
  'resource_exhausted', 'invalid_input', 'execution_failed', 'submission_uncertain',
  'coordinator_lost', 'child_failed', 'owner_unavailable'])

export function executionFailurePresentation(details, translate) {
  const diagnostic = details?.diagnostic
  const controlled = diagnostic?.schema_version === 'addp.execution-failure/v2'
    && Object.keys(diagnostic).every(key => ['schema_version', 'stage', 'reason', 'sql'].includes(key))
    && failureStages.includes(diagnostic.stage) && failureReasons.includes(diagnostic.reason)
    && validSQLContext(diagnostic.sql, diagnostic.reason)
  const category = failureCategories.has(details?.category) ? details.category : 'execution_failed'
  const rawCode = details?.code || details?.error_code || ''
  const code = typeof rawCode === 'string' && /^[A-Za-z0-9_.:-]{1,128}$/.test(rawCode) && !/secret/i.test(rawCode) ? rawCode : ''
  const sql = controlled ? diagnostic.sql : null
  const object = sql?.object ? translate(`common.executionFailure.objects.${sql.object.kind}`, { name: sql.object.name }) : ''
  const position = sql?.position ? translate('common.executionFailure.sourcePosition', sql.position) : ''
  return {
    stage: controlled ? translate(`common.executionFailure.stages.${diagnostic.stage}`) : '',
    reason: translate(controlled ? `common.executionFailure.reasons.${diagnostic.reason}` : `common.executionFailure.${category}`),
    action: translate(controlled ? `common.executionFailure.actions.${diagnostic.reason}` : 'common.executionFailure.unrecordedAction'),
    code, object, position
  }
}

function validSQLContext(sql, reason) {
  if (sql === undefined) return true
  if (!sql || typeof sql !== 'object' || Array.isArray(sql) || !Object.keys(sql).length || Object.keys(sql).some(key => !['object', 'position'].includes(key))) return false
  const allowed = { table_not_found: ['table'], column_not_found: ['column'], ambiguous_column: ['column'],
    function_or_operator_not_found: ['function', 'operator'], query_syntax_invalid: [], type_mismatch: [], division_by_zero: [] }
  if (!Object.hasOwn(allowed, reason)) return false
  if (sql.object !== undefined) {
    const object = sql.object
    if (!object || Object.keys(object).length !== 2 || !allowed[reason].includes(object.kind) || typeof object.name !== 'string'
      || !object.name.length || [...object.name].length > 256 || new TextEncoder().encode(object.name).length > 512
      || /[\p{Cc}\p{Cf}\p{Cs}]/u.test(object.name)) return false
  }
  if (sql.position !== undefined) {
    const position = sql.position
    if (!position || Object.keys(position).length !== 2 || !['line', 'column'].every(key => Number.isInteger(position[key]) && position[key] >= 1 && position[key] <= 1000000)) return false
  }
  return true
}

export function buildDevelopQueryExecutionRoute(execution) {
  if (execution?.module !== 'develop' || execution?.task_type !== 'query' || !execution?.execution_id) return ''
  return `/develop/executions/${encodeURIComponent(execution.execution_id)}`
}
