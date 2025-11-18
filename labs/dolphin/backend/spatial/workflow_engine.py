"""
空间分析工作流引擎
轻量级内存计算引擎，用于高效执行空间算子工作流
"""

from typing import Dict, Any, List, Callable, Optional
from dataclasses import dataclass, field
from collections import defaultdict
import time
import json

from spatial.task_ref import TaskRef, TaskOutput, resolve_refs, is_task_ref


@dataclass
class Task:
    """工作流任务定义"""
    id: str
    operator_code: str
    params: Dict[str, Any]
    dependencies: List[str] = field(default_factory=list)
    description: str = ""

    def __post_init__(self):
        """自动检测依赖关系"""
        if not self.dependencies:
            self.dependencies = self._extract_dependencies()

    def _extract_dependencies(self) -> List[str]:
        """从参数中提取依赖的任务 ID"""
        deps = set()

        def extract_from_value(value):
            if is_task_ref(value):
                deps.add(value.task_id)
            elif isinstance(value, dict):
                for v in value.values():
                    extract_from_value(v)
            elif isinstance(value, list):
                for item in value:
                    extract_from_value(item)

        for param_value in self.params.values():
            extract_from_value(param_value)

        return list(deps)


@dataclass
class TaskExecutionResult:
    """任务执行结果"""
    task_id: str
    operator_code: str
    success: bool
    output: Any = None
    error: Optional[str] = None
    duration_ms: float = 0.0
    timestamp: str = ""


