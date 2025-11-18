# 空间算子工作流引擎使用指南

## 📖 简介

**SpatialWorkflowEngine** 是一个轻量级的空间分析工作流引擎，专为高性能内存计算设计。它支持将多个空间算子组合成 DAG 工作流，并自动处理依赖关系和数据传递。

### 核心优势

- ✅ **极致性能**: 纯内存计算，性能提升 10-5000 倍（相比 Redis 序列化方案）
- ✅ **零配置**: 无需外部依赖，开箱即用
- ✅ **自动依赖**: 自动解析任务依赖关系，拓扑排序执行
- ✅ **调试友好**: 单进程执行，完整堆栈，易于调试
- ✅ **扩展简单**: 轻松添加自定义算子

---

## 🚀 快速开始

### 1. 安装依赖

```bash
cd backend
pip install shapely==2.0.2
```

### 2. 第一个工作流

```python
from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef

# 创建工作流引擎
engine = SpatialWorkflowEngine()

# 添加任务
engine.add_task(
    "buffer1",
    "buffer",
    description="天安门 100m 缓冲区",
    input_geom={"type": "Point", "coordinates": [116.404, 39.915]},
    distance=0.001,  # 约 100m
    segments=16
)

engine.add_task(
    "buffer2",
    "buffer",
    description="故宫 50m 缓冲区",
    input_geom={"type": "Point", "coordinates": [116.397, 39.916]},
    distance=0.0005,  # 约 50m
    segments=16
)

engine.add_task(
    "intersection",
    "intersection",
    description="计算交集",
    geom_a=TaskRef("buffer1"),  # 引用 buffer1 的输出
    geom_b=TaskRef("buffer2")
)

# 执行工作流
results = engine.run()

# 获取结果
final_result = results["intersection"]
print(f"交集几何: {final_result['type']}")
```

**输出**:
```
📦 已注册 7 个空间算子
➕ 添加任务: buffer1 (buffer)
➕ 添加任务: buffer2 (buffer)
➕ 添加任务: intersection (intersection) (依赖: ['buffer1', 'buffer2'])

============================================================
🚀 开始执行工作流（共 3 个任务）
============================================================

[1/3] 🔄 执行任务: buffer1 (buffer)
    ✅ 完成 (0.33ms) - 输出: <Polygon geometry>
...
✅ 工作流执行完成（总耗时: 0.63ms）

交集几何: Polygon
```

---

## 📚 核心概念

### 1. 任务 (Task)

每个任务封装一个空间算子的执行：

```python
engine.add_task(
    task_id="task1",           # 唯一标识
    operator_code="buffer",     # 算子代码
    description="可选描述",      # 任务说明
    **params                    # 算子参数
)
```

### 2. 任务引用 (TaskRef)

用于在工作流中引用上游任务的输出（内存传递）：

```python
from spatial.task_ref import TaskRef

# 引用 task1 的输出
TaskRef("task1")

# 引用 task1 的特定输出键
TaskRef("task1", "matched_count")
```

### 3. 依赖关系

引擎自动从参数中检测依赖：

```python
# 自动检测 task3 依赖 task1 和 task2
engine.add_task("task1", "buffer", ...)
engine.add_task("task2", "buffer", ...)
engine.add_task("task3", "intersection",
               geom_a=TaskRef("task1"),
               geom_b=TaskRef("task2"))
```

### 4. 拓扑排序

引擎自动按依赖顺序执行任务：

```
task1  ──┐
         ├──→ task3
task2  ──┘

执行顺序: task1 → task2 → task3
```

---

## 🧩 可用算子

### 几何处理算子

| 算子代码 | 说明 | 参数 |
|---------|------|------|
| `buffer` | 缓冲区分析 | `input_geom`, `distance`, `segments` |
| `intersection` | 几何相交 | `geom_a`, `geom_b` |
| `union` | 几何合并 | `geometries` (列表) |
| `centroid` | 计算质心 | `input_geom` |
| `convex_hull` | 凸包 | `input_geom` |
| `envelope` | 最小外接矩形 | `input_geom` |
| `simplify` | 简化几何 | `input_geom`, `tolerance` |
| `difference` | 几何差集 | `geom_a`, `geom_b` |
| `symmetric_difference` | 对称差集 | `geom_a`, `geom_b` |

### 空间关系算子

