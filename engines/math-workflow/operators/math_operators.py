"""
Math Workflow Engine - 数学算子实现

提供 5 个基础数学算子：add、subtract、multiply、divide、average
"""

from typing import List
from .base import OperatorMetadata, ParameterMetadata, OutputPortMetadata


# ============ 算子函数实现 ============

def add(a: float, b: float) -> float:
    """加法算子"""
    return a + b


def subtract(a: float, b: float) -> float:
    """减法算子"""
    return a - b


def multiply(a: float, b: float) -> float:
    """乘法算子"""
    return a * b


def divide(a: float, b: float) -> float:
    """除法算子（处理除零错误）"""
    if b == 0:
        raise ValueError("除数不能为零")
    return a / b


def average(values: List[float]) -> float:
    """平均值算子"""
    if not values or len(values) == 0:
        raise ValueError("values 不能为空")
    return sum(values) / len(values)


# ============ 算子元数据定义 ============

ADD_METADATA = OperatorMetadata(
    id="add",
    name="add",
    display_name="加法",
    category="数学运算",
    description="两数相加",
    brief_description="计算两个数的和",
    execution_modes=["workflow"],
    effects=["read"],
    parameters=[
        ParameterMetadata(
            name="a",
            type="float",
            required=True,
            description="加数1",
            default=0.0
        ),
        ParameterMetadata(
            name="b",
            type="float",
            required=True,
            description="加数2",
            default=0.0
        )
    ],
    output_ports=[
        OutputPortMetadata(
            name="default",
            type="float",
            description="和",
            is_default=True
        )
    ],
    use_cases=[
        "基础数值计算",
        "工作流中的中间计算",
        "测试工作流引擎"
    ],
    notes=[
        "输入参数支持整数和浮点数",
        "可通过 $ref 引用前置任务结果"
    ]
)

SUBTRACT_METADATA = OperatorMetadata(
    id="subtract",
    name="subtract",
    display_name="减法",
    category="数学运算",
    description="两数相减",
    brief_description="计算两个数的差",
    execution_modes=["workflow"],
    effects=["read"],
    parameters=[
        ParameterMetadata(
            name="a",
            type="float",
            required=True,
            description="被减数",
            default=0.0
        ),
        ParameterMetadata(
            name="b",
            type="float",
            required=True,
            description="减数",
            default=0.0
        )
    ],
    output_ports=[
        OutputPortMetadata(
            name="default",
            type="float",
            description="差",
            is_default=True
        )
    ]
)

MULTIPLY_METADATA = OperatorMetadata(
    id="multiply",
    name="multiply",
    display_name="乘法",
    category="数学运算",
    description="两数相乘",
    brief_description="计算两个数的积",
    execution_modes=["workflow"],
    effects=["read"],
    parameters=[
        ParameterMetadata(
            name="a",
            type="float",
            required=True,
            description="乘数1",
            default=1.0
        ),
        ParameterMetadata(
            name="b",
            type="float",
            required=True,
            description="乘数2",
            default=1.0
        )
    ],
    output_ports=[
        OutputPortMetadata(
            name="default",
            type="float",
            description="积",
            is_default=True
        )
    ]
)

DIVIDE_METADATA = OperatorMetadata(
    id="divide",
    name="divide",
    display_name="除法",
    category="数学运算",
    description="两数相除",
    brief_description="计算两个数的商",
    execution_modes=["workflow"],
    effects=["read"],
    parameters=[
        ParameterMetadata(
            name="a",
            type="float",
            required=True,
            description="被除数",
            default=1.0
        ),
        ParameterMetadata(
            name="b",
            type="float",
            required=True,
            description="除数",
            default=1.0,
            min=0.0001  # 防止除零
        )
    ],
    output_ports=[
        OutputPortMetadata(
            name="default",
            type="float",
            description="商",
            is_default=True
        )
    ],
    notes=[
        "除数不能为零",
        "建议除数设置最小值 0.0001"
    ]
)

AVERAGE_METADATA = OperatorMetadata(
    id="average",
    name="average",
    display_name="平均值",
    category="统计分析",
    description="计算数组的平均值",
    brief_description="计算一组数的平均值",
    execution_modes=["workflow"],
    effects=["read"],
    parameters=[
        ParameterMetadata(
            name="values",
            type="array",
            item_type="float",
            required=True,
            description="数值数组"
        )
    ],
    output_ports=[
        OutputPortMetadata(
            name="default",
            type="float",
            description="平均值",
            is_default=True
        )
    ],
    use_cases=[
        "计算平均分数",
        "统计平均温度",
        "计算平均价格"
    ]
)


# ============ 算子注册表 ============

OPERATORS = {
    "add": {
        "metadata": ADD_METADATA,
        "function": add
    },
    "subtract": {
        "metadata": SUBTRACT_METADATA,
        "function": subtract
    },
    "multiply": {
        "metadata": MULTIPLY_METADATA,
        "function": multiply
    },
    "divide": {
        "metadata": DIVIDE_METADATA,
        "function": divide
    },
    "average": {
        "metadata": AVERAGE_METADATA,
        "function": average
    }
}


def get_operator_function(name: str):
    """获取算子函数"""
    if name not in OPERATORS:
        raise ValueError(f"算子 '{name}' 不存在")
    return OPERATORS[name]["function"]


def get_operator_metadata(name: str) -> OperatorMetadata:
    """获取算子元数据"""
    if name not in OPERATORS:
        raise ValueError(f"算子 '{name}' 不存在")
    return OPERATORS[name]["metadata"]


def list_operators() -> List[dict]:
    """列出所有算子元数据"""
    operators = []
    for op in OPERATORS.values():
        metadata = op["metadata"].model_dump()
        if not metadata.get("category_path"):
            metadata["category_path"] = [metadata["category"]]
        operators.append(metadata)
    return operators
