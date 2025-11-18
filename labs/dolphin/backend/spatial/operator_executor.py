"""
空间算子任务执行器
将算子封装为 DolphinScheduler 可执行的 Python 脚本
"""

import json
import sys
from pathlib import Path
from typing import Dict, Any

# 添加 backend 目录到 Python 路径
sys.path.insert(0, str(Path(__file__).parent.parent))

from spatial.operator_registry import registry
from spatial.operators import buffer, intersection, union, centroid  # 实际算子实现


class OperatorExecutor:
    """算子执行器"""

    def __init__(self, operator_code: str, params: Dict[str, Any]):
        self.operator_code = operator_code
        self.params = params
        self.operator = registry.get(operator_code)

        if not self.operator:
            raise ValueError(f"Unknown operator: {operator_code}")

    def validate_params(self) -> bool:
        """验证参数完整性"""
        for param_def in self.operator.input_params:
            value = self.params.get(param_def.name)
            if not param_def.validate(value):
                raise ValueError(
                    f"Invalid parameter '{param_def.name}': "
                    f"required={param_def.required}, value={value}"
                )
        return True

    def execute(self) -> Dict[str, Any]:
        """执行算子"""
        self.validate_params()

        # 根据算子类型调用对应的实现函数
        if self.operator_code == "buffer":
            result = buffer(
                self.params["input_geom"],
                self.params["distance"],
                self.params.get("segments", 8)
            )

        elif self.operator_code == "intersection":
            result = intersection(
                self.params["geom_a"],
                self.params["geom_b"]
            )

        elif self.operator_code == "union":
            result = union(self.params["geometries"])

        elif self.operator_code == "centroid":
            result = centroid(self.params["input_geom"])

        # ... 其他算子实现 ...

        else:
            raise NotImplementedError(f"Operator '{self.operator_code}' not implemented")

        return {
            "status": "success",
            "operator": self.operator_code,
            "result": result
        }


def main():
    """
    主入口函数
    从 DolphinScheduler 传入的参数格式：
    python operator_executor.py '{"operator": "buffer", "params": {...}}'
    """
    if len(sys.argv) < 2:
        print(json.dumps({
            "status": "error",
            "message": "Missing task config argument"
        }))
        sys.exit(1)

    try:
        # 解析任务配置
        task_config = json.loads(sys.argv[1])
        operator_code = task_config["operator"]
        params = task_config["params"]

        # 执行算子
        executor = OperatorExecutor(operator_code, params)
        result = executor.execute()

        # 输出结果（DolphinScheduler 会捕获 stdout）
        print(json.dumps(result, ensure_ascii=False, indent=2))
        sys.exit(0)

    except Exception as e:
        error_result = {
            "status": "error",
            "message": str(e)
        }
        print(json.dumps(error_result, ensure_ascii=False, indent=2))
        sys.exit(1)


if __name__ == "__main__":
    main()