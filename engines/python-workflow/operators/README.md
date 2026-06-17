# GeoPandas 算子模块

GeoPandas 引擎的空间算子模块，采用模块化架构设计，提供 24 个空间分析算子。

## 目录结构

```
operators/
├── base.py                     # Pydantic 基础模型和工具函数
├── __init__.py                 # 统一导出接口
├── io_operators.py             # 数据 I/O (2个)
├── geometric_operators.py      # 几何处理 (8个)
├── spatial_relations.py        # 空间关系 (3个)
├── properties_operators.py     # 几何属性 (3个)
├── format_operators.py         # 格式转换 (2个)
└── data_operations.py          # 数据操作 (6个)
```

## 算子列表

### 数据 I/O (2个)
- `load` - 数据加载（支持 table/file/geojson）
- `save` - 数据保存（支持 table/file）

### 几何处理 (8个)
- `buffer` - 缓冲区分析
- `intersection` - 几何相交
- `union` - 几何合并
- `centroid` - 计算质心
- `difference` - 几何差集
- `simplify` - 简化几何
- `convex_hull` - 凸包
- `envelope` - 最小外接矩形

### 空间关系 (3个)
- `contains` - 包含关系判断
- `intersects` - 相交关系判断
- `distance_to` - 距离计算

### 几何属性 (3个)
- `get_area` - 计算面积
- `get_length` - 计算长度/周长
- `get_bounds` - 获取边界框

### 格式转换 (2个)
- `load_from_wkt` - WKT → GeoDataFrame
- `export_to_wkt` - GeoDataFrame → WKT

### 数据操作 (6个)
- `clip` - 裁剪几何
- `voronoi` - 泰森多边形
- `split_by_area` - 按面积分割（多输出端口）
- `dissolve` - 融合几何
- `batch_buffer` - 批量缓冲
- `batch_centroid` - 批量质心

## 使用方式

### 基本使用

```python
from operators import OPERATORS, list_operators, get_operator

# 获取所有算子元数据
operators_list = list_operators()

# 获取特定算子函数
buffer_func = get_operator('buffer')

# 执行算子
result = buffer_func(gdf, distance=100, resolution=16)
```

### 在工作流中使用

```python
from workflow_engine import execute_workflow

workflow = {
    "nodes": [
        {
            "id": "load_data",
            "operator": "load",
            "params": {
                "source_type": "table",
                "engine_id": 1,
                "table": "cities"
            }
        },
        {
            "id": "buffer_analysis",
            "operator": "buffer",
            "params": {
                "input_gdf": "$load_data.output",
                "distance": 500
            },
            "depends_on": ["load_data"]
        }
    ]
}

result = execute_workflow(workflow)
```

## 新增算子开发指南

### 1. 选择合适的模块文件

根据算子功能选择对应的文件：
- 数据 I/O → `io_operators.py`
- 几何处理 → `geometric_operators.py`
- 空间关系 → `spatial_relations.py`
- 几何属性 → `properties_operators.py`
- 格式转换 → `format_operators.py`
- 数据操作 → `data_operations.py`

### 2. 实现算子函数

```python
def my_operator(input_gdf: gpd.GeoDataFrame, param1: float) -> gpd.GeoDataFrame:
    """
    算子功能说明

    Args:
        input_gdf: 输入 GeoDataFrame
        param1: 参数说明

    Returns:
        处理后的 GeoDataFrame
    """
    # 实现代码
    result = input_gdf.copy()
    # ... 处理逻辑
    return result
```

### 3. 定义元数据

```python
from .base import OperatorMetadata, OperatorParam, OperatorCategory, register_operator

MY_OPERATOR_METADATA = OperatorMetadata(
    name="my_operator",
    category=OperatorCategory.GEOMETRIC,
    description="简短描述",
    brief_description="一句话说明，常用于XX场景",

    overview="详细的功能概述，50-100字",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="param1",
            type="param",
            data_type="float",
            required=True,
            description="参数说明"
        )
    ],

    use_cases=[
        "场景1: 具体数字和业务背景",
        "场景2: ...",
        "场景3: ...",
        "场景4: ..."
    ],

    notes=[
        "注意事项1",
        "优化建议2",
        "常见问题3",
        "最佳实践4"
    ],

    workflow_example={
        'id': 'example_node',
        'operator': 'my_operator',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'param1': 100
        },
        'depends_on': ['load_data']
    }
)
```

