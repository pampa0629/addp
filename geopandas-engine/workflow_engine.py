"""
GeoPandas 工作流引擎
核心优化：全程使用 GeoDataFrame 内存传递，避免中间序列化
"""

import geopandas as gpd
from typing import Dict, Any, List, Union
import json
from collections import defaultdict, deque

from operators import get_operator, list_operators


class GeoPandasWorkflowEngine:
    """
    GeoPandas 工作流引擎
    核心功能：DAG 拓扑排序 + GeoDataFrame 内存传递
    """

    def __init__(self):
        self.tasks = {}              # {task_id: TaskDef}
        self.results = {}            # {task_id: GeoDataFrame}  # 内存缓存
        self.task_order = []         # 拓扑排序后的任务执行顺序

    def add_task(self, task_id: str, operator: str, params: Dict[str, Any], depends_on: List[str] = None):
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
            'depends_on': depends_on or []
        }

    def load_workflow(self, workflow_def: Dict[str, Any]):
        """
        从工作流定义加载任务

        Args:
            workflow_def: 工作流定义
                {
                    "tasks": [
                        {
                            "id": "t1",
                            "operator": "buffer",
                            "params": {"distance": 100},
                            "depends_on": []
                        },
                        {
                            "id": "t2",
                            "operator": "centroid",
                            "params": {"input_gdf": {"$ref": "t1"}},
                            "depends_on": ["t1"]
                        }
                    ]
                }
        """
        for task in workflow_def.get('tasks', []):
            self.add_task(
                task_id=task['id'],
                operator=task['operator'],
                params=task.get('params', {}),
                depends_on=task.get('depends_on', [])
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
                    raise ValueError(f"Task {task_id} depends on unknown task {dep}")
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
            raise ValueError("检测到循环依赖")

        return sorted_tasks

    def resolve_references(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """
        解析参数中的引用（如 {"$ref": "task1"}）

        Args:
            params: 参数字典

        Returns:
            解析后的参数字典
        """
        resolved = {}
        for key, value in params.items():
            if isinstance(value, dict) and "$ref" in value:
                # 从内存缓存获取上游结果（GeoDataFrame）
                ref_task_id = value["$ref"]
                if ref_task_id not in self.results:
                    raise ValueError(f"Referenced task {ref_task_id} not found in results")
                resolved[key] = self.results[ref_task_id]
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
        解析 GeoJSON 输入为 GeoDataFrame

        Args:
            geojson_data: GeoJSON 字符串或字典

        Returns:
            GeoDataFrame
        """
        if isinstance(geojson_data, str):
            geojson_data = json.loads(geojson_data)

        return gpd.GeoDataFrame.from_features(geojson_data['features'], crs="EPSG:4326")

    def execute_task(self, task_id: str) -> gpd.GeoDataFrame:
        """
        执行单个任务

        Args:
            task_id: 任务ID

        Returns:
            GeoDataFrame 执行结果
        """
        task = self.tasks[task_id]
        operator_name = task['operator']
        params = task['params']

        # 解析参数引用
        resolved_params = self.resolve_references(params)

        # 处理输入参数：如果有 input_gdf 参数且是 GeoJSON，转换为 GeoDataFrame
        if 'input_gdf' in resolved_params and not isinstance(resolved_params['input_gdf'], gpd.GeoDataFrame):
            if isinstance(resolved_params['input_gdf'], (dict, str)):
                resolved_params['input_gdf'] = self.parse_geojson_input(resolved_params['input_gdf'])

        # 获取算子函数
        operator_func = get_operator(operator_name)

        # 执行算子（返回 GeoDataFrame，不序列化）
        result = operator_func(**resolved_params)

        return result

    def run(self, input_data: Dict[str, Any] = None) -> gpd.GeoDataFrame:
        """
        执行工作流

        Args:
            input_data: 输入数据字典（可选，用于参数化工作流）
                例如：{"poi_location": {...}, "buffer_distance": 100}

        Returns:
            最终结果 GeoDataFrame

        Raises:
            ValueError: 如果工作流定义错误或执行失败
        """
        # 拓扑排序
        self.task_order = self.topological_sort()

        # 如果提供了输入数据，存储到 results 中（key 前缀 "input."）
        if input_data:
            for key, value in input_data.items():
                if isinstance(value, (dict, str)) and key.endswith('_location'):
                    # 自动将 GeoJSON 输入转换为 GeoDataFrame
                    self.results[f"input.{key}"] = self.parse_geojson_input(value)
                else:
                    self.results[f"input.{key}"] = value

        # 逐步执行
        for task_id in self.task_order:
            try:
                result = self.execute_task(task_id)

                # 内存缓存（GeoDataFrame 对象，不序列化）
                self.results[task_id] = result

            except Exception as e:
                raise ValueError(f"任务 {task_id} 执行失败: {str(e)}")

        # 返回最后一个任务的结果
        final_task_id = self.task_order[-1]
        return self.results[final_task_id]

    def get_result_geojson(self, task_id: str = None) -> str:
        """
        获取结果的 GeoJSON 字符串

        Args:
            task_id: 任务ID（可选，默认返回最后一个任务结果）

        Returns:
            GeoJSON 字符串
        """
        if task_id is None:
            task_id = self.task_order[-1]

        if task_id not in self.results:
            raise ValueError(f"Task {task_id} not found in results")

        gdf = self.results[task_id]
        return gdf.to_json()

    def get_all_results_geojson(self) -> Dict[str, str]:
        """
        获取所有任务结果的 GeoJSON

        Returns:
            {task_id: geojson_string} 字典
        """
        return {
            task_id: gdf.to_json()
            for task_id, gdf in self.results.items()
            if isinstance(gdf, gpd.GeoDataFrame) and not task_id.startswith('input.')
        }

    def clear(self):
        """清空工作流状态"""
        self.tasks.clear()
        self.results.clear()
        self.task_order.clear()


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
                "all_results": {...}    # 所有中间结果
            }
    """
    engine = GeoPandasWorkflowEngine()

    try:
        # 加载工作流
        engine.load_workflow(workflow_def)

        # 执行工作流
        final_gdf = engine.run(input_data)

        # 返回结果
        return {
            "status": "success",
            "final_result": engine.get_result_geojson(),
            "all_results": engine.get_all_results_geojson()
        }

    except Exception as e:
        return {
            "status": "failed",
            "error": str(e)
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

        # 处理输入参数：如果是 GeoJSON，转换为 GeoDataFrame
        for key, value in params.items():
            if isinstance(value, (dict, str)) and 'features' in str(value):
                if isinstance(value, str):
                    value = json.loads(value)
                params[key] = gpd.GeoDataFrame.from_features(value['features'], crs="EPSG:4326")

        # 执行算子
        result_gdf = operator_func(**params)

        # 返回 GeoJSON
        return {
            "status": "success",
            "result": result_gdf.to_json()
        }

    except Exception as e:
        return {
            "status": "failed",
            "error": str(e)
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
