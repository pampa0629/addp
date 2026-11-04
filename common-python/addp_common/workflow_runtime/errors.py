class WorkflowValidationError(ValueError):
    """工作流定义不符合 addp.workflow/v1。"""


class WorkflowExecutionError(RuntimeError):
    """工作流执行阶段失败。"""

    def __init__(self, message, *, error_code="EXECUTION_FAILED", partial_result=None, all_results=None, task_order=None, diagnostic=None):
        super().__init__(message)
        if error_code not in {"EXECUTION_FAILED", "TARGET_ALREADY_EXISTS", "INVALID_PARAMS"}:
            raise ValueError("unsupported workflow execution error code")
        self.diagnostic = validated_operator_diagnostic(diagnostic) if error_code == "INVALID_PARAMS" else None
        self.error_code = error_code
        self.partial_result = partial_result
        self.all_results = all_results
        self.task_order = task_order or []


# Closed protocol tuples; never classify arbitrary exception text as input errors.
OPERATOR_INPUT_RULES = {
    ('extract_vertices', 'operator_input_invalid', ''): 'input_gdf',
    ('extract_vertices', 'operator_crs_invalid', ''): '',
    ('extract_vertices', 'operator_field_conflict', 'source_id'): 'input_gdf',
    ('extract_vertices', 'operator_field_conflict', 'part_no'): 'input_gdf',
    ('extract_vertices', 'operator_field_conflict', 'ring_no'): 'input_gdf',
    ('extract_vertices', 'operator_field_conflict', 'vertex_no'): 'input_gdf',
    ('extract_vertices', 'operator_field_conflict', 'vertex_id'): 'input_gdf',

    ('split_multipart', 'operator_input_invalid', ''): 'input_gdf',
    ('split_multipart', 'operator_crs_invalid', ''): '',
    ('split_multipart', 'operator_field_conflict', 'source_id'): 'input_gdf',
    ('split_multipart', 'operator_field_conflict', 'part_no'): 'input_gdf',

    ('get_bounds', 'operator_input_invalid', ''): 'input_gdf',
    ('get_bounds', 'operator_crs_invalid', ''): '',
    ('get_bounds', 'operator_geometry_field_conflict', 'minx'): 'input_gdf',
    ('get_bounds', 'operator_geometry_field_conflict', 'miny'): 'input_gdf',
    ('get_bounds', 'operator_geometry_field_conflict', 'maxx'): 'input_gdf',
    ('get_bounds', 'operator_geometry_field_conflict', 'maxy'): 'input_gdf',

    ('get_area', 'operator_input_invalid', ''): 'input_gdf',
    ('get_area', 'operator_crs_invalid', ''): '',
    ('get_area', 'operator_geometry_field_conflict', 'area'): 'input_gdf',
    ('get_length', 'operator_input_invalid', ''): 'input_gdf',
    ('get_length', 'operator_crs_invalid', ''): '',
    ('get_length', 'operator_geometry_field_conflict', 'length'): 'input_gdf',
    ('split_by_area', 'operator_input_invalid', ''): 'input_gdf',
    ('split_by_area', 'operator_crs_invalid', ''): '',
    ('split_by_area', 'operator_geometry_field_conflict', 'area'): 'input_gdf',
    ('split_by_area', 'operator_nonnegative_number_required', ''): 'threshold',
    ('envelope', 'operator_input_invalid', ''): 'input_gdf',
    ('envelope', 'operator_crs_invalid', ''): '',
    ('representative_point', 'operator_input_invalid', ''): 'input_gdf',
    ('representative_point', 'operator_crs_invalid', ''): '',
    ('centroid', 'operator_input_invalid', ''): 'input_gdf',
    ('centroid', 'operator_crs_invalid', ''): '',
    ('convex_hull', 'operator_input_invalid', ''): 'input_gdf',
    ('convex_hull', 'operator_crs_invalid', ''): '',
    ('minimum_rotated_rectangle', 'operator_input_invalid', ''): 'input_gdf',
    ('minimum_rotated_rectangle', 'operator_crs_invalid', ''): '',
    ('simplify', 'operator_input_invalid', ''): 'input_gdf',
    ('simplify', 'operator_crs_invalid', ''): '',
    ('simplify', 'operator_nonnegative_number_required', ''): 'tolerance',
    ('simplify', 'operator_boolean_required', ''): 'preserve_topology',
    ('buffer', 'operator_input_invalid', ''): 'input_gdf',
    ('buffer', 'operator_crs_invalid', ''): '',
    ('buffer', 'operator_number_invalid', ''): 'distance',
    ('buffer', 'operator_positive_integer_required', ''): 'resolution',
    ('batch_buffer', 'operator_crs_invalid', ''): '',
    ('voronoi', 'operator_input_invalid', ''): 'input_gdf',
    ('voronoi', 'operator_mask_invalid', ''): 'mask_gdf',
    ('voronoi', 'operator_crs_invalid', ''): '',
    ('contains', 'operator_input_invalid', ''): 'input_gdf',
    ('contains', 'operator_reference_invalid', ''): 'gdf_b',
    ('contains', 'operator_crs_invalid', ''): '',
    ('intersects', 'operator_input_invalid', ''): 'input_gdf',
    ('intersects', 'operator_reference_invalid', ''): 'gdf_b',
    ('intersects', 'operator_crs_invalid', ''): '',
    ('dissolve', 'operator_input_invalid', ''): 'input_gdf',
    ('dissolve', 'operator_crs_invalid', ''): '',
    ('dissolve', 'operator_group_fields_invalid', ''): 'group_fields',
    ('dissolve', 'operator_dissolve_fields_conflict', ''): '',

    ('union', 'operator_input_invalid', ''): 'input_gdf',
    ('union', 'operator_reference_invalid', ''): 'gdf_b',
    ('union', 'operator_crs_invalid', ''): '',
    ('union', 'operator_overlay_fields_conflict', ''): '',
    ('union', 'operator_input_overlap', ''): 'input_gdf',
    ('union', 'operator_reference_overlap', ''): 'gdf_b',

    ('clip', 'operator_input_invalid', ''): 'input_gdf',
    ('clip', 'operator_mask_invalid', ''): 'mask_gdf',
    ('clip', 'operator_crs_invalid', ''): '',

    ('difference', 'operator_input_invalid', ''): 'input_gdf',
    ('difference', 'operator_mask_invalid', ''): 'gdf_b',
    ('difference', 'operator_crs_invalid', ''): '',

    ('intersection', 'operator_input_invalid', ''): 'input_gdf',
    ('intersection', 'operator_reference_invalid', ''): 'gdf_b',
    ('intersection', 'operator_crs_invalid', ''): '',
    ('intersection', 'operator_overlay_fields_conflict', ''): '',

    ('extract_line_nodes', 'operator_input_invalid', ''): 'input_gdf',
    ('extract_line_nodes', 'operator_projected_crs_required', ''): '',
    ('extract_line_nodes', 'operator_endpoint_network_invalid', ''): 'input_gdf',
    ('extract_line_nodes', 'operator_field_conflict', 'node_id'): 'input_gdf',
    ('extract_line_nodes', 'operator_field_conflict', 'degree'): 'input_gdf',
    ('extract_line_nodes', 'operator_field_conflict', 'source_id'): 'input_gdf',
    ('extract_line_nodes', 'operator_field_conflict', 'source_part_no'): 'input_gdf',
    ('extract_line_nodes', 'operator_field_conflict', 'end'): 'input_gdf',

    ('trim_line_endpoints', 'operator_distance_limit_invalid', ''): 'max_distance',
    ('trim_line_endpoints', 'operator_input_invalid', ''): 'input_gdf',
    ('trim_line_endpoints', 'operator_reference_invalid', ''): 'reference_gdf',
    ('trim_line_endpoints', 'operator_crs_invalid', ''): '',
    ('trim_line_endpoints', 'operator_source_topology_invalid', ''): 'input_gdf',
    ('trim_line_endpoints', 'operator_trim_conflict', ''): '',

    ('batch_buffer', 'operator_input_invalid', ''): 'input_gdf',
    ('batch_buffer', 'operator_field_conflict', 'distance'): 'input_gdf',
    ('batch_buffer', 'operator_number_array_invalid', ''): 'distances',
    ('batch_buffer', 'operator_positive_integer_required', ''): 'resolution',
    ('distance_to', 'operator_input_invalid', ''): 'input_gdf',
    ('distance_to', 'operator_reference_invalid', ''): 'target_geom',
    ('distance_to', 'operator_geometry_field_conflict', 'distance'): 'input_gdf',
    ('distance_to', 'operator_projected_crs_required', ''): '',
    ('distance_to', 'operator_crs_mismatch', ''): '',
    ('check_line_proximity', 'operator_positive_number_required', ''): 'tolerance',
    ('check_line_proximity', 'operator_input_invalid', ''): 'input_gdf',
    ('check_line_proximity', 'operator_reference_invalid', ''): 'reference_gdf',
    ('check_line_proximity', 'operator_crs_invalid', ''): '',
    ('extract_polygon_rings', 'operator_field_conflict', 'source_id'): 'input_gdf',
    ('extract_polygon_rings', 'operator_field_conflict', 'part_no'): 'input_gdf',
    ('extract_polygon_rings', 'operator_field_conflict', 'ring_no'): 'input_gdf',
    ('extract_polygon_rings', 'operator_field_conflict', 'ring_id'): 'input_gdf',
    ('extract_polygon_rings', 'operator_field_conflict', 'is_hole'): 'input_gdf',
    ('extract_polygon_rings', 'operator_geometry_invalid', ''): 'input_gdf',
}


