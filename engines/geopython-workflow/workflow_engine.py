"""GeoPython domain executor over the shared workflow runtime core."""
from __future__ import annotations

import json
import logging
import geopandas as gpd
import pandas as pd
from addp_common.workflow_runtime import WorkflowRunner, WorkflowExecutionError, WorkflowValidationError
from operators import OPERATORS, get_operator
from operators.raster_compute import RasterDataset, RasterTiles, raster_info, raster_tiles_info, raster_workspace, raster_error_language

logger = logging.getLogger(__name__)


def _execute_operator(operator, params):
    params = dict(params)
    schemas = {p['name']: p for p in OPERATORS[operator].get('param_schema', [])}
    for name, value in params.items():
        if str(schemas.get(name, {}).get('type', '')).lower() != 'geodataframe' or isinstance(value, gpd.GeoDataFrame):
            continue
        if isinstance(value, str):
            value = json.loads(value)
        if not isinstance(value, dict) or value.get('type') != 'FeatureCollection' or not isinstance(value.get('features'), list):
            raise ValueError(f'{name} must be a GeoJSON FeatureCollection')
        params[name] = gpd.GeoDataFrame.from_features(value['features'], crs='EPSG:4326')
    result = get_operator(operator)(**params)
    if isinstance(result, dict) and result and all(isinstance(v, gpd.GeoDataFrame) for v in result.values()):
        return {'__ports__': result}
    return result


def _project(value):
    if isinstance(value, RasterTiles):
        return {'runtime_type': 'raster_tiles', **raster_tiles_info(value)}
    if isinstance(value, RasterDataset):
        return {'runtime_type': 'raster', **raster_info(value)}
    if isinstance(value, gpd.GeoDataFrame):
        if value._geometry_column_name in value.columns:
            return json.loads(value.to_json())
        return json.loads(pd.DataFrame(value).to_json(orient='records'))
    if isinstance(value, pd.DataFrame):
        preview_limit = 50
        return {'type': 'pandas_dataframe',
                'schema': [{'name': name, 'type': str(dtype)} for name, dtype in value.dtypes.items()],
                'row_count': len(value), 'preview_limit': preview_limit,
                'preview_rows': json.loads(value.head(preview_limit).to_json(orient='records', double_precision=15)),
                'truncated': len(value) > preview_limit}
    if isinstance(value, dict):
        return {key: _project(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [_project(item) for item in value]
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    raise ValueError(f'Unsupported public result type: {type(value).__name__}')


def _execute_workflow_operator(operator, params):
    result = _execute_operator(operator, params)
    if isinstance(result, dict) and '__ports__' in result:
        return result
    # Match the declared single port so both implicit and explicit refs resolve.
    ports = OPERATORS[operator].get('output_ports') or [{'name': 'default'}]
    if len(ports) != 1:
        raise ValueError(f'{operator} must return its declared output ports')
    return {'__ports__': {ports[0]['name']: result}}


class GeoPythonWorkflowRunner(WorkflowRunner):
    def __init__(self, *, language='zh-cn'):
        self.language = language
        super().__init__({name for name, meta in OPERATORS.items() if 'workflow' in meta['execution_modes']},
                         _execute_workflow_operator,
                         result_projector=lambda value: json.dumps(_project(value), ensure_ascii=False, allow_nan=False))

    def execute(self, workflow_def, input_data=None, progress=None):
        with raster_error_language(self.language), raster_workspace():
            return super().execute(workflow_def, input_data, progress)


def execute_workflow(workflow_def, input_data=None):
    logs = []
    try:
        result = GeoPythonWorkflowRunner().execute(workflow_def, input_data, progress=lambda status, percent, task: logs.append({'level':'INFO','task_id':task,'message':status,'progress':percent}))
        return {'status': 'success', 'final_result': result.final_result, 'all_results': result.all_results, 'logs': logs}
    except WorkflowValidationError as exc:
        return {'status': 'failed', 'error_code': 'WORKFLOW_INVALID', 'error': str(exc), 'logs': logs}
    except WorkflowExecutionError as exc:
        logger.exception('Workflow execution failed')
        return {'status': 'failed', 'error_code': exc.error_code, 'error': str(exc),
                'all_results': exc.all_results or {}, 'logs': logs}
    except Exception as exc:
        logger.exception('Workflow execution failed')
        return {'status': 'failed', 'error_code': 'EXECUTION_FAILED', 'error': str(exc), 'logs': logs}


def execute_single_operator(operator_name, params):
    try:
        with raster_workspace():
            result = _execute_operator(operator_name, params)
            projected = _project(result)
            if isinstance(result, gpd.GeoDataFrame):
                projected = json.dumps(projected, ensure_ascii=False, allow_nan=False)
            return {'status': 'success', 'result': projected}
    except Exception as exc:
        return {'status': 'failed', 'error': str(exc)}