| 算子代码 | 说明 | 参数 | 返回值 |
|---------|------|------|--------|
| `contains` | 包含关系判断 | `geom_a`, `geom_b` | `bool` |
| `intersects` | 相交关系判断 | `geom_a`, `geom_b` | `bool` |
| `distance` | 距离计算 | `geom_a`, `geom_b` | `float` |

### 几何属性算子

| 算子代码 | 说明 | 参数 | 返回值 |
|---------|------|------|--------|
| `get_area` | 计算面积 | `input_geom` | `float` |
| `get_length` | 计算长度 | `input_geom` | `float` |
| `get_bounds` | 获取边界框 | `input_geom` | `dict` |

### 数据源算子

| 算子代码 | 说明 | 参数 |
|---------|------|------|
| `create_point` | 创建点 | `lon`, `lat` |
| `create_polygon` | 创建多边形 | `coordinates` |
| `create_linestring` | 创建线串 | `coordinates` |
| `load_from_wkt` | 从 WKT 加载 | `wkt_text` |
| `load_from_geojson_string` | 从 GeoJSON 字符串加载 | `geojson_str` |

### 输出算子

| 算子代码 | 说明 | 参数 | 返回值 |
|---------|------|------|--------|
| `export_to_wkt` | 导出为 WKT | `input_geom` | `str` |
| `export_to_geojson_string` | 导出为 GeoJSON | `input_geom`, `pretty` | `str` |

---

## 💡 高级用法

### 1. 并行任务

引擎自动识别可并行执行的任务：

```python
# 5 个 buffer 任务可并行执行（无依赖关系）
for i in range(5):
    engine.add_task(
        f"buffer_{i}",
        "buffer",
        input_geom={"type": "Point", "coordinates": [116.40 + i * 0.01, 39.91]},
        distance=0.001
    )

# Union 等待所有 buffer 完成
engine.add_task(
    "union_all",
    "union",
    geometries=[TaskRef(f"buffer_{i}") for i in range(5)]
)
```

**执行顺序**:
```
buffer_0  ┐
buffer_1  │
buffer_2  ├──→ union_all
buffer_3  │
buffer_4  ┘
```

### 2. 复杂参数引用

支持嵌套字典和列表中的引用：

```python
engine.add_task(
    "complex_task",
    "some_operator",
    geometries=[
        TaskRef("task1"),
        TaskRef("task2"),
        {"type": "Point", "coordinates": [0, 0]}  # 混合常量
    ],
    options={
        "buffer_geom": TaskRef("task3"),
        "distance": 100
    }
)
```

### 3. 自定义算子

注册自己的算子函数：

```python
def my_custom_operator(input_geom: dict, scale: float) -> dict:
    """自定义算子：缩放几何对象"""
    from shapely.geometry import shape, mapping
    from shapely.affinity import scale as shapely_scale

    geom = shape(input_geom)
    scaled = shapely_scale(geom, xfact=scale, yfact=scale)
    return mapping(scaled)

# 注册算子
engine.register_operator("scale", my_custom_operator)

# 使用算子
engine.add_task("scaled", "scale", input_geom={...}, scale=2.0)
```

### 4. 数据血缘图

导出工作流的数据血缘关系（Mermaid 格式）：

```python
lineage = engine.export_lineage()
print(lineage)
```

**输出**:
```
graph LR
    buffer1[buffer]
    buffer2[buffer]
    intersection[intersection]
    buffer1 --> intersection
    buffer2 --> intersection
```

复制到 Mermaid 编辑器即可可视化：https://mermaid.live

### 5. 执行统计

获取详细的执行统计信息：

```python
stats = engine.get_execution_stats()

print(f"总任务数: {stats['total_tasks']}")
print(f"成功数: {stats['success_count']}")
print(f"失败数: {stats['failed_count']}")
print(f"总耗时: {stats['total_duration_ms']:.2f}ms")
print(f"平均耗时: {stats['avg_duration_ms']:.2f}ms")

# 查看每个任务的详细结果
for result in stats['results']:
    print(f"{result.task_id}: {result.duration_ms:.2f}ms")
```

---

## 🔗 与 DolphinScheduler 集成

### 方式 1: 在 DolphinScheduler Python 任务中使用

