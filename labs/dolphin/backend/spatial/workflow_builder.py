"""
DolphinScheduler 工作流模板生成器
根据空间算子自动生成 DolphinScheduler 任务定义
"""

from typing import List, Dict, Any
from dataclasses import dataclass
import json


@dataclass
class WorkflowNode:
    """工作流节点定义"""
    node_id: str
    operator_code: str
    params: Dict[str, Any]
    upstream_nodes: List[str] = None  # 上游节点 ID 列表

    def __post_init__(self):
        if self.upstream_nodes is None:
            self.upstream_nodes = []


class DolphinWorkflowBuilder:
    """DolphinScheduler 工作流构建器"""

    def __init__(self, project_name: str, workflow_name: str):
        self.project_name = project_name
        self.workflow_name = workflow_name
        self.nodes: List[WorkflowNode] = []

    def add_node(self, node: WorkflowNode):
        """添加节点"""
        self.nodes.append(node)
        return self

    def build_task_definition(self, node: WorkflowNode) -> Dict[str, Any]:
        """
        生成单个任务的 DolphinScheduler 定义
        对应 DolphinScheduler 的 Python 任务类型
        """
        # 构造传递给 operator_executor.py 的参数
        task_config = {
            "operator": node.operator_code,
            "params": node.params
        }

        return {
            "code": node.node_id,
            "name": f"{node.operator_code}_{node.node_id}",
            "taskType": "PYTHON",  # 使用 Python 任务类型
            "taskParams": {
                "rawScript": f"""
import subprocess
import json

# 执行空间算子
task_config = {json.dumps(task_config, ensure_ascii=False)}
result = subprocess.run(
    ['python3', '/path/to/operator_executor.py', json.dumps(task_config)],
    capture_output=True,
    text=True
)

print(result.stdout)
if result.returncode != 0:
    raise Exception(result.stderr)
""".strip(),
                "localParams": []
            },
            "flag": "YES",
            "taskPriority": "MEDIUM",
            "workerGroup": "default",
            "failRetryTimes": 2,
            "failRetryInterval": 1,
            "timeoutFlag": "CLOSE",
            "timeoutNotifyStrategy": "",
            "timeout": 0
        }

    def build_workflow_definition(self) -> Dict[str, Any]:
        """生成完整的工作流定义"""
        tasks = []
        task_relations = []

        for node in self.nodes:
            # 添加任务定义
            tasks.append(self.build_task_definition(node))

            # 添加任务依赖关系
            for upstream_id in node.upstream_nodes:
                task_relations.append({
                    "name": "",
                    "preTaskCode": upstream_id,
                    "preTaskVersion": 1,
                    "postTaskCode": node.node_id,
                    "postTaskVersion": 1,
                    "conditionType": "NONE",
                    "conditionParams": {}
                })

        return {
            "name": self.workflow_name,
            "description": f"空间算子工作流: {self.workflow_name}",
            "globalParams": [],
            "tasks": tasks,
            "taskRelationList": task_relations,
            "tenantCode": "default",
            "timeout": 0,
            "executionType": "PARALLEL"
        }

    def to_json(self) -> str:
        """导出为 JSON 字符串"""
        return json.dumps(self.build_workflow_definition(), indent=2, ensure_ascii=False)


# ========================================
# 示例：构建空间分析工作流
# ========================================

def create_buffer_intersection_workflow():
    """
    示例工作流：
    1. 对几何 A 做 100 米缓冲区
    2. 对几何 B 做 50 米缓冲区
    3. 计算两个缓冲区的交集
    """
    builder = DolphinWorkflowBuilder(
        project_name="spatial_analysis",
        workflow_name="buffer_intersection_demo"
    )

    # 节点 1: 对几何 A 做缓冲区
    node1 = WorkflowNode(
        node_id="node_1",
        operator_code="buffer",
        params={
            "input_geom": {
                "type": "Point",
                "coordinates": [116.404, 39.915]  # 北京天安门
            },
            "distance": 100.0,
            "segments": 16
        }
    )

    # 节点 2: 对几何 B 做缓冲区
    node2 = WorkflowNode(
        node_id="node_2",
        operator_code="buffer",
        params={
            "input_geom": {
                "type": "Point",
                "coordinates": [116.405, 39.916]  # 附近点
            },
            "distance": 50.0,
            "segments": 16
        }
    )

    # 节点 3: 计算交集（依赖节点 1 和 2）
    node3 = WorkflowNode(
        node_id="node_3",
        operator_code="intersection",
        params={
            "geom_a": "${{ task.node_1.output }}",  # 引用上游节点输出
            "geom_b": "${{ task.node_2.output }}"
        },
        upstream_nodes=["node_1", "node_2"]
    )

    builder.add_node(node1).add_node(node2).add_node(node3)
    return builder


if __name__ == "__main__":
    # 生成工作流定义
    workflow = create_buffer_intersection_workflow()
    print(workflow.to_json())