"""
Spark Workflow Engine
核心功能: DAG 拓扑排序 + DataFrame 内存传递
"""

import logging
from typing import Dict, Any, List
from collections import defaultdict, deque
from pyspark.sql import DataFrame

from operators import get_operator, list_operators
from spark_connector import get_spark_connector

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


class SparkWorkflowEngine:
    """
    Spark 工作流引擎
    核心功能: DAG 拓扑排序 + DataFrame 内存传递
    """

    def __init__(self, engine_id: int, tenant_id: int):
        """
        初始化工作流引擎

        Args:
            engine_id: Spark引擎ID (从system.engines获取)
        """
        self.engine_id = engine_id
        self.tenant_id = tenant_id
        self.tasks = {}              # {task_id: TaskDef}
        self.results = {}            # {task_id: {port_name: DataFrame}}  # 内存缓存(端口结构)
        self.task_order = []         # 拓扑排序后的任务执行顺序

        # 获取SparkSession (预连接)
        connector = get_spark_connector()
        self.spark = connector.get_or_create_session(engine_id, tenant_id)

        logger.info(f"SparkWorkflowEngine initialized for engine {engine_id}")

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

        标准格式:
            {
                "tasks": [
                    {
                        "id": "t1",
                        "operator": "load",
                        "params": {"source_type": "table", ...},
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
        拓扑排序 (Kahn 算法)

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

            # 遍历所有任务,减少依赖此任务的入度
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
        解析参数中的引用 (支持端口选择)

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

    def execute_task(self, task_id: str) -> Dict[str, DataFrame]:
        """
        执行单个任务,返回端口输出字典

        Args:
            task_id: 任务ID

        Returns:
            Dict[port_name, DataFrame] 端口输出字典
        """
        task = self.tasks[task_id]
        operator_name = task['operator']
        params = task['params']

        # 解析参数引用
        resolved_params = self.resolve_references(params)

        # 运行时资源上下文不属于工作流定义，也不暴露在算子元数据中。
        if operator_name in ['load', 'sql']:
            resolved_params['engine_id'] = self.engine_id
            resolved_params['tenant_id'] = self.tenant_id
        elif operator_name == 'save':
            resolved_params['engine_id'] = self.engine_id

        # 获取算子函数
        operator_func = get_operator(operator_name)

        # 执行算子
        logger.info(f"Executing task {task_id} with operator {operator_name}")
        result = operator_func(**resolved_params)

        # 自动适配单输出/多输出
        if isinstance(result, dict):
            # 检查是否是执行结果字典 (如save算子返回 {"status": "success", ...})
            if "status" in result:
                # 这是执行结果,不是DataFrame,包装为特殊端口
                return {"result": result}
            # 多输出: {"large": df1, "small": df2}
            return result
        elif isinstance(result, DataFrame):
            # 单输出: 自动包装为 {"default": df}
            return {"default": result}
        else:
            # 其他类型 (如临时视图的确认信息)
            return {"result": result}

    def run(self, input_data: Dict[str, Any] = None) -> Dict[str, Any]:
        """
        执行工作流

        Args:
            input_data: 输入数据字典 (可选,用于参数化工作流)
                例如: {"buffer_distance": 100, "filter_condition": "population > 1000000"}

        Returns:
            执行结果字典
                {
                    "status": "success",
                    "final_result": DataFrame or dict,
                    "task_order": [...],
                    "message": "..."
                }

        Raises:
            ValueError: 如果工作流定义错误或执行失败
        """
        # 拓扑排序
        self.task_order = self.topological_sort()

        # 如果提供了输入数据,存储到 results 中 (key 前缀 "input.",包装为端口格式)
        if input_data:
            for key, value in input_data.items():
                self.results[f"input.{key}"] = {"default": value}

        # 逐步执行
        for task_id in self.task_order:
            try:
                port_outputs = self.execute_task(task_id)

                # 内存缓存 (端口字典格式)
                self.results[task_id] = port_outputs

                logger.info(f"Task {task_id} completed successfully")

            except Exception as e:
                logger.error(f"Task {task_id} failed: {str(e)}", exc_info=True)
                raise ValueError(f"任务 {task_id} 执行失败: {str(e)}")

        # 返回最后一个任务的结果
        final_task_id = self.task_order[-1]
        final_outputs = self.results[final_task_id]

        # 确定最终结果
        if len(final_outputs) == 1:
            final_result = list(final_outputs.values())[0]
        elif "default" in final_outputs:
            final_result = final_outputs["default"]
        else:
            # 多个端口且无 default,返回端口字典
            final_result = final_outputs

        return {
            "status": "success",
            "final_result": final_result,
            "all_results": self.results,
            "task_order": self.task_order,
            "message": f"Workflow completed successfully. Executed {len(self.task_order)} tasks."
        }

    def get_result_df(self, task_id: str = None, port: str = None) -> DataFrame:
        """
        获取任务结果DataFrame

        Args:
            task_id: 任务ID (可选,默认返回最后一个任务结果)
            port: 端口名称 (可选,默认使用 default 或唯一端口)

        Returns:
            DataFrame
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
            df = port_outputs[port]
        elif len(port_outputs) == 1:
            df = list(port_outputs.values())[0]
        elif "default" in port_outputs:
            df = port_outputs["default"]
        else:
            raise ValueError(f"Task '{task_id}' has multiple ports. Please specify 'port' parameter. Available: {list(port_outputs.keys())}")

        return df

    def clear(self):
        """清空工作流状态"""
        self.tasks.clear()
        self.results.clear()
        self.task_order.clear()


# ========================================
# 便捷函数
# ========================================

def execute_workflow(engine_id: int, tenant_id: int, workflow_def: Dict[str, Any], input_data: Dict[str, Any] = None) -> Dict[str, Any]:
    """
    执行工作流 (便捷函数)

    Args:
        engine_id: Spark引擎ID
        workflow_def: 工作流定义
        input_data: 输入数据

    Returns:
        结果字典
            {
                "status": "success",
                "final_result": ...,
                "task_order": [...],
                "message": "..."
            }
    """
    try:
        validate_workflow_def(workflow_def)

        engine = SparkWorkflowEngine(engine_id, tenant_id)

        # 加载工作流
        engine.load_workflow(workflow_def)

        # 执行工作流
        result = engine.run(input_data)

        return result

    except WorkflowInvalidError as e:
        logger.error(f"Workflow validation failed: {e}", exc_info=True)
        return {
            "status": "failed",
            "error_code": "WORKFLOW_INVALID",
            "error": str(e)
        }

    except Exception as e:
        logger.error(f"Workflow execution failed: {e}", exc_info=True)
        return {
            "status": "failed",
            "error": str(e)
        }


def execute_single_operator(engine_id: int, tenant_id: int, operator_name: str, params: Dict[str, Any]) -> Dict[str, Any]:
    """
    执行单个算子 (便捷函数)

    Args:
        engine_id: Spark引擎ID
        operator_name: 算子名称
        params: 参数字典

    Returns:
        结果字典
            {
                "status": "success",
                "result": ...
            }
    """
    try:
        # 获取SparkSession
        connector = get_spark_connector()
        spark = connector.get_or_create_session(engine_id, tenant_id)

        # 获取算子函数
        operator_func = get_operator(operator_name)

        # 运行时资源上下文不属于 direct params，由 Runtime 注入。
        if operator_name in ['load', 'sql']:
            params['engine_id'] = engine_id
            params['tenant_id'] = tenant_id
        elif operator_name == 'save':
            params['engine_id'] = engine_id

        # 执行算子
        result = operator_func(**params)

        return {
            "status": "success",
            "result": result
        }

    except Exception as e:
        logger.error(f"Operator execution failed: {e}", exc_info=True)
        return {
            "status": "failed",
            "error": str(e)
        }


# ========================================
# 测试代码
# ========================================

if __name__ == "__main__":
    # 测试简单工作流
    import json

    workflow_def = {
        "tasks": [
            {
                "id": "t1",
                "operator": "load",
                "params": {
                    "source_type": "table",
                    "schema": "test_db",
                    "table": "cities"
                },
                "depends_on": []
            },
            {
                "id": "t2",
                "operator": "filter",
                "params": {
                    "input_df": {"$ref": "t1"},
                    "condition": "population > 10000000"
                },
                "depends_on": ["t1"]
            },
            {
                "id": "t3",
                "operator": "add_column",
                "params": {
                    "input_df": {"$ref": "t2"},
                    "column_name": "point_geom",
                    "expression": "ST_Point(longitude, latitude)"
                },
                "depends_on": ["t2"]
            }
        ]
    }

    # 执行工作流 (需要Spark资源ID,这里假设为34)
    result = execute_workflow(
        engine_id=34,
        tenant_id=1,
        workflow_def=workflow_def
    )

    print("工作流执行结果:")
    print(json.dumps({
        "status": result["status"],
        "task_order": result.get("task_order"),
        "message": result.get("message"),
        "error": result.get("error")
    }, indent=2, ensure_ascii=False))