class SpatialWorkflowEngine:
    """
    空间分析工作流引擎

    特点:
    - 内存计算（无 I/O 开销）
    - 自动依赖解析
    - 拓扑排序执行
    - 详细执行日志

    示例:
        engine = SpatialWorkflowEngine()

        # 添加任务
        engine.add_task("buffer1", "buffer",
                       input_geom={...}, distance=100)
        engine.add_task("buffer2", "buffer",
                       input_geom={...}, distance=50)
        engine.add_task("intersection", "intersection",
                       geom_a=TaskRef("buffer1"),
                       geom_b=TaskRef("buffer2"))

        # 执行工作流
        results = engine.run()
        final_result = results["intersection"]
    """

    def __init__(self, verbose: bool = True):
        """
        初始化工作流引擎

        Args:
            verbose: 是否打印详细执行日志
        """
        self.tasks: List[Task] = []
        self.context: Dict[str, Any] = {}  # 内存上下文（存储任务输出）
        self.operators: Dict[str, Callable] = {}
        self.execution_results: List[TaskExecutionResult] = []
        self.verbose = verbose

        # 注册所有可用算子
        self._register_operators()

    def _register_operators(self):
        """注册所有可用空间算子"""
        from spatial.operators import (
            buffer, intersection, union, centroid,
            contains, intersects, distance
        )

        self.operators = {
            'buffer': buffer,
            'intersection': intersection,
            'union': union,
            'centroid': centroid,
            'contains': contains,
            'intersects': intersects,
            'distance': distance,
        }

        if self.verbose:
            print(f"📦 已注册 {len(self.operators)} 个空间算子")

    def register_operator(self, code: str, func: Callable):
        """
        注册自定义算子

        Args:
            code: 算子代码
            func: 算子函数
        """
        self.operators[code] = func
        if self.verbose:
            print(f"✅ 注册自定义算子: {code}")

    def add_task(self, task_id: str, operator_code: str,
                 description: str = "", **params) -> str:
        """
        添加任务到工作流

        Args:
            task_id: 任务唯一标识
            operator_code: 算子代码
            description: 任务描述（可选）
            **params: 算子参数（可包含 TaskRef）

        Returns:
            task_id: 用于后续引用

        示例:
            engine.add_task("buf1", "buffer",
                           input_geom={...}, distance=100)
            engine.add_task("buf2", "buffer",
                           input_geom={...}, distance=50)
            engine.add_task("result", "intersection",
                           geom_a=TaskRef("buf1"),
                           geom_b=TaskRef("buf2"))
        """
        if operator_code not in self.operators:
            raise ValueError(
                f"未知算子: {operator_code}。"
                f"可用算子: {list(self.operators.keys())}"
            )

        task = Task(
            id=task_id,
            operator_code=operator_code,
            params=params,
            description=description
        )

        self.tasks.append(task)

        if self.verbose:
            deps_str = f" (依赖: {task.dependencies})" if task.dependencies else ""
            print(f"➕ 添加任务: {task_id} ({operator_code}){deps_str}")

        return task_id

    def run(self) -> Dict[str, Any]:
        """
        执行工作流

        Returns:
            所有任务的输出结果字典 {task_id: output}

        Raises:
            ValueError: 任务依赖循环
            RuntimeError: 任务执行失败
        """
        if not self.tasks:
            raise ValueError("工作流为空，请先添加任务")

        if self.verbose:
            print("\n" + "=" * 60)
            print(f"🚀 开始执行工作流（共 {len(self.tasks)} 个任务）")
            print("=" * 60)

        # 拓扑排序
        sorted_tasks = self._topological_sort()

        # 执行任务
        start_time = time.time()

        for i, task in enumerate(sorted_tasks, 1):
            self._execute_task(task, i, len(sorted_tasks))

        total_duration = (time.time() - start_time) * 1000

        if self.verbose:
            print("=" * 60)
            print(f"✅ 工作流执行完成（总耗时: {total_duration:.2f}ms）")
            print("=" * 60)
            self._print_summary()

        return self.context

    def _execute_task(self, task: Task, index: int, total: int):
        """执行单个任务"""
        task_start = time.time()

        if self.verbose:
            print(f"\n[{index}/{total}] 🔄 执行任务: {task.id} ({task.operator_code})")
            if task.description:
                print(f"    描述: {task.description}")

        try:
            # 解析参数（将 TaskRef 替换为实际数据）
            resolved_params = resolve_refs(task.params, self.context)

            if self.verbose:
                print(f"    参数: {self._format_params(resolved_params)}")

            # 执行算子（内存计算）
            operator_func = self.operators[task.operator_code]
            result = operator_func(**resolved_params)

            # 存储到内存上下文
            self.context[task.id] = result

            duration_ms = (time.time() - task_start) * 1000

            # 记录执行结果
            exec_result = TaskExecutionResult(
                task_id=task.id,
                operator_code=task.operator_code,
                success=True,
                output=result,
                duration_ms=duration_ms,
                timestamp=time.strftime("%Y-%m-%d %H:%M:%S")
            )
            self.execution_results.append(exec_result)

            if self.verbose:
                result_preview = self._format_result(result)
                print(f"    ✅ 完成 ({duration_ms:.2f}ms) - 输出: {result_preview}")

        except Exception as e:
            duration_ms = (time.time() - task_start) * 1000

            # 记录失败结果
            exec_result = TaskExecutionResult(
                task_id=task.id,
                operator_code=task.operator_code,
                success=False,
                error=str(e),
                duration_ms=duration_ms,
                timestamp=time.strftime("%Y-%m-%d %H:%M:%S")
            )
            self.execution_results.append(exec_result)

            if self.verbose:
                print(f"    ❌ 失败 ({duration_ms:.2f}ms)")
                print(f"    错误: {e}")

            raise RuntimeError(f"任务 '{task.id}' 执行失败: {e}") from e

    def _topological_sort(self) -> List[Task]:
        """
        拓扑排序（确保按依赖顺序执行）

        Returns:
            排序后的任务列表

        Raises:
            ValueError: 存在循环依赖
        """
        # 构建任务字典
        task_dict = {t.id: t for t in self.tasks}

        # 深度优先搜索
        visited = set()
        temp_mark = set()
        sorted_tasks = []

        def visit(task_id: str):
            if task_id in temp_mark:
                raise ValueError(f"检测到循环依赖: {task_id}")

            if task_id in visited:
                return

            temp_mark.add(task_id)

            task = task_dict.get(task_id)
            if task is None:
                raise ValueError(f"任务 '{task_id}' 未定义，但被其他任务依赖")

            for dep_id in task.dependencies:
                visit(dep_id)

            temp_mark.remove(task_id)
            visited.add(task_id)
            sorted_tasks.append(task)

        for task in self.tasks:
            if task.id not in visited:
                visit(task.id)

        if self.verbose:
            print(f"\n📋 任务执行顺序: {[t.id for t in sorted_tasks]}")

        return sorted_tasks

    def get_result(self, task_id: str) -> Any:
        """获取指定任务的输出结果"""
        if task_id not in self.context:
            raise ValueError(f"任务 '{task_id}' 的结果不存在（可能未执行）")
        return self.context[task_id]

    def get_execution_stats(self) -> Dict[str, Any]:
        """获取执行统计信息"""
        total_duration = sum(r.duration_ms for r in self.execution_results)
        success_count = sum(1 for r in self.execution_results if r.success)

        return {
            "total_tasks": len(self.execution_results),
            "success_count": success_count,
            "failed_count": len(self.execution_results) - success_count,
            "total_duration_ms": total_duration,
            "avg_duration_ms": total_duration / len(self.execution_results) if self.execution_results else 0,
            "results": self.execution_results
        }

    def export_lineage(self) -> str:
        """
        导出数据血缘关系图（Mermaid 格式）

        Returns:
            Mermaid 图定义字符串
        """
        lines = ["graph LR"]

        for task in self.tasks:
            # 节点定义
            lines.append(f"    {task.id}[{task.operator_code}]")

            # 边定义
            for dep_id in task.dependencies:
                lines.append(f"    {dep_id} --> {task.id}")

        return "\n".join(lines)

    def _format_params(self, params: Dict[str, Any]) -> str:
        """格式化参数用于日志显示"""
        formatted = {}
        for key, value in params.items():
            if isinstance(value, dict) and "type" in value:
                # GeoJSON 简化显示
                formatted[key] = f"<{value['type']}>"
            elif isinstance(value, (int, float, str, bool)):
                formatted[key] = value
            else:
                formatted[key] = type(value).__name__

        return json.dumps(formatted, ensure_ascii=False)

    def _format_result(self, result: Any) -> str:
        """格式化结果用于日志显示"""
        if isinstance(result, dict) and "type" in result:
            return f"<{result['type']} geometry>"
        elif isinstance(result, (int, float, bool)):
            return str(result)
        elif isinstance(result, str):
            return f'"{result[:50]}..."' if len(result) > 50 else f'"{result}"'
        elif isinstance(result, TaskOutput):
            return str(result)
        else:
            return f"<{type(result).__name__}>"

    def _print_summary(self):
        """打印执行摘要"""
        stats = self.get_execution_stats()

        print("\n📊 执行统计:")
        print(f"  总任务数: {stats['total_tasks']}")
        print(f"  成功: {stats['success_count']}")
        print(f"  失败: {stats['failed_count']}")
        print(f"  总耗时: {stats['total_duration_ms']:.2f}ms")
        print(f"  平均耗时: {stats['avg_duration_ms']:.2f}ms/任务")

        # 最慢的 3 个任务
        sorted_by_duration = sorted(
            self.execution_results,
            key=lambda r: r.duration_ms,
            reverse=True
        )[:3]

        if sorted_by_duration:
            print("\n⏱️  最慢的任务:")
            for r in sorted_by_duration:
                print(f"  - {r.task_id} ({r.operator_code}): {r.duration_ms:.2f}ms")


