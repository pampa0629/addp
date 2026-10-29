export const failureStages = Object.freeze(['validation', 'authorization', 'preparation', 'execution', 'result'])
export const failureReasons = Object.freeze([
  'configuration_missing', 'configuration_invalid', 'timeout_invalid', 'result_limit_invalid',
  'query_template_invalid', 'relation_binding_invalid', 'authorization_invalid',
  'engine_unavailable', 'engine_mismatch', 'query_syntax_invalid', 'column_not_found',
  'table_not_found', 'ambiguous_column', 'function_or_operator_not_found', 'division_by_zero',
  'type_mismatch', 'permission_denied', 'connection_failed',
  'json_invalid', 'array_invalid', 'bytes_invalid', 'table_write_failed', 'execution_failed',
  'target_exists',
  'operator_input_invalid', 'operator_reference_invalid', 'operator_field_conflict', 'operator_geometry_field_conflict',
  'operator_number_array_invalid', 'operator_positive_integer_required', 'operator_positive_number_required',
  'operator_projected_crs_required', 'operator_crs_mismatch', 'operator_crs_invalid',
  'timeout', 'cancelled', 'resource_exhausted', 'query_failed', 'result_invalid', 'database_required', 'data_protection_required', 'local_read_unsupported',
  'protection_computed_output_unsupported', 'protection_lineage_unverified', 'protection_role_choice_unavailable', 'protection_not_ready',
  'protection_expired', 'protection_not_yet_valid', 'protection_schema_mismatch', 'protection_operation_unsupported'
])

const failureCategories = new Set(['permission_denied', 'timeout', 'cancelled', 'connection_failed',
  'resource_exhausted', 'invalid_input', 'execution_failed', 'submission_uncertain',
  'coordinator_lost', 'child_failed', 'owner_unavailable'])

export function executionFailurePresentation(details, translate) {
  const diagnostic = details?.diagnostic
  const controlled = diagnostic?.schema_version === 'addp.execution-failure/v3'
    && Object.keys(diagnostic).every(key => ['schema_version', 'stage', 'reason', 'sql', 'write', 'target_change_possible', 'operator'].includes(key))
    && failureStages.includes(diagnostic.stage) && failureReasons.includes(diagnostic.reason)
    && validSQLContext(diagnostic.sql, diagnostic.reason)
    && validWriteContext(diagnostic)
    && validOperatorContext(diagnostic)
    && (diagnostic.target_change_possible === undefined || diagnostic.target_change_possible === true
      && ['preparation', 'execution', 'result'].includes(diagnostic.stage))
  const category = failureCategories.has(details?.category) ? details.category : 'execution_failed'
  const rawCode = details?.code || details?.error_code || ''
  const code = typeof rawCode === 'string' && /^[A-Za-z0-9_.:-]{1,128}$/.test(rawCode) && !/secret/i.test(rawCode) ? rawCode : ''
  const operator = controlled ? diagnostic.operator : null
  const parameters = operator ? { field: operator.field || '' } : undefined
  const sql = controlled ? diagnostic.sql : null
  const object = sql?.object ? translate(`common.executionFailure.objects.${sql.object.kind}`, { name: sql.object.name }) : ''
  const position = sql?.position ? translate('common.executionFailure.sourcePosition', sql.position) : ''
  return {
    stage: controlled ? translate(`common.executionFailure.stages.${diagnostic.stage}`) : '',
    reason: translate(controlled ? `common.executionFailure.reasons.${diagnostic.reason}` : `common.executionFailure.${category}`, parameters),
    action: translate(controlled ? `common.executionFailure.actions.${diagnostic.reason}` : 'common.executionFailure.unrecordedAction', parameters),
    code, object, position,
    operator: operator ? translate(`common.executionFailure.operatorNames.${operator.operator_id}`) : '',
    node: operator ? translate('common.executionFailure.operatorNodeIndex', { index: operator.node_index }) : '',
    parameter: operator?.parameter ? translate(`common.executionFailure.operatorParameters.${operator.parameter}`) : '',
    field: operator?.field || '',
    write: controlled && diagnostic.write ? translate('common.executionFailure.writeColumn', diagnostic.write) : '',
    targetWarning: controlled && diagnostic.target_change_possible === true ? translate('common.executionFailure.targetChangePossible') : ''
  }
}

export const operatorInputRules = Object.freeze({
  batch_buffer: { operator_input_invalid:['input_gdf',''], operator_field_conflict:['input_gdf','distance'],
    operator_number_array_invalid:['distances',''], operator_positive_integer_required:['resolution',''] },
  distance_to: { operator_geometry_field_conflict:['input_gdf','distance'], operator_projected_crs_required:['',''], operator_crs_mismatch:['',''] },
  check_line_proximity: { operator_positive_number_required:['tolerance',''], operator_input_invalid:['input_gdf',''],
    operator_reference_invalid:['reference_gdf',''], operator_crs_invalid:['',''] }
})

function validOperatorContext(diagnostic) {
  if (!diagnostic.reason.startsWith('operator_')) return diagnostic.operator === undefined
  const value = diagnostic.operator
  if (diagnostic.stage !== 'execution' || diagnostic.sql !== undefined || diagnostic.write !== undefined ||
      !value || typeof value !== 'object' || Array.isArray(value) ||
      Object.keys(value).some(key => !['node_index','operator_id','parameter','field'].includes(key)) ||
      !Number.isInteger(value.node_index) || value.node_index < 1 || value.node_index > 65535) return false
  const rules = Object.hasOwn(operatorInputRules, value.operator_id) ? operatorInputRules[value.operator_id] : null
  const rule = rules && Object.hasOwn(rules, diagnostic.reason) ? rules[diagnostic.reason] : null
  if (!rule) return false
  return (rule[0] ? value.parameter === rule[0] : value.parameter === undefined) &&
    (rule[1] ? value.field === rule[1] : value.field === undefined)
}

function validWriteContext(diagnostic) {
  const write = diagnostic.write
  if (write === undefined) return true
  const types = { json_invalid: 'json', array_invalid: 'array', bytes_invalid: 'bytes' }
  return write !== null && typeof write === 'object' && !Array.isArray(write)
    && Object.keys(write).length === 2 && diagnostic.sql === undefined && diagnostic.stage === 'execution'
    && Number.isInteger(write.column_index) && write.column_index >= 1 && write.column_index <= 65535
    && Object.hasOwn(types, diagnostic.reason) && write.declared_type === types[diagnostic.reason]
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
