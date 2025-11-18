"""
任务引用机制
用于在工作流中引用上游任务的输出（内存传递）
"""

from typing import Any, Optional


class TaskRef:
    """
    任务输出引用

    用于在工作流定义中引用上游任务的输出，运行时会自动替换为实际数据

    示例:
        engine.add_task("task1", "buffer", input_geom={...}, distance=100)
        engine.add_task("task2", "intersection",
                       geom_a=TaskRef("task1"),  # 引用 task1 的输出
                       geom_b={...})
    """

    def __init__(self, task_id: str, output_key: str = "result"):
        """
        初始化任务引用

        Args:
            task_id: 上游任务的 ID
            output_key: 输出键名（如果任务有多个输出）
        """
        self.task_id = task_id
        self.output_key = output_key

    def __repr__(self):
        if self.output_key == "result":
            return f"TaskRef({self.task_id})"
        return f"TaskRef({self.task_id}.{self.output_key})"

    def __str__(self):
        return self.__repr__()


class TaskOutput:
    """
    任务输出容器

    支持多个输出值（类似 Python 的 tuple unpacking）

    示例:
        # 算子返回多个值
        def spatial_join(left, right):
            return TaskOutput(
                result=joined_gdf,
                left_count=len(left),
                right_count=len(right),
                matched_count=len(joined_gdf)
            )

        # 引用特定输出
        TaskRef("join_task", "matched_count")
    """

    def __init__(self, result: Any = None, **kwargs):
        """
        初始化任务输出

        Args:
            result: 主要输出结果（默认输出）
            **kwargs: 额外的命名输出
        """
        self.outputs = {"result": result}
        self.outputs.update(kwargs)

    def get(self, key: str = "result") -> Any:
        """获取指定输出"""
        return self.outputs.get(key)

    def __getitem__(self, key: str) -> Any:
        """支持字典式访问"""
        return self.outputs[key]

    def __repr__(self):
        keys = list(self.outputs.keys())
        return f"TaskOutput({', '.join(keys)})"


def is_task_ref(obj: Any) -> bool:
    """判断对象是否为 TaskRef"""
    return isinstance(obj, TaskRef)


def resolve_refs(params: dict, context: dict) -> dict:
    """
    解析参数中的所有 TaskRef 引用

    Args:
        params: 原始参数字典（可能包含 TaskRef）
        context: 任务执行上下文（存储所有任务输出）

    Returns:
        解析后的参数字典（TaskRef 替换为实际数据）

    示例:
        params = {
            "geom_a": TaskRef("task1"),
            "geom_b": {"type": "Point", "coordinates": [0, 0]},
            "distance": 100
        }

        context = {
            "task1": {"type": "Polygon", "coordinates": [...]}
        }

        resolved = resolve_refs(params, context)
        # {
        #     "geom_a": {"type": "Polygon", "coordinates": [...]},
        #     "geom_b": {"type": "Point", "coordinates": [0, 0]},
        #     "distance": 100
        # }
    """
    resolved = {}

    for key, value in params.items():
        if is_task_ref(value):
            # 从上下文获取上游任务输出
            task_output = context.get(value.task_id)

            if task_output is None:
                raise ValueError(
                    f"任务 '{value.task_id}' 的输出未找到。"
                    f"可能原因：1) 任务未执行 2) 任务 ID 错误 3) 依赖顺序错误"
                )

            # 处理 TaskOutput 多值输出
            if isinstance(task_output, TaskOutput):
                resolved[key] = task_output.get(value.output_key)
            else:
                # 简单输出（向后兼容）
                if value.output_key != "result":
                    raise ValueError(
                        f"任务 '{value.task_id}' 返回简单值，不支持键 '{value.output_key}'"
                    )
                resolved[key] = task_output

        elif isinstance(value, dict):
            # 递归解析嵌套字典
            resolved[key] = resolve_refs(value, context)

        elif isinstance(value, list):
            # 递归解析列表
            resolved[key] = [
                resolve_refs({"item": item}, context)["item"] if isinstance(item, (dict, TaskRef))
                else item
                for item in value
            ]

        else:
            # 普通值直接复制
            resolved[key] = value

    return resolved


# ========================================
# 测试代码
# ========================================

if __name__ == "__main__":
    print("测试 TaskRef 和 resolve_refs")
    print("=" * 50)

    # 测试 1: 简单引用
    print("\n测试 1: 简单引用")
    params1 = {
        "geom_a": TaskRef("buffer1"),
        "geom_b": TaskRef("buffer2"),
        "distance": 100
    }

    context1 = {
        "buffer1": {"type": "Polygon", "coordinates": [[0, 0]]},
        "buffer2": {"type": "Polygon", "coordinates": [[1, 1]]}
    }

    resolved1 = resolve_refs(params1, context1)
    print(f"原始参数: {params1}")
    print(f"解析后: {resolved1}")
    assert resolved1["geom_a"]["type"] == "Polygon"
    print("✅ 测试 1 通过")

    # 测试 2: TaskOutput 多值输出
    print("\n测试 2: TaskOutput 多值输出")
    params2 = {
        "count": TaskRef("join_task", "matched_count"),
        "result": TaskRef("join_task", "result")
    }

    context2 = {
        "join_task": TaskOutput(
            result={"joined": "data"},
            matched_count=42,
            left_count=100,
            right_count=80
        )
    }

    resolved2 = resolve_refs(params2, context2)
    print(f"原始参数: {params2}")
    print(f"解析后: {resolved2}")
    assert resolved2["count"] == 42
    print("✅ 测试 2 通过")

    # 测试 3: 嵌套引用
    print("\n测试 3: 嵌套引用和列表")
    params3 = {
        "geometries": [
            TaskRef("task1"),
            TaskRef("task2"),
            {"type": "Point", "coordinates": [0, 0]}
        ],
        "options": {
            "buffer_geom": TaskRef("task3"),
            "distance": 50
        }
    }

    context3 = {
        "task1": {"type": "Polygon", "coordinates": [[1, 1]]},
        "task2": {"type": "Polygon", "coordinates": [[2, 2]]},
        "task3": {"type": "Polygon", "coordinates": [[3, 3]]}
    }

    resolved3 = resolve_refs(params3, context3)
    print(f"原始参数: {params3}")
    print(f"解析后: {resolved3}")
    assert len(resolved3["geometries"]) == 3
    assert resolved3["options"]["buffer_geom"]["type"] == "Polygon"
    print("✅ 测试 3 通过")

    # 测试 4: 错误处理
    print("\n测试 4: 错误处理（任务未找到）")
    try:
        params4 = {"geom": TaskRef("nonexistent_task")}
        context4 = {}
        resolve_refs(params4, context4)
        print("❌ 应该抛出异常")
    except ValueError as e:
        print(f"✅ 正确捕获异常: {e}")

    print("\n" + "=" * 50)
    print("🎉 所有测试通过！")