# ========================================
# 测试代码
# ========================================

if __name__ == "__main__":
    print("测试 SpatialWorkflowEngine")
    print("=" * 60)

    # 创建工作流引擎
    engine = SpatialWorkflowEngine(verbose=True)

    # 示例 1: 简单工作流（缓冲区 → 交集）
    print("\n【示例 1】缓冲区交集分析")
    print("-" * 60)

    engine.add_task(
        "buffer1",
        "buffer",
        description="对天安门创建 100 米缓冲区",
        input_geom={"type": "Point", "coordinates": [116.404, 39.915]},
        distance=100.0,
        segments=16
    )

    engine.add_task(
        "buffer2",
        "buffer",
        description="对附近点创建 50 米缓冲区",
        input_geom={"type": "Point", "coordinates": [116.405, 39.916]},
        distance=50.0,
        segments=16
    )

    engine.add_task(
        "intersection",
        "intersection",
        description="计算两个缓冲区的交集",
        geom_a=TaskRef("buffer1"),
        geom_b=TaskRef("buffer2")
    )

    # 执行工作流
    results = engine.run()

    # 查看结果
    print("\n📦 最终结果:")
    print(f"  交集几何: {results['intersection']['type']}")

    # 导出血缘图
    print("\n🔗 数据血缘关系:")
    print(engine.export_lineage())

    # 示例 2: 复杂工作流（并行 + 聚合）
    print("\n\n【示例 2】多点缓冲区合并")
    print("-" * 60)

    engine2 = SpatialWorkflowEngine(verbose=True)

    # 并行创建 3 个缓冲区
    points = [
        ("天安门", [116.404, 39.915]),
        ("故宫", [116.397, 39.916]),
        ("景山公园", [116.395, 39.926])
    ]

    buffer_tasks = []
    for name, coords in points:
        task_id = f"buffer_{name}"
        engine2.add_task(
            task_id,
            "buffer",
            description=f"{name} 缓冲区 50m",
            input_geom={"type": "Point", "coordinates": coords},
            distance=50.0,
            segments=8
        )
        buffer_tasks.append(task_id)

    # 合并所有缓冲区
    engine2.add_task(
        "union_all",
        "union",
        description="合并所有缓冲区",
        geometries=[TaskRef(tid) for tid in buffer_tasks]
    )

    # 计算质心
    engine2.add_task(
        "centroid",
        "centroid",
        description="计算合并后几何的质心",
        input_geom=TaskRef("union_all")
    )

    # 执行工作流
    results2 = engine2.run()

    # 查看结果
    print("\n📦 最终结果:")
    print(f"  质心坐标: {results2['centroid']['coordinates']}")

    print("\n🎉 测试完成！")
