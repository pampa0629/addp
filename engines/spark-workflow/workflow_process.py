"""Parent Runtime dispatches one workflow/direct invocation to a private Driver."""
from pathlib import Path
import sys

from addp_common.workflow_runtime import WorkflowValidationError, validate_workflow_def
from operator_metadata import get_operator_metadata
from query_process import QueryDriverError, run_driver
from spark_connector import cluster_master_url
from system_client import get_engine
from workflow_engine import SQL_RELATION_NOT_FOUND


def _dispatch(engine_id, tenant_id, invocation):
    if any(type(value) is not int or value <= 0 for value in (engine_id, tenant_id)):
        raise ValueError('invalid Spark invocation identity')
    engine = get_engine(engine_id, tenant_id)
    if (type(engine.get('id')) is not int or type(engine.get('tenant_id')) is not int
            or engine.get('id') != engine_id or engine.get('tenant_id') != tenant_id
            or engine.get('engine_type') != 'spark'):
        raise ValueError('Spark cluster identity mismatch')
    connection = engine['connection_info']
    cluster_master_url(connection)
    request = dict(invocation, engine_id=engine_id, tenant_id=tenant_id, connection=connection)
    result = run_driver([sys.executable, str(Path(__file__).with_name('workflow_driver.py'))],
                        request, timeout=600, parse_float=float)
    if result.get('status') not in {'success', 'failed'}:
        raise ValueError('invalid Spark Driver result')
    if result['status'] == 'failed':
        code = 'EXECUTION_FAILED'
        if invocation['mode'] == 'workflow' and result == {'status': 'failed', 'error_code': SQL_RELATION_NOT_FOUND}:
            code = SQL_RELATION_NOT_FOUND
        return {'status': 'failed', 'error_code': code, 'driver_failure': 'failed'}
    return result


def _failure(error):
    reason = 'timeout' if isinstance(error, QueryDriverError) and error.code == 'query_timeout' else 'failed'
    return {'status': 'failed', 'error_code': 'EXECUTION_FAILED', 'driver_failure': reason}


def execute_workflow(engine_id, tenant_id, workflow_def, input_data=None):
    try:
        validate_workflow_def(workflow_def, operator_ids={
            op['id'] for op in get_operator_metadata() if 'workflow' in op['execution_modes']})
        return _dispatch(engine_id, tenant_id, {
            'mode': 'workflow', 'workflow_def': workflow_def, 'input_data': input_data or {}})
    except WorkflowValidationError as error:
        return {'status': 'failed', 'error_code': 'WORKFLOW_INVALID', 'error': str(error)}
    except Exception as error:
        return _failure(error)


def execute_single_operator(engine_id, tenant_id, operator_name, params):
    try:
        metadata = next(op for op in get_operator_metadata() if op['id'] == operator_name)
        if 'direct' not in metadata['execution_modes'] or not isinstance(params, dict):
            raise ValueError('invalid direct invocation')
        return _dispatch(engine_id, tenant_id, {'mode': 'direct', 'operator': operator_name, 'params': params})
    except Exception as error:
        return _failure(error)
