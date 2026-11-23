"""Private stdin/result-file entry point; never starts an HTTP service."""
import json
from pathlib import Path
import sys

from spark_connector import invocation_session
from workflow_engine import SQL_RELATION_NOT_FOUND, run_workflow
from workflow_result import serialize_workflow_value


def execute_invocation(request):
    if request.get('mode') != 'workflow' or set(request) != {'engine_id', 'tenant_id', 'connection', 'mode', 'workflow_def', 'input_data'}:
        raise ValueError('invalid Spark invocation')
    with invocation_session(request['engine_id'], request['tenant_id'], request['connection']):
        result = run_workflow(request['engine_id'], request['tenant_id'],
                              request['workflow_def'], request['input_data'])
        return serialize_workflow_value(result)


def main():
    try:
        request = json.loads(sys.stdin.buffer.read(1024 * 1024 + 1))
        result = execute_invocation(request)
        if result.get('status') != 'success' and result != {'status': 'failed', 'error_code': SQL_RELATION_NOT_FOUND}:
            raise ValueError('Spark invocation failed')
    except Exception:
        result = {'status': 'failed', 'error_code': 'EXECUTION_FAILED'}
    Path(sys.argv[1]).write_text(json.dumps(result, ensure_ascii=False, allow_nan=False), encoding='utf-8')


if __name__ == '__main__':
    main()
