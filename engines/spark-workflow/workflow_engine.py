"""Spark domain operators over the shared addp.workflow/v1 runtime core."""
import logging
from typing import Any

from addp_common.workflow_runtime import WorkflowRunner, WorkflowValidationError
from operator_metadata import get_operator_metadata
from operators import get_operator
from spark_connector import get_spark_connector

logger = logging.getLogger(__name__)


def _execute_operator(engine_id: int, tenant_id: int, operator: str, params: dict[str, Any]):
    params = dict(params)
    if operator in ('load', 'sql'):
        params.update(engine_id=engine_id, tenant_id=tenant_id)
    elif operator == 'save':
        params['engine_id'] = engine_id
    return get_operator(operator)(**params)


class SparkWorkflowRunner(WorkflowRunner):
    def __init__(self, engine_id: int, tenant_id: int):
        self.engine_id = engine_id
        self.tenant_id = tenant_id
        self._metadata = {op['id']: op for op in get_operator_metadata() if 'workflow' in op['execution_modes']}
        super().__init__(set(self._metadata), self._execute_workflow_operator)

    def _execute_workflow_operator(self, operator, params):
        result = _execute_operator(self.engine_id, self.tenant_id, operator, params)
        ports = [port['name'] for port in self._metadata[operator]['output_ports']]
        if len(ports) == 1:
            return {'__ports__': {ports[0]: result}}
        if not isinstance(result, dict) or set(result) != set(ports):
            raise ValueError(f'{operator} must return its declared output ports')
        return {'__ports__': result}


def execute_workflow(engine_id: int, tenant_id: int, workflow_def: dict[str, Any], input_data=None):
    try:
        result = SparkWorkflowRunner(engine_id, tenant_id).execute(workflow_def, input_data)
        return {'status': 'success', 'final_result': result.final_result,
                'all_results': result.all_results, 'task_order': result.task_order,
                'message': f'工作流执行成功，共 {len(result.task_order)} 个任务'}
    except WorkflowValidationError as exc:
        return {'status': 'failed', 'error_code': 'WORKFLOW_INVALID', 'error': str(exc)}
    except Exception as exc:
        logger.exception('Workflow execution failed')
        return {'status': 'failed', 'error_code': 'EXECUTION_FAILED', 'error': str(exc)}


def execute_single_operator(engine_id: int, tenant_id: int, operator_name: str, params: dict[str, Any]):
    try:
        get_spark_connector().get_or_create_session(engine_id, tenant_id)
        result = _execute_operator(engine_id, tenant_id, operator_name, params)
        return {'status': 'success', 'result': result}
    except Exception as exc:
        logger.exception('Operator execution failed')
        return {'status': 'failed', 'error': str(exc)}
