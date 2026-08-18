"""
GeoPython Workflow
核心优化：全程使用 GeoDataFrame 内存传递，避免中间序列化
"""

import geopandas as gpd
from typing import Dict, Any, List, Union
import json
import logging
import traceback
from collections import defaultdict, deque
from datetime import datetime

from operators import OPERATORS, get_operator, list_operators

logger = logging.getLogger(__name__)


class WorkflowInvalidError(Exception):
    """工作流定义无效错误"""
    pass


def validate_workflow_def(workflow_def: Dict[str, Any]):
    """校验 addp.workflow/v1 工作流定义。"""
    if not isinstance(workflow_def, dict) or 'tasks' not in workflow_def:
        raise WorkflowInvalidError("工作流定义缺少 'tasks' 字段")
    if not isinstance(workflow_def['tasks'], list) or len(workflow_def['tasks']) == 0:
        raise WorkflowInvalidError("工作流定义中的 'tasks' 必须是非空数组")

    task_ids = set()
    for task in workflow_def['tasks']:
        if 'id' not in task:
            raise WorkflowInvalidError("任务定义缺少 'id' 字段")
        if not isinstance(task['id'], str) or not task['id'].strip():
            raise WorkflowInvalidError("任务 id 必须是非空字符串")
        if task['id'] in task_ids:
            raise WorkflowInvalidError(f"任务 id 重复: {task['id']}")
        task_ids.add(task['id'])
        if 'operator' not in task:
            raise WorkflowInvalidError(f"任务 '{task['id']}' 缺少 'operator' 字段")
        if 'params' not in task:
            raise WorkflowInvalidError(f"任务 '{task['id']}' 缺少 'params' 字段")
        if not isinstance(task['params'], dict):
            raise WorkflowInvalidError(f"任务 '{task['id']}' 的 'params' 必须是对象")
        if 'depends_on' not in task:
            raise WorkflowInvalidError(f"任务 '{task['id']}' 缺少 'depends_on' 字段")
        if not isinstance(task['depends_on'], list):
            raise WorkflowInvalidError(f"任务 '{task['id']}' 的 'depends_on' 必须是数组")
        if not all(isinstance(dep, str) for dep in task['depends_on']):
            raise WorkflowInvalidError(f"任务 '{task['id']}' 的 'depends_on' 必须是字符串数组")
        if len(set(task['depends_on'])) != len(task['depends_on']):
            raise WorkflowInvalidError(f"任务 '{task['id']}' 的 depends_on 包含重复依赖")

    for task in workflow_def['tasks']:
        dependencies = set(task['depends_on'])
        for ref_task_id in _collect_workflow_refs(task['params']):
            if ref_task_id not in task_ids:
                raise WorkflowInvalidError(f"任务 '{task['id']}' 引用了不存在的任务 '{ref_task_id}'")
            if ref_task_id not in dependencies:
                raise WorkflowInvalidError(f"任务 '{task['id']}' 引用了任务 '{ref_task_id}' 但未在 depends_on 中声明")


def _collect_workflow_refs(value: Any) -> List[str]:
    refs = []
    if isinstance(value, dict):
        if "$ref" in value:
            ref = value["$ref"]
            if not isinstance(ref, str) or not ref.strip():
                raise WorkflowInvalidError("$ref 必须是非空字符串")
            refs.append(ref.strip())
            return refs
        for item in value.values():
            refs.extend(_collect_workflow_refs(item))
    elif isinstance(value, list):
        for item in value:
            refs.extend(_collect_workflow_refs(item))
    return refs


