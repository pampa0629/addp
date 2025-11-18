"""
空间算子注册中心
定义所有可用的空间算子及其参数规范
"""

from dataclasses import dataclass
from typing import Dict, List, Any, Optional
from enum import Enum


class ParamType(Enum):
    """参数类型枚举"""
    FLOAT = "float"
    INT = "int"
    STRING = "string"
    GEOJSON = "geojson"
    WKT = "wkt"
    TABLE_REF = "table_ref"  # 引用数据表
    FIELD_REF = "field_ref"  # 引用字段名


@dataclass
class OperatorParam:
    """算子参数定义"""
    name: str
    type: ParamType
    required: bool = True
    default: Any = None
    description: str = ""

    def validate(self, value: Any) -> bool:
        """验证参数值"""
        if self.required and value is None:
            return False
        # TODO: 添加类型验证逻辑
        return True


@dataclass
class SpatialOperator:
    """空间算子定义"""
    code: str  # 算子唯一标识，如 "buffer", "intersection"
    name: str  # 显示名称
    category: str  # 分类：几何处理、空间关系、空间分析等
    description: str
    input_params: List[OperatorParam]
    output_type: str  # 输出类型：geometry, table, metrics

    def to_dict(self) -> Dict[str, Any]:
        """转换为字典格式（用于前端渲染）"""
        return {
            "code": self.code,
            "name": self.name,
            "category": self.category,
            "description": self.description,
            "params": [
                {
                    "name": p.name,
                    "type": p.type.value,
                    "required": p.required,
                    "default": p.default,
                    "description": p.description
                }
                for p in self.input_params
            ],
            "output_type": self.output_type
        }


# ========================================
# 算子注册表
# ========================================

SPATIAL_OPERATORS = [
    # 几何处理算子
    SpatialOperator(
        code="buffer",
        name="缓冲区分析",
        category="几何处理",
        description="对几何对象创建指定距离的缓冲区",
        input_params=[
            OperatorParam("input_geom", ParamType.GEOJSON, description="输入几何对象"),
            OperatorParam("distance", ParamType.FLOAT, description="缓冲距离（米）"),
            OperatorParam("segments", ParamType.INT, default=8, required=False,
                         description="圆弧段数（精度控制）")
        ],
        output_type="geometry"
    ),

    SpatialOperator(
        code="intersection",
        name="几何相交",
        category="几何处理",
        description="计算两个几何对象的交集",
        input_params=[
            OperatorParam("geom_a", ParamType.GEOJSON, description="几何对象A"),
            OperatorParam("geom_b", ParamType.GEOJSON, description="几何对象B")
        ],
        output_type="geometry"
    ),

    SpatialOperator(
        code="union",
        name="几何合并",
        category="几何处理",
        description="合并多个几何对象为一个",
        input_params=[
            OperatorParam("geometries", ParamType.GEOJSON, description="几何对象数组")
        ],
        output_type="geometry"
    ),

    SpatialOperator(
        code="centroid",
        name="计算质心",
        category="几何处理",
        description="计算几何对象的质心点",
        input_params=[
            OperatorParam("input_geom", ParamType.GEOJSON, description="输入几何对象")
        ],
        output_type="geometry"
    ),

    # 空间关系算子
    SpatialOperator(
        code="contains",
        name="包含关系判断",
        category="空间关系",
        description="判断几何A是否完全包含几何B",
        input_params=[
            OperatorParam("geom_a", ParamType.GEOJSON, description="几何对象A"),
            OperatorParam("geom_b", ParamType.GEOJSON, description="几何对象B")
        ],
        output_type="metrics"
    ),

    SpatialOperator(
        code="intersects",
        name="相交关系判断",
        category="空间关系",
        description="判断两个几何对象是否相交",
        input_params=[
            OperatorParam("geom_a", ParamType.GEOJSON, description="几何对象A"),
            OperatorParam("geom_b", ParamType.GEOJSON, description="几何对象B")
        ],
        output_type="metrics"
    ),

    SpatialOperator(
        code="distance",
        name="距离计算",
        category="空间关系",
        description="计算两个几何对象之间的最短距离",
        input_params=[
            OperatorParam("geom_a", ParamType.GEOJSON, description="几何对象A"),
            OperatorParam("geom_b", ParamType.GEOJSON, description="几何对象B")
        ],
        output_type="metrics"
    ),

    # 数据处理算子
    SpatialOperator(
        code="spatial_join",
        name="空间连接",
        category="数据处理",
        description="基于空间关系连接两个数据表",
        input_params=[
            OperatorParam("left_table", ParamType.TABLE_REF, description="左表"),
            OperatorParam("right_table", ParamType.TABLE_REF, description="右表"),
            OperatorParam("predicate", ParamType.STRING, default="intersects",
                         description="空间谓词：intersects, contains, within")
        ],
        output_type="table"
    ),

    SpatialOperator(
        code="aggregate",
        name="空间聚合",
        category="数据处理",
        description="按空间范围聚合统计",
        input_params=[
            OperatorParam("input_table", ParamType.TABLE_REF, description="输入数据表"),
            OperatorParam("group_by_geom", ParamType.GEOJSON, description="聚合范围几何"),
            OperatorParam("agg_field", ParamType.FIELD_REF, description="聚合字段"),
            OperatorParam("agg_func", ParamType.STRING, default="sum",
                         description="聚合函数：sum, count, avg, min, max")
        ],
        output_type="table"
    ),
]


class OperatorRegistry:
    """算子注册管理器"""

    def __init__(self):
        self._operators: Dict[str, SpatialOperator] = {
            op.code: op for op in SPATIAL_OPERATORS
        }

    def get(self, code: str) -> Optional[SpatialOperator]:
        """获取指定算子"""
        return self._operators.get(code)

    def list_all(self) -> List[SpatialOperator]:
        """列出所有算子"""
        return list(self._operators.values())

    def list_by_category(self, category: str) -> List[SpatialOperator]:
        """按分类列出算子"""
        return [op for op in self._operators.values() if op.category == category]

    def register(self, operator: SpatialOperator):
        """注册新算子（用于插件扩展）"""
        self._operators[operator.code] = operator

    def to_json(self) -> List[Dict[str, Any]]:
        """导出为 JSON（用于前端加载）"""
        return [op.to_dict() for op in self._operators.values()]


# 全局单例
registry = OperatorRegistry()