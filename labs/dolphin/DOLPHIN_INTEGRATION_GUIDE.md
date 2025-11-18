# DolphinScheduler 集成完整指南

## 📖 概述

本指南详细说明如何将空间算子工作流引擎集成到 Apache DolphinScheduler 中。

---

## 🎯 集成方式对比

| 方式 | 复杂度 | 灵活性 | 性能 | 推荐场景 |
|------|--------|--------|------|---------|
| **方式 1: Python 任务** | ⭐ 简单 | ⭐⭐ 中 | ⭐⭐⭐ 高 | 快速原型、单个工作流 |
| **方式 2: JSON 配置** | ⭐⭐ 中等 | ⭐⭐⭐ 高 | ⭐⭐⭐ 高 | 动态工作流、参数化 |
| **方式 3: 混合架构** | ⭐⭐⭐ 复杂 | ⭐⭐⭐ 高 | ⭐⭐⭐ 最高 | 生产环境、复杂场景 |

---

## 方式 1: Python 任务直接调用（推荐入门）

### 步骤 1: 上传代码到 DolphinScheduler

#### 1.1 打包代码

```bash
cd /Users/pampa/code/addp/labs/dolphin/backend

# 创建部署包
tar -czf spatial_workflow.tar.gz spatial/ requirements.txt

# 或者使用 zip
zip -r spatial_workflow.zip spatial/ requirements.txt
```

#### 1.2 上传到资源中心

1. 登录 DolphinScheduler Web UI
2. 进入 **资源中心 → 文件管理**
3. 创建目录 `spatial/`
4. 上传文件:
   - `spatial_workflow.tar.gz`
   - 或直接上传 `spatial/` 目录下的所有 `.py` 文件

#### 1.3 在 Worker 节点上部署

SSH 到 DolphinScheduler Worker 节点：

```bash
# 下载并解压
cd /opt/dolphinscheduler/
wget http://dolphin-resource-server/spatial_workflow.tar.gz
tar -xzf spatial_workflow.tar.gz

# 安装依赖
pip3 install -r requirements.txt

# 验证安装
python3 -c "from spatial.workflow_engine import SpatialWorkflowEngine; print('OK')"
```

### 步骤 2: 创建 Python 任务

#### 2.1 在 DolphinScheduler 中创建项目

1. 进入 **项目管理 → 创建项目**
2. 项目名称: `spatial_analysis`
3. 项目描述: `空间分析工作流`

#### 2.2 创建工作流定义

1. 进入 **工作流定义 → 创建工作流**
2. 工作流名称: `buffer_analysis`

#### 2.3 添加 Python 任务

拖拽 **Python** 任务节点到画布，配置如下：

**任务名称**: `spatial_workflow_task`

**Python 脚本**:
```python
#!/usr/bin/env python3
import sys
import json

# 添加代码路径
sys.path.insert(0, '/opt/dolphinscheduler/spatial')

from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef

# 创建工作流
engine = SpatialWorkflowEngine(verbose=True)

# 添加任务
engine.add_task(
    "buffer1",
    "buffer",
    description="天安门缓冲区",
    input_geom={"type": "Point", "coordinates": [116.404, 39.915]},
    distance=0.001,  # ~100m
    segments=16
)

engine.add_task(
    "buffer2",
    "buffer",
    description="故宫缓冲区",
    input_geom={"type": "Point", "coordinates": [116.397, 39.916]},
    distance=0.0005,  # ~50m
    segments=16
)

engine.add_task(
    "intersection",
    "intersection",
    description="计算交集",
    geom_a=TaskRef("buffer1"),
    geom_b=TaskRef("buffer2")
)

# 执行工作流
results = engine.run()

# 输出结果
output = {
    "status": "success",
    "result": results["intersection"],
    "stats": engine.get_execution_stats()
}

print(json.dumps(output, indent=2, ensure_ascii=False))

# 设置输出变量（供下游任务使用）
print(f"##[set-output name=result_geom]{json.dumps(results['intersection'])}")
```

**其他配置**:
- **Python 版本**: Python 3
- **环境变量**: 无需配置
- **资源**: 默认即可

#### 2.4 运行工作流

1. 点击 **保存**
2. 点击 **上线**
3. 点击 **运行**
4. 查看日志输出

---

## 方式 2: JSON 配置工作流（推荐生产）

