export const failureStages = Object.freeze(['validation', 'authorization', 'preparation', 'execution', 'result'])
export const failureReasons = Object.freeze([
  'configuration_missing', 'configuration_invalid', 'timeout_invalid', 'result_limit_invalid',
  'query_template_invalid', 'relation_binding_invalid', 'authorization_invalid',
  'engine_unavailable', 'engine_mismatch', 'query_syntax_invalid', 'column_not_found',
  'table_not_found', 'ambiguous_column', 'function_or_operator_not_found', 'division_by_zero',
  'type_mismatch', 'permission_denied', 'connection_failed',
  'json_invalid', 'array_invalid', 'bytes_invalid', 'table_write_failed', 'execution_failed',
  'target_exists',
  'operator_group_fields_invalid', 'operator_dissolve_fields_conflict',
  'operator_input_overlap', 'operator_reference_overlap',
  'operator_overlay_fields_conflict', 'operator_endpoint_network_invalid', 'operator_distance_limit_invalid', 'operator_source_topology_invalid', 'operator_trim_conflict',
  'operator_input_invalid', 'operator_mask_invalid', 'operator_reference_invalid', 'operator_field_conflict', 'operator_geometry_field_conflict',
  'operator_number_invalid', 'operator_nonnegative_number_required', 'operator_boolean_required', 'operator_number_array_invalid', 'operator_positive_integer_required', 'operator_positive_number_required',
  'operator_projected_crs_required', 'operator_crs_mismatch', 'operator_crs_invalid', 'operator_geometry_invalid',
  'timeout', 'cancelled', 'resource_exhausted', 'query_failed', 'result_invalid', 'database_required', 'data_protection_required', 'local_read_unsupported',
  'protection_computed_output_unsupported', 'protection_lineage_unverified', 'protection_role_choice_unavailable', 'protection_not_ready',
  'protection_expired', 'protection_not_yet_valid', 'protection_schema_mismatch', 'protection_operation_unsupported'
])

const failureCategories = new Set(['permission_denied', 'timeout', 'cancelled', 'connection_failed',
  'resource_exhausted', 'invalid_input', 'execution_failed', 'submission_uncertain',
  'coordinator_lost', 'child_failed', 'owner_unavailable'])

const operatorFailureTemplates = Object.freeze({ contains: 'spatialRelation', intersects: 'spatialRelation', voronoi: 'voronoi',
  centroid: 'construction', convex_hull: 'construction', minimum_rotated_rectangle: 'construction',
  'buffer:operator_input_invalid': 'buffer', 'buffer:operator_crs_invalid': 'buffer',
  'batch_buffer:operator_crs_invalid': 'buffer',
  'simplify:operator_input_invalid': 'simplify', 'simplify:operator_crs_invalid': 'simplify' })

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
  const profile = operator && (operatorFailureTemplates[`${operator.operator_id}:${diagnostic.reason}`] || operatorFailureTemplates[operator.operator_id])
  const templates = `common.executionFailure${profile ? `.${profile}` : ''}`
  const sql = controlled ? diagnostic.sql : null
  const object = sql?.object ? translate(`common.executionFailure.objects.${sql.object.kind}`, { name: sql.object.name }) : ''
  const position = sql?.position ? translate('common.executionFailure.sourcePosition', sql.position) : ''
  return {
    stage: controlled ? translate(`common.executionFailure.stages.${diagnostic.stage}`) : '',
    reason: translate(controlled ? `${templates}.reasons.${diagnostic.reason}` : `common.executionFailure.${category}`, parameters),
    action: translate(controlled ? `${templates}.actions.${diagnostic.reason}` : 'common.executionFailure.unrecordedAction', parameters),
    code, object, position,
    operator: operator ? translate(`common.executionFailure.operatorNames.${operator.operator_id}`) : '',
    node: operator ? translate('common.executionFailure.operatorNodeIndex', { index: operator.node_index }) : '',
    parameter: operator?.parameter ? translate(`common.executionFailure.operatorParameters.${operator.parameter}`) : '',
    field: operator?.field || '',
    write: controlled && diagnostic.write ? translate('common.executionFailure.writeColumn', diagnostic.write) : '',
    targetWarning: controlled && diagnostic.target_change_possible === true ? translate('common.executionFailure.targetChangePossible') : ''
  }
}