### 4. 注册算子

```python
OPERATORS = dict([
    register_operator(MY_OPERATOR_METADATA, my_operator),
    # ... 其他算子
])
```

### 5. 更新 __init__.py

在 `operators/__init__.py` 中添加导入和导出：

```python
# 导入算子实现
from .your_module import my_operator

# 在 __all__ 中添加
__all__ = [
    # ... 其他导出
    'my_operator',
]
```

## 元数据规范

### OperatorParam 的 type 字段

- `"input"` - 输入数据（通常是 GeoDataFrame）
- `"output"` - 输出数据
- `"param"` - 参数（数值、字符串等）

### data_type 字段

支持的数据类型：
- `"GeoDataFrame"` - 地理数据框
- `"float"` - 浮点数
- `"int"` - 整数
- `"str"` - 字符串
- `"bool"` - 布尔值
- `"list[float]"` - 浮点数列表
- `"list[str]"` - 字符串列表
- `"list[dict]"` - 字典列表

### OperatorCategory 枚举

- `DATA_IO` - "数据I/O"
- `GEOMETRIC` - "几何处理"
- `SPATIAL_RELATION` - "空间关系"
- `PROPERTIES` - "几何属性"
- `FORMAT_CONVERSION` - "格式转换"
- `DATA_OPERATION` - "数据操作"

## 技术特性

### Pydantic 类型安全

使用 Pydantic BaseModel 提供运行时类型验证：

```python
# 自动验证参数类型
metadata = OperatorMetadata(
    name="buffer",
    params=[
        OperatorParam(name="distance", type="param", data_type="float", ...)
    ]
)

# 如果类型错误，会抛出 ValidationError
```

### 向后兼容

通过 `to_legacy_dict()` 方法确保 API 格式兼容：

```python
legacy_dict = metadata.to_legacy_dict()
# 返回与旧版 operators.py 完全一致的字典格式
```

### 多输出端口支持

对于有多个输出的算子（如 `split_by_area`），使用 `output_ports`：

```python
from .base import OutputPort

metadata = OperatorMetadata(
    # ... 其他字段
    output_ports=[
        OutputPort(
            name="larger",
            type="geodataframe",
            description="面积大于阈值的几何",
            is_default=True
        ),
        OutputPort(
            name="smaller",
            type="geodataframe",
            description="面积小于阈值的几何",
            is_default=False
        )
    ]
)
```

## 测试

### 单元测试

```python
# 测试算子功能
def test_buffer():
    import geopandas as gpd
    from operators import get_operator

    # 创建测试数据
    gdf = gpd.GeoDataFrame(...)

    # 执行算子
    buffer_func = get_operator('buffer')
    result = buffer_func(gdf, distance=100)

    # 验证结果
    assert result is not None
    assert len(result) == len(gdf)
```

### 导入测试

```python
from operators import OPERATORS, list_operators

# 验证算子数量
assert len(OPERATORS) == 24

# 验证 API 格式
operators_list = list_operators()
assert len(operators_list) == 24
assert all('name' in op for op in operators_list)
```

## 故障排查

### 导入错误

```bash
# 检查 Python 语法
python -m py_compile operators/*.py

# 测试导入
python -c "from operators import OPERATORS; print(len(OPERATORS))"
```

### API 返回错误

```bash
# 启动服务
python api_server.py

# 测试端点
curl http://localhost:5001/api/operators
```

### 元数据不完整

检查 Pydantic 模型是否包含所有必需字段：
- name, category, description, brief_description
- overview, params, use_cases, notes, workflow_example

## 重构历史

本模块由原始的 `operators.py` (2109行) 重构而来，重构日期：2025-12-23

**重构前**：
- 单文件 2109 行
- 函数实现与元数据分离
- 新增算子需要修改 2 处

**重构后**：
- 7 个模块文件（base + 6 个分类文件）
- 每个文件 200-400 行
- 元数据与实现共存
- 使用 Pydantic 提供类型安全

详见：[python-workflow开发总结](../docs/python-workflow开发总结.md)

## 参考资料

- [Pydantic 文档](https://docs.pydantic.dev/)
- [GeoPandas 文档](https://geopandas.org/)
- [Shapely 文档](https://shapely.readthedocs.io/)
- [ADDP 开发原则](../../../docs/spec/addp开发原则.md)