### 优势
- ✅ 工作流定义与代码分离
- ✅ 可以动态修改工作流（无需修改代码）
- ✅ 参数化，支持批量运行

### 步骤 1: 创建工作流配置文件

创建 `workflow_config.json`:

```json
{
  "name": "beijing_poi_analysis",
  "description": "北京 POI 密度分析",
  "tasks": [
    {
      "id": "create_center",
      "operator": "create_point",
      "description": "创建分析中心点",
      "params": {
        "lon": 116.404,
        "lat": 39.915
      }
    },
    {
      "id": "buffer_1km",
      "operator": "buffer",
      "description": "1km 服务范围",
      "params": {
        "input_geom": {"$ref": "create_center"},
        "distance": 0.009,
        "segments": 32
      }
    },
    {
      "id": "calculate_area",
      "operator": "get_area",
      "description": "计算服务范围面积",
      "params": {
        "input_geom": {"$ref": "buffer_1km"}
      }
    },
    {
      "id": "export_result",
      "operator": "export_to_wkt",
      "description": "导出为 WKT 格式",
      "params": {
        "input_geom": {"$ref": "buffer_1km"}
      }
    }
  ]
}
```

### 步骤 2: 上传配置文件

1. 将 `workflow_config.json` 上传到 DolphinScheduler 资源中心
2. 或者使用参数传递

### 步骤 3: 创建执行任务

**Python 脚本**:

```python
#!/usr/bin/env python3
import sys
import json

sys.path.insert(0, '/opt/dolphinscheduler/spatial')

from spatial.dolphin_integration import execute_spatial_workflow

# 方式 A: 从资源文件读取
with open('/opt/dolphinscheduler/resources/workflow_config.json') as f:
    workflow_def = json.load(f)

# 方式 B: 从上游任务获取（参数传递）
# workflow_def = json.loads(${workflow_config})

# 执行工作流
result = execute_spatial_workflow(workflow_def)

# 输出结果
print(json.dumps(result, indent=2, ensure_ascii=False))

# 检查执行状态
if result["status"] == "success":
    print(f"\n✅ 工作流执行成功！")
    print(f"   总耗时: {result['stats']['total_duration_ms']:.2f}ms")

    # 设置输出变量
    export_result = result['results'].get('export_result')
    if export_result:
        print(f"##[set-output name=wkt_result]{export_result}")
else:
    raise Exception(f"工作流执行失败: {result['error']}")
```

---

## 方式 3: 混合架构（推荐大型项目）

### 架构设计

```
DolphinScheduler 工作流:
│
├─ [数据加载任务] (并行，方案 A)
│   ├─ 从 PostGIS 加载北京 POI
│   ├─ 从 PostGIS 加载上海 POI  (并行)
│   └─ 从 PostGIS 加载深圳 POI
│
├─ [空间分析工作流] (方案 B - 内存引擎)
│   └─ Python 任务：执行 SpatialWorkflowEngine
│       Buffer → Intersection → Union → Area
│
└─ [结果导出任务] (并行，方案 A)
    ├─ 导出到 MinIO
    ├─ 更新 PostgreSQL  (并行)
    └─ 发送通知邮件
```

### 实现示例

#### Task 1: 数据加载（并行）

创建 3 个并行的 SQL 任务：

**任务 1: 加载北京数据**
```sql
SELECT ST_AsGeoJSON(geom) as geom_json, name, category
FROM pois
WHERE city = '北京'
LIMIT 1000;
```

输出变量: `beijing_pois`

#### Task 2: 空间分析工作流

依赖: `Task 1` 完成后执行

**Python 脚本**:
```python
import sys
import json
sys.path.insert(0, '/opt/dolphinscheduler/spatial')

from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef

# 从上游任务获取数据
beijing_pois = json.loads(${beijing_pois})
shanghai_pois = json.loads(${shanghai_pois})

# 创建工作流
engine = SpatialWorkflowEngine()

# 批量创建缓冲区
poi_geoms = [poi['geom_json'] for poi in beijing_pois]
engine.add_task("batch_buffers", "batch_buffer",
               geometries=poi_geoms, distance=0.0005)

# 合并所有缓冲区
engine.add_task("union_all", "union",
               geometries=TaskRef("batch_buffers"))

# 计算总面积
engine.add_task("total_area", "get_area",
               input_geom=TaskRef("union_all"))

# 执行
results = engine.run()

# 输出结果
area_sq_m = results["total_area"] * 111000 * 111000
print(f"总覆盖面积: {area_sq_m:.2f} 平方米")
print(f"##[set-output name=coverage_area]{area_sq_m}")
```