```python
# DolphinScheduler Python 任务脚本
import sys
sys.path.insert(0, '/path/to/backend')

from spatial.dolphin_integration import execute_spatial_workflow

# 定义工作流
workflow_definition = {
    "name": "my_workflow",
    "tasks": [
        {
            "id": "task1",
            "operator": "buffer",
            "params": {
                "input_geom": {"type": "Point", "coordinates": [116.404, 39.915]},
                "distance": 0.001
            }
        },
        {
            "id": "task2",
            "operator": "centroid",
            "params": {
                "input_geom": {"$ref": "task1"}  # 引用 task1 输出
            }
        }
    ]
}

# 执行
result = execute_spatial_workflow(workflow_definition)

# 输出结果
print(json.dumps(result, indent=2))

# 设置 DolphinScheduler 输出变量
if result["status"] == "success":
    final_result = result["results"]["task2"]
    print(f"##[set-output name=result]{json.dumps(final_result)}")
```

### 方式 2: 混合架构（推荐）

将工作流引擎作为 DolphinScheduler 的一个任务使用：

```
DolphinScheduler DAG:
├─ [数据加载任务] (并行从多个数据源加载)
│
├─ [空间分析工作流任务] ← 内部使用 SpatialWorkflowEngine
│   └─ Buffer → Intersection → Union → Centroid (内存计算)
│
└─ [结果导出任务] (并行导出到多个目标)
```

**优势**:
- 数据加载/导出利用 DolphinScheduler 的分布式能力
- 中间计算利用工作流引擎的内存性能
- 两全其美

---

## 📊 性能对比

根据实际测试结果：

| 场景 | 任务数 | 方案 A（打散） | 方案 B（引擎） | 性能提升 |
|------|--------|---------------|---------------|---------|
| 小型工作流 | 3 | 450ms | 46ms | **9.7x** |
| 中型工作流 | 5 | 755ms | 0.7ms | **1057x** |
| 复杂工作流 | 10 | 5600ms | 1.2ms | **4490x** |

**结论**: 对于小中型工作流（<100MB 数据），工作流引擎性能提升显著。

---

## 🛠️ 调试技巧

### 1. 启用详细日志

```python
engine = SpatialWorkflowEngine(verbose=True)  # 默认已启用
```

### 2. 检查任务执行顺序

```python
# 执行前打印任务顺序
sorted_tasks = engine._topological_sort()
print([t.id for t in sorted_tasks])
```

### 3. 单步调试

在 Python 调试器中设置断点：

```python
# 在 workflow_engine.py 的 _execute_task 方法中设置断点
import pdb; pdb.set_trace()
```

### 4. 查看中间结果

```python
# 执行后查看所有中间结果
for task_id, result in engine.context.items():
    print(f"{task_id}: {result}")
```

### 5. 错误追踪

引擎会自动捕获异常并提供详细错误信息：

```
[3/5] 🔄 执行任务: intersection (intersection)
    ❌ 失败 (0.18ms)
    错误: 任务 'buffer1' 的输出未找到。
          可能原因：1) 任务未执行 2) 任务 ID 错误 3) 依赖顺序错误

RuntimeError: 任务 'intersection' 执行失败: ...
```

---

## 🚨 常见问题

### Q1: 如何处理大数据量？

**A**: 工作流引擎适用于小中型数据（<100MB）。对于大数据量：
- 使用数据库端计算（PostGIS）
- 或者使用方案 A（打散到 DolphinScheduler）

### Q2: 任务之间如何共享数据？

**A**: 使用 `TaskRef` 引用上游任务输出，数据通过内存传递（无序列化开销）。

### Q3: 如何处理循环依赖？

**A**: 引擎会自动检测循环依赖并抛出 `ValueError`：
```
ValueError: 检测到循环依赖: task3
```

### Q4: 支持条件分支吗？

**A**: 暂不支持。所有任务都会执行。如需条件分支，可以：
- 在 DolphinScheduler 层面处理
- 或在自定义算子中实现逻辑判断

### Q5: 如何重用工作流？

**A**: 将工作流定义保存为 JSON，需要时加载执行：

```python
import json

# 保存
workflow_def = {...}
with open('workflow.json', 'w') as f:
    json.dump(workflow_def, f)

# 加载
with open('workflow.json') as f:
    workflow_def = json.load(f)

result = execute_spatial_workflow(workflow_def)
```

---

## 📖 完整示例

### 示例 1: 北京缓冲区分析

