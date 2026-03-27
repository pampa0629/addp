"""
工作流验证 Chain

实现四层验证：
1. 结构验证：检查基本结构是否正确
2. 唯一性验证：检查任务 ID 是否唯一
3. 依赖验证：检查依赖有效性和循环依赖
4. 参数验证：检查参数名称、类型、必需性
"""
from typing import List, Set, Dict
from collections import deque

from models.workflow_models import Workflow, ValidationResult, Task
from tools.develop_tools import OperatorDetailTool


class WorkflowValidationChain:
    """
    工作流验证 Chain

    功能：
    1. 四层验证（结构/唯一性/依赖/参数）
    2. 生成详细的错误信息和修复建议
    3. 支持警告（非致命错误）
    """

    def __init__(self, operator_detail_tool: OperatorDetailTool):
        """
        初始化工作流验证 Chain

        Args:
            operator_detail_tool: 算子详情 Tool（用于参数验证）
        """
        self.operator_detail_tool = operator_detail_tool

    async def validate(self, workflow: Workflow, engine_type: str = "python_workflow") -> ValidationResult:
        """
        执行完整的工作流验证

        Args:
            workflow: 待验证的工作流
            engine_type: 工作流引擎类型（python_workflow/spark_workflow/math_workflow），用于获取正确的算子定义

        Returns:
            ValidationResult: 验证结果（包含错误、警告、建议）
        """
        print(f"[WorkflowValidationChain] 开始验证工作流（{len(workflow.tasks)} 个任务，引擎: {engine_type}）")

        errors = []
        warnings = []
        suggestions = []

        # 第 1 层：结构验证
        print(f"[WorkflowValidationChain] 第 1 层：结构验证")
        structure_errors = self._validate_structure(workflow)
        errors.extend(structure_errors)

        # 如果结构验证失败，后续验证可能无意义
        if structure_errors:
            print(f"[WorkflowValidationChain] ❌ 结构验证失败，跳过后续验证")
            return ValidationResult(
                is_valid=False,
                errors=errors,
                warnings=warnings,
                suggestions=self._generate_suggestions(errors, warnings)
            )

        # 第 2 层：唯一性验证
        print(f"[WorkflowValidationChain] 第 2 层：唯一性验证")
        uniqueness_errors = self._validate_uniqueness(workflow)
        errors.extend(uniqueness_errors)

        # 第 3 层：依赖验证
        print(f"[WorkflowValidationChain] 第 3 层：依赖验证")
        dependency_errors, dependency_warnings = self._validate_dependencies(workflow)
        errors.extend(dependency_errors)
        warnings.extend(dependency_warnings)

        # 第 4 层：参数验证（异步，传递 engine_type）
        print(f"[WorkflowValidationChain] 第 4 层：参数验证")
        param_errors, param_warnings = await self._validate_parameters(workflow, engine_type)
        errors.extend(param_errors)
        warnings.extend(param_warnings)

        # 生成修复建议
        suggestions = self._generate_suggestions(errors, warnings)

        is_valid = len(errors) == 0

        if is_valid:
            print(f"[WorkflowValidationChain] ✅ 验证通过（{len(warnings)} 个警告）")
        else:
            print(f"[WorkflowValidationChain] ❌ 验证失败（{len(errors)} 个错误，{len(warnings)} 个警告）")

        return ValidationResult(
            is_valid=is_valid,
            errors=errors,
            warnings=warnings,
            suggestions=suggestions
        )

    def _validate_structure(self, workflow: Workflow) -> List[str]:
        """
        第 1 层：结构验证

        检查：
        - tasks 字段是否存在且为列表
        - 至少有一个任务
        - 每个任务有必需字段（id、operator、params）

        Args:
            workflow: 待验证的工作流

        Returns:
            错误列表
        """
        errors = []

        # 检查 tasks 是否存在
        if not hasattr(workflow, "tasks"):
            errors.append("工作流缺少 'tasks' 字段")
            return errors

        # 检查 tasks 是否为列表
        if not isinstance(workflow.tasks, list):
            errors.append("'tasks' 字段必须是数组")
            return errors

        # 检查至少有一个任务
        if len(workflow.tasks) == 0:
            errors.append("工作流至少需要一个任务")
            return errors

        # 检查每个任务的必需字段
        for i, task in enumerate(workflow.tasks):
            if not hasattr(task, "id") or not task.id:
                errors.append(f"任务 #{i+1} 缺少 'id' 字段")

            if not hasattr(task, "operator") or not task.operator:
                errors.append(f"任务 #{i+1} (id={getattr(task, 'id', '?')}) 缺少 'operator' 字段")

            if not hasattr(task, "params"):
                errors.append(f"任务 #{i+1} (id={getattr(task, 'id', '?')}) 缺少 'params' 字段")

            if not hasattr(task, "depends_on"):
                errors.append(f"任务 #{i+1} (id={getattr(task, 'id', '?')}) 缺少 'depends_on' 字段")

        return errors

    def _validate_uniqueness(self, workflow: Workflow) -> List[str]:
        """
        第 2 层：唯一性验证

        检查：
        - 所有任务 ID 是否唯一

        Args:
            workflow: 待验证的工作流

        Returns:
            错误列表
        """
        errors = []

        # 统计任务 ID 出现次数
        task_id_counts = {}
        for task in workflow.tasks:
            task_id = task.id
            task_id_counts[task_id] = task_id_counts.get(task_id, 0) + 1

        # 找出重复的 ID
        duplicates = [task_id for task_id, count in task_id_counts.items() if count > 1]

        if duplicates:
            errors.append(
                f"任务 ID 重复: {', '.join(duplicates)} "
                f"（每个任务 ID 必须唯一）"
            )

        return errors

    def _validate_dependencies(self, workflow: Workflow) -> tuple[List[str], List[str]]:
        """
        第 3 层：依赖验证

        检查：
        - 依赖的任务 ID 是否存在
        - 是否有循环依赖（使用拓扑排序检测）
        - 是否有孤立任务（没有依赖也不被依赖，除了 load/save）

        Args:
            workflow: 待验证的工作流

        Returns:
            (错误列表, 警告列表)
        """
        errors = []
        warnings = []

        # 构建任务 ID 集合
        task_ids = {task.id for task in workflow.tasks}

        # 构建依赖图（邻接表）
        dependency_graph: Dict[str, List[str]] = {task.id: [] for task in workflow.tasks}
        in_degree: Dict[str, int] = {task.id: 0 for task in workflow.tasks}

        # 检查依赖存在性 + 构建依赖图
        for task in workflow.tasks:
            for dep in task.depends_on:
                # 检查依赖的任务是否存在
                if dep not in task_ids:
                    errors.append(
                        f"任务 '{task.id}' 依赖不存在的任务 '{dep}'"
                    )
                else:
                    # 添加边：dep → task.id
                    dependency_graph[dep].append(task.id)
                    in_degree[task.id] += 1

        # 检查循环依赖（Kahn 拓扑排序算法）
        if not errors:  # 只在没有依赖不存在错误时检查循环依赖
            has_cycle = self._has_cycle(dependency_graph, in_degree)
            if has_cycle:
                errors.append("工作流存在循环依赖，无法执行")

        # 警告：孤立任务（没有依赖也不被依赖，且不是 load/save）
        for task in workflow.tasks:
            is_isolated = (
                len(task.depends_on) == 0 and
                len(dependency_graph[task.id]) == 0
            )

            if is_isolated and task.operator not in ["load", "save"]:
                warnings.append(
                    f"任务 '{task.id}' ({task.operator}) 是孤立任务（没有输入也没有输出）"
                )

        return errors, warnings

    def _has_cycle(
        self,
        graph: Dict[str, List[str]],
        in_degree: Dict[str, int]
    ) -> bool:
        """
        使用 Kahn 算法检测有向图是否有环

        Args:
            graph: 邻接表（task_id -> [依赖它的任务列表]）
            in_degree: 每个任务的入度

        Returns:
            True 如果有环，False 如果无环
        """
        # 拷贝入度字典（避免修改原始数据）
        in_degree = in_degree.copy()

        # 初始化队列（所有入度为 0 的节点）
        queue = deque([task_id for task_id, degree in in_degree.items() if degree == 0])

        # 拓扑排序
        sorted_count = 0

        while queue:
            current = queue.popleft()
            sorted_count += 1

            # 减少所有依赖当前节点的节点的入度
            for neighbor in graph[current]:
                in_degree[neighbor] -= 1
                if in_degree[neighbor] == 0:
                    queue.append(neighbor)

        # 如果排序的节点数量少于总节点数，说明有环
        return sorted_count < len(graph)

    async def _validate_parameters(self, workflow: Workflow, engine_type: str = "python_workflow") -> tuple[List[str], List[str]]:
        """
        第 4 层：参数验证

        检查：
        - 算子是否存在
        - 必需参数是否提供
        - 参数名称是否正确（不在算子定义中的参数）
        - 参数引用格式是否正确（{{task_id.output_name}}）

        Args:
            workflow: 待验证的工作流
            engine_type: 工作流引擎类型（python_workflow/spark_workflow/math_workflow）

        Returns:
            (错误列表, 警告列表)
        """
        errors = []
        warnings = []

        # 构建任务 ID 到输出名称的映射（用于验证参数引用）
        task_outputs: Dict[str, Set[str]] = {}

        for task in workflow.tasks:
            # 1. 获取算子定义（传递 engine_type）
            operator_def = await self.operator_detail_tool._arun(operator_name=task.operator, engine_type=engine_type)

            if not operator_def:
                errors.append(
                    f"任务 '{task.id}' 使用了未知算子 '{task.operator}'"
                )
                continue

            # 2. 构建算子参数集合
            defined_params = {p["name"] for p in operator_def.get("parameters", [])}
            required_params = {
                p["name"] for p in operator_def.get("parameters", [])
                if p.get("required", False)
            }

            # 3. 检查必需参数
            provided_params = set(task.params.keys()) if task.params else set()

            missing_params = required_params - provided_params
            if missing_params:
                errors.append(
                    f"任务 '{task.id}' 缺少必需参数: {', '.join(missing_params)}"
                )

            # 4. 检查参数名称
            unknown_params = provided_params - defined_params
            if unknown_params:
                errors.append(
                    f"任务 '{task.id}' 包含未定义的参数: {', '.join(unknown_params)}"
                )

            # 5. 记录输出（用于后续参数引用验证）
            operator_outputs = operator_def.get("outputs", [])
            task_outputs[task.id] = {out["name"] for out in operator_outputs}

            # 6. 检查参数引用格式（警告级别）
            if task.params:
                for param_name, param_value in task.params.items():
                    if isinstance(param_value, str) and "{{" in param_value:
                        # 检查引用格式
                        if not self._is_valid_reference(param_value):
                            warnings.append(
                                f"任务 '{task.id}' 的参数 '{param_name}' 引用格式可能不正确: {param_value} "
                                f"（正确格式: {{{{task_id.output_name}}}}）"
                            )

        return errors, warnings

    def _is_valid_reference(self, value: str) -> bool:
        """
        检查参数引用格式是否正确

        正确格式: {{task_id.output_name}} 或 {{task_id.output_name[0]}}

        Args:
            value: 参数值

        Returns:
            True 如果格式正确
        """
        import re

        # 简单的正则匹配：{{任意字符.任意字符}}
        pattern = r'\{\{[\w]+\.[\w\[\]]+\}\}'
        matches = re.findall(pattern, value)

        # 如果包含 {{，则必须至少有一个匹配
        if "{{" in value:
            return len(matches) > 0

        return True

    def _generate_suggestions(self, errors: List[str], warnings: List[str]) -> List[str]:
        """
        根据错误和警告生成修复建议

        Args:
            errors: 错误列表
            warnings: 警告列表

        Returns:
            建议列表
        """
        suggestions = []

        # 根据错误类型生成建议
        for error in errors:
            if "缺少必需参数" in error:
                suggestions.append("请参考算子的 workflow_example，确保填写所有必需参数")

            elif "未定义的参数" in error:
                suggestions.append("检查参数名称拼写，确保与算子定义完全一致（区分大小写）")

            elif "循环依赖" in error:
                suggestions.append("使用拓扑排序调整任务顺序，确保依赖关系是有向无环图（DAG）")

            elif "依赖不存在" in error:
                suggestions.append("检查 depends_on 字段中的任务 ID 是否正确，确保依赖的任务存在于工作流中")

            elif "任务 ID 重复" in error:
                suggestions.append("为每个任务分配唯一的 ID（如 task1, task2, task3...）")

            elif "未知算子" in error:
                suggestions.append("检查算子名称是否正确，可以通过算子发现接口查看所有可用算子")

        # 根据警告生成建议
        for warning in warnings:
            if "引用格式可能不正确" in warning:
                suggestions.append("参数引用应使用 {{task_id.output_name}} 格式")

            elif "孤立任务" in warning:
                suggestions.append("孤立任务不会被执行，考虑删除或添加依赖关系")

        # 去重
        suggestions = list(set(suggestions))

        return suggestions


# 便捷函数：创建 WorkflowValidationChain 实例
def create_workflow_validation_chain(operator_detail_tool: OperatorDetailTool):
    """创建工作流验证 Chain 实例"""
    return WorkflowValidationChain(operator_detail_tool)