#### Task 3: 结果导出（并行）

依赖: `Task 2` 完成

创建 3 个并行任务:
- 导出到 MinIO (Python 任务)
- 更新数据库 (SQL 任务)
- 发送邮件 (Shell 任务)

---

## 🔧 高级配置

### 1. 参数传递

#### 使用 DolphinScheduler 全局参数

在工作流定义中添加全局参数：

```json
{
  "globalParams": [
    {
      "prop": "center_lon",
      "value": "116.404",
      "type": "VARCHAR"
    },
    {
      "prop": "center_lat",
      "value": "39.915",
      "type": "VARCHAR"
    },
    {
      "prop": "buffer_distance",
      "value": "0.001",
      "type": "VARCHAR"
    }
  ]
}
```

在 Python 任务中使用：

```python
center_lon = float(${center_lon})
center_lat = float(${center_lat})
buffer_distance = float(${buffer_distance})

engine.add_task("buffer", "buffer",
               input_geom={"type": "Point", "coordinates": [center_lon, center_lat]},
               distance=buffer_distance)
```

### 2. 任务依赖配置

在 DolphinScheduler DAG 中连接任务：

```
Task A (加载数据) ──┐
                    ├──→ Task C (空间分析)
Task B (加载数据) ──┘
                    ↓
                Task D (导出结果)
```

### 3. 错误处理

在 Python 任务中添加错误处理：

```python
try:
    result = execute_spatial_workflow(workflow_def)

    if result["status"] == "success":
        print("✅ 执行成功")
    else:
        print(f"❌ 执行失败: {result['error']}")
        sys.exit(1)  # 标记任务失败

except Exception as e:
    print(f"❌ 异常: {str(e)}")
    import traceback
    traceback.print_exc()
    sys.exit(1)
```

### 4. 日志配置

启用详细日志：

```python
# 启用工作流引擎日志
engine = SpatialWorkflowEngine(verbose=True)

# 执行完成后输出统计
stats = engine.get_execution_stats()
print(f"\n📊 执行统计:")
print(f"   总任务数: {stats['total_tasks']}")
print(f"   成功数: {stats['success_count']}")
print(f"   失败数: {stats['failed_count']}")
print(f"   总耗时: {stats['total_duration_ms']:.2f}ms")
```

---

## 📊 实际案例

### 案例 1: 每日 POI 密度分析

**需求**: 每天凌晨 1 点分析全国主要城市的 POI 密度

**DolphinScheduler 配置**:

1. **定时调度**: Cron 表达式 `0 0 1 * * ?`

2. **工作流 DAG**:
```
[加载 10 个城市 POI 数据] (并行)
        ↓
[批量执行空间分析] (10 个 Python 任务，每个城市一个)
        ↓
[汇总结果并导出报告]
        ↓
[发送邮件通知]
```

3. **Python 任务示例** (以北京为例):

```python
#!/usr/bin/env python3
import sys
sys.path.insert(0, '/opt/dolphinscheduler/spatial')

from spatial.dolphin_integration import execute_spatial_workflow

# 从上游任务获取 POI 数据
city_name = ${city_name}  # "北京"
pois_data = ${pois_data}  # JSON 数据

# 定义工作流
workflow_def = {
    "name": f"{city_name}_poi_density",
    "tasks": [
        {
            "id": "batch_buffer",
            "operator": "batch_buffer",
            "params": {
                "geometries": json.loads(pois_data),
                "distance": 0.0005  # 50m
            }
        },
        {
            "id": "union_all",
            "operator": "union",
            "params": {
                "geometries": {"$ref": "batch_buffer"}
            }
        },
        {
            "id": "calculate_area",
            "operator": "get_area",
            "params": {
                "input_geom": {"$ref": "union_all"}
            }
        }
    ]
}

result = execute_spatial_workflow(workflow_def)

if result["status"] == "success":
    area_sq_km = result["results"]["calculate_area"] * 111000 * 111000 / 1e6
    print(f"{city_name} POI 覆盖面积: {area_sq_km:.2f} 平方公里")
    print(f"##[set-output name=coverage_area]{area_sq_km}")
```