```python
from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef

engine = SpatialWorkflowEngine()

# 天安门 1000m 缓冲区
engine.add_task(
    "buffer_tiananmen",
    "buffer",
    input_geom={"type": "Point", "coordinates": [116.39754, 39.90750]},
    distance=0.009,  # ~1000m
    segments=32
)

# 故宫 800m 缓冲区
engine.add_task(
    "buffer_gugong",
    "buffer",
    input_geom={"type": "Point", "coordinates": [116.39723, 39.91649]},
    distance=0.007,  # ~800m
    segments=32
)

# 计算交集
engine.add_task(
    "intersection",
    "intersection",
    geom_a=TaskRef("buffer_tiananmen"),
    geom_b=TaskRef("buffer_gugong")
)

# 计算交集面积
engine.add_task(
    "area",
    "get_area",
    input_geom=TaskRef("intersection")
)

# 执行
results = engine.run()

# 查看结果
area_sq_degrees = results["area"]
area_sq_meters = area_sq_degrees * 111000 * 111000
print(f"交集面积: {area_sq_meters:.2f} 平方米")
```

### 示例 2: POI 密度分析

```python
engine = SpatialWorkflowEngine()

# 创建商圈边界
engine.add_task(
    "mall_boundary",
    "create_point",
    lon=116.404,
    lat=39.915
)

# 1km 缓冲区
engine.add_task(
    "service_area",
    "buffer",
    input_geom=TaskRef("mall_boundary"),
    distance=0.009,
    segments=32
)

# 批量创建 POI 缓冲区
poi_points = [
    (116.405, 39.916),
    (116.406, 39.917),
    (116.403, 39.914)
]

poi_tasks = []
for i, (lon, lat) in enumerate(poi_points):
    task_id = f"poi_{i}"
    engine.add_task(task_id, "create_point", lon=lon, lat=lat)
    poi_tasks.append(task_id)

# 检查哪些 POI 在服务范围内
for i, poi_task in enumerate(poi_tasks):
    engine.add_task(
        f"check_{i}",
        "contains",
        geom_a=TaskRef("service_area"),
        geom_b=TaskRef(poi_task)
    )

results = engine.run()

# 统计
count = sum(results[f"check_{i}"] for i in range(len(poi_points)))
print(f"服务范围内 POI 数量: {count}")
```

---

## 🎓 最佳实践

### 1. 任务命名

使用描述性的任务 ID：

```python
# ✅ 好
engine.add_task("buffer_poi_1km", "buffer", ...)
engine.add_task("intersection_area", "intersection", ...)

# ❌ 差
engine.add_task("t1", "buffer", ...)
engine.add_task("t2", "intersection", ...)
```

### 2. 添加任务描述

便于理解工作流逻辑：

```python
engine.add_task(
    "buffer_mall",
    "buffer",
    description="计算商场 1km 服务范围",  # 添加描述
    ...
)
```

### 3. 合理分解任务

每个任务应该是原子性的，职责单一：

```python
# ✅ 好 - 职责清晰
engine.add_task("load_data", "load_from_wkt", ...)
engine.add_task("create_buffer", "buffer", ...)
engine.add_task("calculate_area", "get_area", ...)

# ❌ 差 - 一个自定义算子做太多事
def complex_analysis(input_geom):
    # 加载数据 + 缓冲 + 计算面积 + 导出...
    ...
```

### 4. 利用批量算子

对于重复操作，使用批量算子提升性能：

```python
# ✅ 更高效
engine.add_task(
    "batch_buffers",
    "batch_buffer",
    geometries=[...],  # 一次传入所有几何对象
    distance=100
)

# ❌ 低效
for i, geom in enumerate(geometries):
    engine.add_task(f"buffer_{i}", "buffer", input_geom=geom, distance=100)
```

### 5. 结合 DolphinScheduler

复杂场景下结合两者优势：

```python
# DolphinScheduler 负责:
# - 并行加载 10 个城市数据（分布式）
# - 定时调度（每天凌晨 1 点）

# 工作流引擎负责:
# - 每个城市的空间分析（内存计算）
# - 复杂的几何运算链（高性能）
```

---

## 📚 扩展阅读

- [HYBRID_ARCHITECTURE.md](HYBRID_ARCHITECTURE.md) - 混合架构设计详解
- [DOLPHIN_INTEGRATION.md](DOLPHIN_INTEGRATION.md) - DolphinScheduler 集成方案
- [USE_CASES.md](USE_CASES.md) - 实际应用场景
- [Performance Report](examples/performance_report.json) - 性能测试报告

---

## 🤝 贡献

欢迎提交自定义算子或改进建议！

**添加新算子的步骤**:
1. 在 `spatial/operators_extended.py` 中实现函数
2. 在 `dolphin_integration.py` 中注册算子
3. 编写测试用例
4. 更新本文档

---

## 📝 许可证

本项目遵循 ADDP 平台的开源协议。