class PythonWorkflowEngine:
    """
    GeoPython Workflow
    核心功能：DAG 拓扑排序 + GeoDataFrame 内存传递
    """

    def __init__(self):
        self.tasks = {}              # {task_id: TaskDef}
        self.results = {}            # {task_id: {port_name: GeoDataFrame}}  # 内存缓存(端口结构)
        self.task_order = []         # 拓扑排序后的任务执行顺序
        self.logs = []               # 执行日志列表

    def add_task(self, task_id: str, operator: str, params: Dict[str, Any], depends_on: List[str]):
        """
        添加任务节点

        Args:
            task_id: 任务ID
            operator: 算子名称
            params: 参数字典
            depends_on: 依赖的任务ID列表
        """
        self.tasks[task_id] = {
            'operator': operator,
            'params': params,
            'depends_on': depends_on
        }

    def load_workflow(self, workflow_def: Dict[str, Any]):
        """
        从工作流定义加载任务

        标准格式：
            {
                "tasks": [
                    {
                        "id": "t1",
                        "operator": "buffer",
                        "params": {"distance": 100},
                        "depends_on": []
                    }
                ]
            }

        Args:
            workflow_def: 工作流定义
        """
        validate_workflow_def(workflow_def)

        for task in workflow_def['tasks']:
            self.add_task(
                task_id=task['id'],
                operator=task['operator'],
                params=task['params'],
                depends_on=task['depends_on']
            )

    def topological_sort(self) -> List[str]:
        """
        拓扑排序（Kahn 算法）

        Returns:
            排序后的任务ID列表

        Raises:
            ValueError: 如果检测到循环依赖
        """
        # 计算入度
        in_degree = {task_id: 0 for task_id in self.tasks}
        for task_id, task in self.tasks.items():
            for dep in task['depends_on']:
                if dep not in in_degree:
                    raise WorkflowInvalidError(f"任务 '{task_id}' 依赖的任务 '{dep}' 不存在")
                in_degree[task_id] += 1

        # 找出所有入度为 0 的节点
        queue = deque([task_id for task_id, degree in in_degree.items() if degree == 0])

        sorted_tasks = []
        while queue:
            task_id = queue.popleft()
            sorted_tasks.append(task_id)

            # 遍历所有任务，减少依赖此任务的入度
            for other_id, other_task in self.tasks.items():
                if task_id in other_task['depends_on']:
                    in_degree[other_id] -= 1
                    if in_degree[other_id] == 0:
                        queue.append(other_id)

        # 检查是否有环
        if len(sorted_tasks) != len(self.tasks):
            raise WorkflowInvalidError("工作流包含循环依赖")

        return sorted_tasks

    def resolve_references(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """
        解析参数中的引用（支持端口选择）

        格式:
        - {"$ref": "task_id"} → 使用 default 端口
        - {"$ref": "task_id", "port": "large"} → 使用指定端口

        Args:
            params: 参数字典

        Returns:
            解析后的参数字典
        """
        resolved = {}
        for key, value in params.items():
            if isinstance(value, dict) and "$ref" in value:
                # 从内存缓存获取上游结果
                ref_task_id = value["$ref"]
                port_name = value.get("port", "default")  # 默认使用 default 端口

                if ref_task_id not in self.results:
                    raise ValueError(f"Referenced task {ref_task_id} not found in results")

                task_outputs = self.results[ref_task_id]
                if port_name not in task_outputs:
                    available_ports = list(task_outputs.keys())
                    raise ValueError(
                        f"Task '{ref_task_id}' does not have port '{port_name}'. "
                        f"Available ports: {available_ports}"
                    )

                resolved[key] = task_outputs[port_name]
            elif isinstance(value, list):
                # 递归处理列表
                resolved[key] = [
                    self.resolve_references({'item': item})['item']
                    if isinstance(item, dict) else item
                    for item in value
                ]
            elif isinstance(value, dict):
                # 递归处理嵌套字典
                resolved[key] = self.resolve_references(value)
            else:
                resolved[key] = value

        return resolved

    def parse_geojson_input(self, geojson_data: Union[Dict, str]) -> gpd.GeoDataFrame:
        """
        解析 GeoJSON 或 WKT 输入为 GeoDataFrame

        Args:
            geojson_data: GeoJSON 字符串/字典 或 WKT 字符串

        Returns:
            GeoDataFrame
        """
        from shapely import wkt
        from shapely.geometry import shape

        # 情况 1: WKT 字符串 (e.g., "POINT(120 30)")
        if isinstance(geojson_data, str):
            # 尝试解析为 WKT
            try:
                geom = wkt.loads(geojson_data)
                return gpd.GeoDataFrame([{}], geometry=[geom], crs="EPSG:4326")
            except Exception:
                # 如果不是 WKT，尝试作为 GeoJSON 字符串解析
                geojson_data = json.loads(geojson_data)

        # 情况 2: GeoJSON 字典 (包含 features)
        if isinstance(geojson_data, dict):
            if 'features' in geojson_data:
                return gpd.GeoDataFrame.from_features(geojson_data['features'], crs="EPSG:4326")
            # 单个几何对象 (e.g., {"type": "Point", "coordinates": [...]})
            elif 'type' in geojson_data and 'coordinates' in geojson_data:
                geom = shape(geojson_data)
                return gpd.GeoDataFrame([{}], geometry=[geom], crs="EPSG:4326")

        raise ValueError(f"Unsupported geometry input format: {type(geojson_data)}")

    def log(self, level: str, message: str, task_id: str = None):
        """添加日志条目"""
        log_entry = {
            "timestamp": datetime.now().isoformat(),
            "level": level,
            "message": message
        }
        if task_id:
            log_entry["task_id"] = task_id
        self.logs.append(log_entry)

        # 同时输出到 logger
        log_func = getattr(logger, level.lower(), logger.info)
        log_func(f"[{task_id or 'workflow'}] {message}")

    def execute_task(self, task_id: str) -> Dict[str, gpd.GeoDataFrame]:
        """
        执行单个任务，返回端口输出字典

        Args:
            task_id: 任务ID

        Returns:
            Dict[port_name, GeoDataFrame] 端口输出字典
        """
        task = self.tasks[task_id]
        operator_name = task['operator']
        params = task['params']

        self.log("INFO", f"开始执行算子 '{operator_name}'", task_id)

        try:
            # 解析参数引用
            resolved_params = self.resolve_references(params)

            # 处理输入参数：如果有 input_gdf 参数且是 GeoJSON，转换为 GeoDataFrame
            if 'input_gdf' in resolved_params and not isinstance(resolved_params['input_gdf'], gpd.GeoDataFrame):
                if isinstance(resolved_params['input_gdf'], (dict, str)):
                    resolved_params['input_gdf'] = self.parse_geojson_input(resolved_params['input_gdf'])

            # 获取算子函数
            operator_func = get_operator(operator_name)

            # 执行算子
            result = operator_func(**resolved_params)

            # 自动适配单输出/多输出。
            # GeoDataFrame 字典仍表示多端口输出；普通 JSON 字典表示格式/文件类算子的单个结构化结果。
            if isinstance(result, dict):
                if all(isinstance(value, gpd.GeoDataFrame) for value in result.values()):
                    # 多输出: {"large": gdf1, "small": gdf2}
                    output_ports = result
                else:
                    output_ports = {"default": result}
            elif isinstance(result, gpd.GeoDataFrame):
                # 单输出: 自动包装为 {"default": gdf}
                output_ports = {"default": result}
            else:
                output_ports = {"default": result}

            self.log("INFO", f"算子执行成功，输出端口: {list(output_ports.keys())}", task_id)
            return output_ports

        except Exception as e:
            error_msg = f"算子执行失败: {str(e)}"
            self.log("ERROR", error_msg, task_id)
            self.log("ERROR", f"详细堆栈:\n{traceback.format_exc()}", task_id)
            raise ValueError(error_msg) from e


    def run(self, input_data: Dict[str, Any] = None) -> gpd.GeoDataFrame:
        """
        执行工作流

        Args:
            input_data: 输入数据字典（可选，用于参数化工作流）
                例如：{"poi_location": {...}, "buffer_distance": 100}

        Returns:
            最终结果 GeoDataFrame (默认端口或唯一端口的输出)

        Raises:
            ValueError: 如果工作流定义错误或执行失败
        """
        self.log("INFO", "开始执行工作流")

        # 拓扑排序
        self.task_order = self.topological_sort()
        self.log("INFO", f"任务执行顺序: {self.task_order}")

        # 如果提供了输入数据，存储到 results 中（key 前缀 "input."，包装为端口格式）
        if input_data:
            for key, value in input_data.items():
                # 自动将几何输入转换为 GeoDataFrame
                # 支持的后缀: _location, _geom, _geometry, _wkt
                is_geometry_input = any(key.endswith(suffix) for suffix in ['_location', '_geom', '_geometry', '_wkt'])

                if isinstance(value, (dict, str)) and is_geometry_input:
                    # 包装为端口格式
                    self.results[f"input.{key}"] = {"default": self.parse_geojson_input(value)}
                else:
                    self.results[f"input.{key}"] = {"default": value}
            self.log("INFO", f"已加载输入数据: {list(input_data.keys())}")

        # 逐步执行
        for task_id in self.task_order:
            try:
                port_outputs = self.execute_task(task_id)

                # 内存缓存（端口字典格式）
                self.results[task_id] = port_outputs

            except Exception as e:
                error_msg = f"任务 {task_id} 执行失败: {str(e)}"
                self.log("ERROR", error_msg)
                raise ValueError(error_msg) from e

        # 返回最后一个任务的结果
        final_task_id = self.task_order[-1]
        final_outputs = self.results[final_task_id]

        # 如果只有一个端口，返回该端口
        if len(final_outputs) == 1:
            result = list(final_outputs.values())[0]
        # 如果有 default 端口，返回 default
        elif "default" in final_outputs:
            result = final_outputs["default"]
        else:
            # 多个端口且无 default，报错提示
            raise ValueError(
                f"Final task '{final_task_id}' has multiple output ports without 'default'. "
                f"Available ports: {list(final_outputs.keys())}. "
                f"Please specify which port to return."
            )

        self.log("INFO", "工作流执行成功")
        return result

    def get_result_geojson(self, task_id: str = None, port: str = None) -> str:
        """
        获取结果的 GeoJSON 字符串

        Args:
            task_id: 任务ID（可选，默认返回最后一个任务结果）
            port: 端口名称（可选，默认使用 default 或唯一端口）

        Returns:
            GeoJSON 字符串
        """
        if task_id is None:
            task_id = self.task_order[-1]

        if task_id not in self.results:
            raise ValueError(f"Task {task_id} not found in results")

        port_outputs = self.results[task_id]

        # 确定使用哪个端口
        if port:
            if port not in port_outputs:
                raise ValueError(f"Task '{task_id}' does not have port '{port}'. Available: {list(port_outputs.keys())}")
            gdf = port_outputs[port]
        elif len(port_outputs) == 1:
            gdf = list(port_outputs.values())[0]
        elif "default" in port_outputs:
            gdf = port_outputs["default"]
        else:
            raise ValueError(f"Task '{task_id}' has multiple ports. Please specify 'port' parameter. Available: {list(port_outputs.keys())}")

        # 检查是否有有效的 geometry 列
        if isinstance(gdf, gpd.GeoDataFrame) and hasattr(gdf, '_geometry_column_name') and gdf._geometry_column_name in gdf.columns:
            return gdf.to_json()
        if isinstance(gdf, gpd.GeoDataFrame):
            import pandas as pd
            return pd.DataFrame(gdf).to_json(orient='records')
        return json.dumps(gdf, ensure_ascii=False, default=str)

    def get_all_results_geojson(self) -> Dict[str, str]:
        """
        获取所有任务结果的 GeoJSON

        Returns:
            {task_id: geojson_string} 字典（使用默认端口或唯一端口）
        """
        results = {}
        for task_id, port_outputs in self.results.items():
            if task_id.startswith('input.'):
                continue

            # 过滤出可序列化端口。GeoDataFrame 输出保留原有 GeoJSON 语义；
            # 普通 JSON 输出用于格式转换、文件处理等非空间表格结果。
            serializable_ports = {
                port_name: value
                for port_name, value in port_outputs.items()
                if isinstance(value, (gpd.GeoDataFrame, dict, list, str, int, float, bool)) or value is None
            }

            # 如果只有一个端口，使用该端口
            if len(serializable_ports) == 1:
                gdf = list(serializable_ports.values())[0]
                # 检查是否有有效的 geometry 列
                if isinstance(gdf, gpd.GeoDataFrame) and hasattr(gdf, '_geometry_column_name') and gdf._geometry_column_name in gdf.columns:
                    results[task_id] = gdf.to_json()
                elif isinstance(gdf, gpd.GeoDataFrame):
                    import pandas as pd
                    results[task_id] = pd.DataFrame(gdf).to_json(orient='records')
                else:
                    results[task_id] = json.dumps(gdf, ensure_ascii=False, default=str)
            # 如果有 default 端口，使用 default
            elif "default" in serializable_ports:
                gdf = serializable_ports["default"]
                if isinstance(gdf, gpd.GeoDataFrame) and hasattr(gdf, '_geometry_column_name') and gdf._geometry_column_name in gdf.columns:
                    results[task_id] = gdf.to_json()
                elif isinstance(gdf, gpd.GeoDataFrame):
                    import pandas as pd
                    results[task_id] = pd.DataFrame(gdf).to_json(orient='records')
                else:
                    results[task_id] = json.dumps(gdf, ensure_ascii=False, default=str)
            # 否则跳过这个任务（没有明确的输出）

        return results

    def clear(self):
        """清空工作流状态"""
        self.tasks.clear()
        self.results.clear()
        self.task_order.clear()
        self.logs.clear()


# ========================================
# 便捷函数
# ========================================

def execute_workflow(workflow_def: Dict[str, Any], input_data: Dict[str, Any] = None) -> Dict[str, Any]:
    """
    执行工作流（便捷函数）

    Args:
        workflow_def: 工作流定义
        input_data: 输入数据

    Returns:
        结果字典
            {
                "status": "success",
                "final_result": "...",  # GeoJSON 字符串
                "all_results": {...},   # 所有中间结果
                "logs": [...]            # 执行日志
            }
    """
    engine = PythonWorkflowEngine()

    try:
        # 加载工作流
        engine.load_workflow(workflow_def)

        # 执行工作流
        final_gdf = engine.run(input_data)

        # 返回结果
        return {
            "status": "success",
            "final_result": engine.get_result_geojson(),
            "all_results": engine.get_all_results_geojson(),
            "logs": engine.logs
        }

    except WorkflowInvalidError as e:
        return {
            "status": "failed",
            "error_code": "WORKFLOW_INVALID",
            "error": str(e),
            "logs": engine.logs,
            "traceback": traceback.format_exc()
        }

    except Exception as e:
        return {
            "status": "failed",
            "error": str(e),
            "logs": engine.logs,
            "traceback": traceback.format_exc()
        }


def execute_single_operator(operator_name: str, params: Dict[str, Any]) -> Dict[str, Any]:
    """
    执行单个算子（便捷函数）

    Args:
        operator_name: 算子名称
        params: 参数字典

    Returns:
        结果字典
            {
                "status": "success",
                "result": "..."  # GeoJSON 字符串
            }
    """
    try:
        # 获取算子函数
        operator_func = get_operator(operator_name)

        # 只按算子元数据转换 GeoDataFrame 输入；object 参数必须保持结构化对象。
        param_schema = {
            item.get("name"): item
            for item in OPERATORS.get(operator_name, {}).get("param_schema", [])
            if isinstance(item, dict) and item.get("name")
        }
        for key, value in params.items():
            schema = param_schema.get(key, {})
            if str(schema.get("type") or "").strip().lower() != "geodataframe":
                continue
            if isinstance(value, str):
                value = json.loads(value)
            if not isinstance(value, dict) or value.get("type") != "FeatureCollection" or not isinstance(value.get("features"), list):
                raise ValueError(f"参数 '{key}' 必须是 GeoJSON FeatureCollection")
            params[key] = gpd.GeoDataFrame.from_features(value["features"], crs="EPSG:4326")

        # 执行算子
        result = operator_func(**params)

        if isinstance(result, gpd.GeoDataFrame):
            result = result.to_json()
        elif not isinstance(result, (dict, list, str, int, float, bool)) and result is not None:
            result = json.loads(json.dumps(result, ensure_ascii=False, default=str))

        return {
            "status": "success",
            "result": result
        }

    except Exception as e:
        return {
            "status": "failed",
            "error": str(e),
            "traceback": traceback.format_exc()
        }


# ========================================
# 测试代码
# ========================================

if __name__ == "__main__":
    # 测试简单工作流
    workflow_def = {
        "tasks": [
            {
                "id": "t1",
                "operator": "buffer",
                "params": {
                    "input_gdf": {
                        "type": "FeatureCollection",
                        "features": [{
                            "type": "Feature",
                            "geometry": {"type": "Point", "coordinates": [116.404, 39.915]},
                            "properties": {"name": "Beijing"}
                        }]
                    },
                    "distance": 0.01
                },
                "depends_on": []
            },
            {
                "id": "t2",
                "operator": "centroid",
                "params": {
                    "input_gdf": {"$ref": "t1"}
                },
                "depends_on": ["t1"]
            }
        ]
    }

    result = execute_workflow(workflow_def)
    print("工作流执行结果:")
    print(json.dumps(result, indent=2, ensure_ascii=False))