class OperatorInputError(ValueError):
    """A known admission fact; the runner binds its actual node and operator."""

    def __init__(self, reason, message, *, field=""):
        super().__init__(message)
        if reason not in {reason for _, reason, _ in OPERATOR_INPUT_RULES}:
            raise ValueError('unsupported operator input reason')
        self.reason = reason
        self.field = field

    def diagnostic(self, operator_id, node_index):
        if not isinstance(self.field, str):
            return None
        rule = OPERATOR_INPUT_RULES.get((operator_id, self.reason, self.field))
        if rule is None or type(node_index) is not int or not 1 <= node_index <= 65535:
            return None
        parameter, field = rule, self.field
        context = {'node_index': node_index, 'operator_id': operator_id}
        if parameter: context['parameter'] = parameter
        if field: context['field'] = field
        return {'schema_version': 'addp.execution-failure/v3', 'stage': 'execution',
                'reason': self.reason, 'operator': context}


def validated_operator_diagnostic(value):
    if not isinstance(value, dict) or set(value) != {'schema_version', 'stage', 'reason', 'operator'}:
        return None
    if value['schema_version'] != 'addp.execution-failure/v3' or value['stage'] != 'execution':
        return None
    context = value['operator']
    if not isinstance(context, dict) or not {'node_index', 'operator_id'} <= set(context) or not set(context) <= {'node_index', 'operator_id', 'parameter', 'field'}:
        return None
    if type(context['node_index']) is not int or not 1 <= context['node_index'] <= 65535:
        return None
    if not isinstance(context['operator_id'], str) or not isinstance(value['reason'], str): return None
    field = context.get('field', '')
    if not isinstance(field, str): return None
    rule = OPERATOR_INPUT_RULES.get((context['operator_id'], value['reason'], field))
    if rule is None: return None
    parameter = rule
    expected = {'node_index': context['node_index'], 'operator_id': context['operator_id']}
    if parameter: expected['parameter'] = parameter
    if field: expected['field'] = field
    if context != expected: return None
    return {**value, 'operator': dict(expected)}