export const operatorInputRules = Object.freeze([
  ["centroid", "operator_input_invalid", "input_gdf", ""],
  ["centroid", "operator_crs_invalid", "", ""],
  ["convex_hull", "operator_input_invalid", "input_gdf", ""],
  ["convex_hull", "operator_crs_invalid", "", ""],
  ["minimum_rotated_rectangle", "operator_input_invalid", "input_gdf", ""],
  ["minimum_rotated_rectangle", "operator_crs_invalid", "", ""],
  ["simplify", "operator_input_invalid", "input_gdf", ""],
  ["simplify", "operator_crs_invalid", "", ""],
  ["simplify", "operator_nonnegative_number_required", "tolerance", ""],
  ["simplify", "operator_boolean_required", "preserve_topology", ""],
  ["buffer", "operator_input_invalid", "input_gdf", ""],
  ["buffer", "operator_crs_invalid", "", ""],
  ["buffer", "operator_number_invalid", "distance", ""],
  ["buffer", "operator_positive_integer_required", "resolution", ""],
  ["batch_buffer", "operator_crs_invalid", "", ""],
  ["voronoi", "operator_input_invalid", "input_gdf", ""],
  ["voronoi", "operator_mask_invalid", "mask_gdf", ""],
  ["voronoi", "operator_crs_invalid", "", ""],
  ["contains", "operator_input_invalid", "input_gdf", ""],
  ["contains", "operator_reference_invalid", "gdf_b", ""],
  ["contains", "operator_crs_invalid", "", ""],
  ["intersects", "operator_input_invalid", "input_gdf", ""],
  ["intersects", "operator_reference_invalid", "gdf_b", ""],
  ["intersects", "operator_crs_invalid", "", ""],
  ["dissolve", "operator_input_invalid", "input_gdf", ""],
  ["dissolve", "operator_crs_invalid", "", ""],
  ["dissolve", "operator_group_fields_invalid", "group_fields", ""],
  ["dissolve", "operator_dissolve_fields_conflict", "", ""],

  ["union", "operator_input_invalid", "input_gdf", ""],
  ["union", "operator_reference_invalid", "gdf_b", ""],
  ["union", "operator_crs_invalid", "", ""],
  ["union", "operator_overlay_fields_conflict", "", ""],
  ["union", "operator_input_overlap", "input_gdf", ""],
  ["union", "operator_reference_overlap", "gdf_b", ""],

  ["clip", "operator_input_invalid", "input_gdf", ""],
  ["clip", "operator_mask_invalid", "mask_gdf", ""],
  ["clip", "operator_crs_invalid", "", ""],
  ["difference", "operator_input_invalid", "input_gdf", ""],
  ["difference", "operator_mask_invalid", "gdf_b", ""],
  ["difference", "operator_crs_invalid", "", ""],
  ["intersection", "operator_input_invalid", "input_gdf", ""],
  ["intersection", "operator_reference_invalid", "gdf_b", ""],
  ["intersection", "operator_crs_invalid", "", ""],
  ["intersection", "operator_overlay_fields_conflict", "", ""],
  ["extract_line_nodes", "operator_input_invalid", "input_gdf", ""],
  ["extract_line_nodes", "operator_projected_crs_required", "", ""],
  ["extract_line_nodes", "operator_endpoint_network_invalid", "input_gdf", ""],
  ["extract_line_nodes", "operator_field_conflict", "input_gdf", "node_id"],
  ["extract_line_nodes", "operator_field_conflict", "input_gdf", "degree"],
  ["extract_line_nodes", "operator_field_conflict", "input_gdf", "source_id"],
  ["extract_line_nodes", "operator_field_conflict", "input_gdf", "source_part_no"],
  ["extract_line_nodes", "operator_field_conflict", "input_gdf", "end"],
  ["trim_line_endpoints", "operator_distance_limit_invalid", "max_distance", ""],
  ["trim_line_endpoints", "operator_input_invalid", "input_gdf", ""],
  ["trim_line_endpoints", "operator_reference_invalid", "reference_gdf", ""],
  ["trim_line_endpoints", "operator_crs_invalid", "", ""],
  ["trim_line_endpoints", "operator_source_topology_invalid", "input_gdf", ""],
  ["trim_line_endpoints", "operator_trim_conflict", "", ""],

  ["batch_buffer", "operator_input_invalid", "input_gdf", ""],
  ["batch_buffer", "operator_field_conflict", "input_gdf", "distance"],
  ["batch_buffer", "operator_number_array_invalid", "distances", ""],
  ["batch_buffer", "operator_positive_integer_required", "resolution", ""],
  ["distance_to", "operator_geometry_field_conflict", "input_gdf", "distance"],
  ["distance_to", "operator_projected_crs_required", "", ""],
  ["distance_to", "operator_crs_mismatch", "", ""],
  ["check_line_proximity", "operator_positive_number_required", "tolerance", ""],
  ["check_line_proximity", "operator_input_invalid", "input_gdf", ""],
  ["check_line_proximity", "operator_reference_invalid", "reference_gdf", ""],
  ["check_line_proximity", "operator_crs_invalid", "", ""],
  ["extract_polygon_rings", "operator_field_conflict", "input_gdf", "source_id"],
  ["extract_polygon_rings", "operator_field_conflict", "input_gdf", "part_no"],
  ["extract_polygon_rings", "operator_field_conflict", "input_gdf", "ring_no"],
  ["extract_polygon_rings", "operator_field_conflict", "input_gdf", "ring_id"],
  ["extract_polygon_rings", "operator_field_conflict", "input_gdf", "is_hole"],
  ["extract_polygon_rings", "operator_geometry_invalid", "input_gdf", ""],
].map(Object.freeze))

function validOperatorContext(diagnostic) {
  if (!diagnostic.reason.startsWith('operator_')) return diagnostic.operator === undefined
  const value = diagnostic.operator
  if (diagnostic.stage !== 'execution' || diagnostic.sql !== undefined || diagnostic.write !== undefined ||
      !value || typeof value !== 'object' || Array.isArray(value) ||
      Object.keys(value).some(key => !['node_index','operator_id','parameter','field'].includes(key)) ||
      !Number.isInteger(value.node_index) || value.node_index < 1 || value.node_index > 65535) return false
  return operatorInputRules.some(([operator, reason, parameter, field]) =>
    value.operator_id === operator && diagnostic.reason === reason &&
    (parameter ? value.parameter === parameter : value.parameter === undefined) &&
    (field ? value.field === field : value.field === undefined))
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
