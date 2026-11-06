"""GeoPython domain executor over the shared workflow runtime core."""
from __future__ import annotations

import json
import logging
import geopandas as gpd
import pandas as pd
from addp_common.workflow_runtime import OperatorInputError, WorkflowRunner, WorkflowExecutionError, WorkflowValidationError
from operators import OPERATORS, get_operator
from operators.io_operators import GeoPackageSaveError
from operators.orc_table import SourceChanged as OrcSourceChanged
from operators.properties_operators import MeasurementInputError, BoundsInputError
from operators.spatial_relations import DistanceInputError, RelationInputError
from operators.data_operations import MultiBufferInputError, ClipInputError, VoronoiInputError
from operators.geometric_operators import LineCheckInputError, LineSplitInputError, PolygonCheckInputError, PolygonizeInputError, ReferenceSnapInputError, GeometryValidityInputError, VertexInputError, MultipartInputError, InteriorPointInputError, UnaryConstructionInputError, SimplifyInputError, BufferInputError, NearLineInputError, EndpointSnapError, LineExtensionError, LineTrimError, PolygonRingInputError, LineNodeInputError, IntersectionInputError, DissolveInputError, UnionInputError, DifferenceInputError
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
    if (operator in {'check_dangling_points', 'check_line_intersection', 'check_line_self_intersection',
                     'check_line_overlap', 'check_line_self_overlap', 'split_line_intersections',
                     'group_line_components', 'normalize_line_network', 'merge_line_chains'}
            and not {'input_gdf', 'id_field'} <= params.keys()):
        raise LineCheckInputError('operator_input_invalid', 'input_gdf and id_field are required')
    if operator == 'merge_line_chains' and 'match_fields' not in params:
        raise LineCheckInputError('operator_match_fields_invalid', 'match_fields is required')
    if operator == 'check_polygon_overlap' and not {'input_gdf', 'id_field'} <= params.keys():
        raise PolygonCheckInputError('overlap_source', 'input_gdf and id_field are required')
    if operator == 'check_polygon_gap':
        if 'input_gdf' not in params:
            raise PolygonCheckInputError('gap_source', 'input_gdf is required')
        if 'coverage_gdf' not in params:
            raise PolygonCheckInputError('coverage', 'coverage_gdf is required')
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
                         self._execute_operator,
                         result_projector=lambda value: json.dumps(_project(value), ensure_ascii=False, allow_nan=False))

    def _execute_operator(self, operator, params):
        try:
            return _execute_workflow_operator(operator, params)
        except LineCheckInputError as exc:
            raise OperatorInputError(exc.reason, exc.message(self.language), field=exc.field) from exc
        except LineSplitInputError as exc:
            raise OperatorInputError(exc.reason, exc.message(self.language), field=exc.field) from exc
        except PolygonCheckInputError as exc:
            reason = {
                'overlap_source': 'operator_input_invalid',
                'overlap_crs': 'operator_crs_invalid',
                'gap_source': 'operator_input_invalid',
                'coverage': 'operator_mask_invalid',
                'gap_crs': 'operator_crs_invalid',
            }[exc.code]
            raise OperatorInputError(reason, exc.message(self.language)) from exc
        except PolygonizeInputError as exc:
            message = str(exc) if self.language == "en" else "拓扑构面需要带活动几何列的图层，几何须为有效、非空且坐标有限的二维线或多线。"
            raise OperatorInputError("operator_input_invalid", message) from exc
        except GeoPackageSaveError as exc:
            raise ValueError(exc.message(self.language)) from exc
        except (EndpointSnapError, LineExtensionError, ReferenceSnapInputError, GeometryValidityInputError, MultipartInputError, BoundsInputError, MeasurementInputError, InteriorPointInputError, UnaryConstructionInputError, SimplifyInputError, BufferInputError, VoronoiInputError, RelationInputError, DistanceInputError, NearLineInputError, MultiBufferInputError, ClipInputError, PolygonRingInputError, LineTrimError, LineNodeInputError, IntersectionInputError, DissolveInputError, UnionInputError, DifferenceInputError) as exc:
            reasons = {
                EndpointSnapError: {'source': 'operator_input_invalid', 'reference': 'operator_reference_invalid',
                                    'crs': 'operator_crs_invalid', 'tolerance': 'operator_positive_number_required'},
                LineExtensionError: {'source': 'operator_input_invalid', 'reference': 'operator_reference_invalid',
                                     'crs': 'operator_crs_invalid', 'tolerance': 'operator_distance_limit_invalid'},
                ReferenceSnapInputError: {'source':'operator_input_invalid','reference':'operator_reference_invalid','crs':'operator_crs_invalid','tolerance':'operator_nonnegative_number_required'},
                GeometryValidityInputError: {'source':'operator_input_invalid','fields':'operator_output_fields_invalid'},
                VertexInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid','field':'operator_field_conflict'},
                MultipartInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid','field':'operator_field_conflict'},
                BoundsInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid','field':'operator_geometry_field_conflict'},
                MeasurementInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid','area':'operator_geometry_field_conflict','length':'operator_geometry_field_conflict','threshold':'operator_nonnegative_number_required'},
                InteriorPointInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid'},
                UnaryConstructionInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid'},
                SimplifyInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid',
                                     'tolerance':'operator_nonnegative_number_required','preserve_topology':'operator_boolean_required'},
                BufferInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid',
                                   'distance':'operator_number_invalid','resolution':'operator_positive_integer_required'},
                VoronoiInputError: {'source': 'operator_input_invalid', 'mask': 'operator_mask_invalid', 'crs': 'operator_crs_invalid'},
                RelationInputError: {'source': 'operator_input_invalid', 'reference': 'operator_reference_invalid',
                                     'crs': 'operator_crs_invalid'},
                DissolveInputError: {'source':'operator_input_invalid','crs':'operator_crs_invalid','groups':'operator_group_fields_invalid','fields':'operator_dissolve_fields_conflict'},
                UnionInputError: {'source': 'operator_input_invalid', 'reference': 'operator_reference_invalid',
                                  'crs': 'operator_crs_invalid', 'fields': 'operator_overlay_fields_conflict',
                                  'source_overlap': 'operator_input_overlap', 'reference_overlap': 'operator_reference_overlap'},
                ClipInputError: {'source': 'operator_input_invalid', 'mask': 'operator_mask_invalid', 'crs': 'operator_crs_invalid'},
                DifferenceInputError: {'source': 'operator_input_invalid', 'mask': 'operator_mask_invalid', 'crs': 'operator_crs_invalid'},
                IntersectionInputError: {'source': 'operator_input_invalid', 'reference': 'operator_reference_invalid',
                                         'crs': 'operator_crs_invalid', 'fields': 'operator_overlay_fields_conflict'},
                LineNodeInputError: {'source': 'operator_input_invalid', 'crs': 'operator_projected_crs_required',
                                     'network': 'operator_endpoint_network_invalid', 'field_conflict': 'operator_field_conflict'},
                LineTrimError: {'tolerance': 'operator_distance_limit_invalid', 'source': 'operator_input_invalid',
                                'reference': 'operator_reference_invalid', 'crs': 'operator_crs_invalid',
                                'topology': 'operator_source_topology_invalid', 'conflict': 'operator_trim_conflict'},
                PolygonRingInputError: {"field_conflict": "operator_field_conflict", "geometry_invalid": "operator_geometry_invalid"},
                MultiBufferInputError: {'source': 'operator_input_invalid', 'crs': 'operator_crs_invalid', 'distance_conflict': 'operator_field_conflict',
                                       'distances': 'operator_number_array_invalid', 'resolution': 'operator_positive_integer_required'},
                DistanceInputError: {'source': 'operator_input_invalid', 'reference': 'operator_reference_invalid', 'geometry_column_conflict': 'operator_geometry_field_conflict',
                                     'projected_crs': 'operator_projected_crs_required', 'crs_mismatch': 'operator_crs_mismatch'},
                NearLineInputError: {'tolerance': 'operator_positive_number_required', 'source': 'operator_input_invalid',
                                    'reference': 'operator_reference_invalid', 'crs': 'operator_crs_invalid'},
            }
            reason = reasons[type(exc)].get(exc.code)
            if reason is not None:
                field = exc.code if isinstance(exc, MeasurementInputError) and exc.code in {"area", "length"} else exc.field if isinstance(exc, (MultipartInputError, BoundsInputError, PolygonRingInputError, LineNodeInputError)) else "distance" if reason in {"operator_field_conflict", "operator_geometry_field_conflict"} else ""
                message = str(exc) if isinstance(exc, PolygonRingInputError) else exc.message(self.language)
                raise OperatorInputError(reason, message, field=field) from exc
            # A numerical calculation failure is not a parameter admission failure.
            raise ValueError(exc.message(self.language)) from exc

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
                'all_results': exc.all_results or {}, 'logs': logs,
                **({'diagnostic': exc.diagnostic} if exc.diagnostic is not None else {})}
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
    except OrcSourceChanged:
        return {'status':'failed','error_code':'ORC_SOURCE_CHANGED','error':'ORC source changed; refresh metadata'}
    except Exception as exc:
        return {'status': 'failed', 'error': str(exc)}
