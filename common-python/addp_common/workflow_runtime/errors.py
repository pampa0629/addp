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
    ('batch_buffer', 'operator_input_invalid'): ('input_gdf', ''),
    ('batch_buffer', 'operator_field_conflict'): ('input_gdf', 'distance'),
    ('batch_buffer', 'operator_number_array_invalid'): ('distances', ''),
    ('batch_buffer', 'operator_positive_integer_required'): ('resolution', ''),
    ('distance_to', 'operator_geometry_field_conflict'): ('input_gdf', 'distance'),
    ('distance_to', 'operator_projected_crs_required'): ('', ''),
    ('distance_to', 'operator_crs_mismatch'): ('', ''),
    ('check_line_proximity', 'operator_positive_number_required'): ('tolerance', ''),
    ('check_line_proximity', 'operator_input_invalid'): ('input_gdf', ''),
    ('check_line_proximity', 'operator_reference_invalid'): ('reference_gdf', ''),
    ('check_line_proximity', 'operator_crs_invalid'): ('', ''),
}


class OperatorInputError(ValueError):
    """A known admission fact; the runner binds its actual node and operator."""

    def __init__(self, reason, message):
        super().__init__(message)
        if reason not in {reason for _, reason in OPERATOR_INPUT_RULES}:
            raise ValueError('unsupported operator input reason')
        self.reason = reason

    def diagnostic(self, operator_id, node_index):
        rule = OPERATOR_INPUT_RULES.get((operator_id, self.reason))
        if rule is None or type(node_index) is not int or not 1 <= node_index <= 65535:
            return None
        parameter, field = rule
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
    rule = OPERATOR_INPUT_RULES.get((context['operator_id'], value['reason']))
    if rule is None: return None
    parameter, field = rule
    expected = {'node_index': context['node_index'], 'operator_id': context['operator_id']}
    if parameter: expected['parameter'] = parameter
    if field: expected['field'] = field
    if context != expected: return None
    return {**value, 'operator': dict(expected)}