### 案例 2: 实时地理围栏告警

**需求**: 当用户进入/离开指定区域时触发告警

**DolphinScheduler 配置**:

1. **触发方式**: API 触发（外部系统调用）

2. **工作流**:
```
[接收用户位置] → [判断是否在围栏内] → [发送告警]
```

3. **Python 任务**:

```python
#!/usr/bin/env python3
import sys
sys.path.insert(0, '/opt/dolphinscheduler/spatial')

from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef

# 从 API 参数获取
user_lon = float(${user_lon})
user_lat = float(${user_lat})
fence_geojson = ${fence_geojson}  # 围栏几何

engine = SpatialWorkflowEngine()

# 创建用户位置点
engine.add_task("user_point", "create_point",
               lon=user_lon, lat=user_lat)

# 判断是否在围栏内
engine.add_task("is_inside", "contains",
               geom_a=json.loads(fence_geojson),
               geom_b=TaskRef("user_point"))

results = engine.run()

if results["is_inside"]:
    print("✅ 用户在围栏内")
    print("##[set-output name=alert_type]enter")
else:
    print("❌ 用户在围栏外")
    print("##[set-output name=alert_type]exit")
```

---

## 🚀 性能优化建议

### 1. 使用资源池

为空间分析任务配置独立的 Worker 组：

```yaml
# DolphinScheduler 配置
worker.groups: spatial_analysis
```

在任务配置中指定:
- Worker 分组: `spatial_analysis`
- CPU 配额: 2 核
- 内存配额: 4GB

### 2. 批量处理

对于大量相似任务，使用批量算子：

```python
# ✅ 高效
engine.add_task("batch_buffers", "batch_buffer",
               geometries=all_pois,  # 一次处理 1000 个
               distance=0.0005)

# ❌ 低效
for poi in all_pois:  # 创建 1000 个任务
    engine.add_task(f"buffer_{poi['id']}", "buffer", ...)
```

### 3. 缓存中间结果

对于重复使用的几何对象，缓存到 Redis：

```python
import redis
r = redis.Redis(host='localhost')

# 缓存结果
result_key = f"workflow_result_{workflow_id}"
r.set(result_key, json.dumps(results), ex=3600)

# 下游任务读取
cached_result = json.loads(r.get(result_key))
```

---

## 📚 相关文档

- [WORKFLOW_ENGINE_GUIDE.md](WORKFLOW_ENGINE_GUIDE.md) - 工作流引擎使用指南
- [HYBRID_ARCHITECTURE.md](HYBRID_ARCHITECTURE.md) - 混合架构设计
- [examples/dolphin_python_task.py](backend/examples/dolphin_python_task.py) - Python 任务示例

---

## 🆘 常见问题

### Q1: 如何查看工作流执行日志？

**A**: 在 DolphinScheduler 中:
1. 进入 **工作流实例** 页面
2. 点击任务节点
3. 点击 **查看日志**

### Q2: 如何传递大数据量？

**A**: 使用外部存储:
```python
# 上游任务：保存到 MinIO/S3
import boto3
s3 = boto3.client('s3')
s3.put_object(Bucket='results', Key='data.json', Body=json.dumps(large_data))
print(f"##[set-output name=data_key]data.json")

# 下游任务：从 MinIO/S3 读取
data_key = ${data_key}
obj = s3.get_object(Bucket='results', Key=data_key)
large_data = json.loads(obj['Body'].read())
```

### Q3: 如何处理任务失败？

**A**: 配置重试策略:
- 失败重试次数: 3
- 重试间隔: 1 分钟
- 失败策略: 继续执行其他任务 / 整个工作流失败

---

## ✅ 总结

三种集成方式各有优势：

- **方式 1 (Python 任务)**: 适合快速原型，简单直接
- **方式 2 (JSON 配置)**: 适合生产环境，灵活可维护
- **方式 3 (混合架构)**: 适合复杂场景，性能最优

**推荐路径**:
1. 先用方式 1 验证功能
2. 生产环境升级到方式 2
3. 大型项目采用方式 3

现在你可以立即开始在 DolphinScheduler 中使用空间算子工作流引擎了！🚀
