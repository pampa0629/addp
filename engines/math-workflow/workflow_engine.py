"""
Math Workflow Engine - 简化版 DAG 工作流引擎

参考 Python Workflow 的实现，但去掉 GeoDataFrame 相关逻辑。
支持：
- DAG 工作流定义（tasks 数组，depends_on 依赖）
- Kahn 算法拓扑排序
- 内存传递中间结果（避免序列化开销）
- 参数引用（{"$ref": "task1.result"} 或 {"$ref": "task1"}）
"""

from collections import defaultdict, deque
from typing import Dict, Any, List
import logging

logger = logging.getLogger(__name__)


class WorkflowInvalidError(Exception):
    """工作流定义无效错误"""
    pass


class MathWorkflowEngine:
    """简化版 DAG 工作流引擎"""

    def __init__(self):
        self.tasks: Dict[str, Dict] = {}
        self.results: Dict[str, Any] = {}

    def load_workflow(self, workflow_def: Dict):
        """加载工作流定义"""
        if 'tasks' not in workflow_def:
            raise WorkflowInvalidError("工作流定义缺少 'tasks' 字段")

        for task in workflow_def['tasks']:
            if 'id' not in task:
                raise WorkflowInvalidError("任务定义缺少 'id' 字段")
            if 'operator' not in task:
                raise WorkflowInvalidError(f"任务 '{task['id']}' 缺少 'operator' 字段")

            self.tasks[task['id']] = task

        logger.info(f"加载工作流: {len(self.tasks)} 个任务")

    def topological_sort(self) -> List[str]:
        """
        Kahn 算法拓扑排序

        返回任务执行顺序列表

        Raises:
            WorkflowInvalidError: 如果工作流包含循环依赖
        """
        in_degree = defaultdict(int)
        graph = defaultdict(list)

        # 构建图和入度表
        for task_id, task in self.tasks.items():
            depends_on = task.get('depends_on', [])
            for dep in depends_on:
                if dep not in self.tasks:
                    raise WorkflowInvalidError(
                        f"任务 '{task_id}' 依赖的任务 '{dep}' 不存在"
                    )
                graph[dep].append(task_id)
                in_degree[task_id] += 1

        # 找到所有入度为 0 的节点（起始任务）
        queue = deque([
            task_id for task_id in self.tasks.keys()
            if in_degree[task_id] == 0
        ])

        sorted_tasks = []

        while queue:
            task_id = queue.popleft()
            sorted_tasks.append(task_id)

            # 移除该节点，更新相邻节点的入度
            for neighbor in graph[task_id]:
                in_degree[neighbor] -= 1
                if in_degree[neighbor] == 0:
                    queue.append(neighbor)

        # 如果排序后的任务数少于总任务数，说明存在循环依赖
        if len(sorted_tasks) != len(self.tasks):
            missing_tasks = set(self.tasks.keys()) - set(sorted_tasks)
            raise WorkflowInvalidError(
                f"工作流包含循环依赖，涉及任务: {', '.join(missing_tasks)}"
            )

        logger.info(f"拓扑排序完成: {' -> '.join(sorted_tasks)}")
        return sorted_tasks

    def resolve_params(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """
        解析参数引用（$ref）

        支持两种引用格式：
        - {"$ref": "task1"} - 引用任务的默认输出
        - {"$ref": "task1.output_port"} - 引用任务的特定输出端口（预留）

        Args:
            params: 原始参数字典

        Returns:
            解析后的参数字典
        """
        resolved = {}

        for key, value in params.items():
            if isinstance(value, dict) and "$ref" in value:
                ref = value["$ref"]

                # 解析引用（支持 "task1" 或 "task1.port" 格式）
                if "." in ref:
                    ref_task_id, port_name = ref.split(".", 1)
                    # 当前简化版只支持单输出，暂不实现多输出端口
                    logger.warning(f"多输出端口引用 '{ref}' 当前未实现，使用默认输出")
                    ref_task_id = ref.split(".")[0]
                else:
                    ref_task_id = ref

                # 从结果中获取引用的任务输出
                if ref_task_id not in self.results:
                    raise WorkflowInvalidError(
                        f"参数引用的任务 '{ref_task_id}' 未执行或不存在"
                    )

                resolved[key] = self.results[ref_task_id]
                logger.debug(f"参数 '{key}' 引用任务 '{ref_task_id}' 的结果: {resolved[key]}")
            else:
                resolved[key] = value

        return resolved

    def execute(self, input_data: Dict = None) -> Dict[str, Any]:
        """
        执行工作流

        Args:
            input_data: 输入数据（可选，当前版本未使用）

        Returns:
            所有任务的执行结果字典 {task_id: result}
        """
        from operators import get_operator_function

        # 拓扑排序获取执行顺序
        task_order = self.topological_sort()

        logger.info(f"开始执行工作流: {len(task_order)} 个任务")

        # 按顺序执行任务
        for task_id in task_order:
            task = self.tasks[task_id]
            operator_name = task['operator']
            params = task.get('params', {})

            logger.info(f"执行任务 '{task_id}': 算子 '{operator_name}'")

            # 解析参数引用
            resolved_params = self.resolve_params(params)

            # 获取算子函数并执行
            try:
                operator_func = get_operator_function(operator_name)
                result = operator_func(**resolved_params)

                # 存储结果到内存
                self.results[task_id] = result

                logger.info(f"任务 '{task_id}' 执行成功: 结果 = {result}")

            except Exception as e:
                logger.error(f"任务 '{task_id}' 执行失败: {e}")
                raise

        logger.info(f"工作流执行完成: {len(self.results)} 个任务成功")
        return self.results
