# Temporal + GeoPandas 空间分析工作流

🚀 **使用 Temporal Python SDK 实现分布式空间数据处理流水线**

---

## 📋 目录

- [项目简介](#项目简介)
- [为什么选择 Temporal](#为什么选择-temporal)
- [架构设计](#架构设计)
- [快速开始](#快速开始)
- [使用示例](#使用示例)
- [核心组件](#核心组件)
- [与 DolphinScheduler 对比](#与-dolphinscheduler-对比)
- [API 参考](#api-参考)

---

## 项目简介

本项目将 **Temporal Workflow 引擎** 与现有的 **GeoPandas 空间算子** 集成,实现:

✅ **代码即 DAG** - Python 函数定义工作流,无需 UI 拖拽
✅ **类型安全** - 完整的 Python 类型提示支持
✅ **自动重试** - 内置容错机制,任务失败自动重试
✅ **分布式执行** - 多 Worker 并行处理,支持水平扩展
✅ **版本控制友好** - Workflow 代码直接存入 Git
✅ **IDE 友好** - 支持断点调试,自动补全

---

## 为什么选择 Temporal

### Temporal 的核心优势

| 特性 | Temporal | DolphinScheduler | 说明 |
|-----|----------|------------------|------|
| **定义方式** | Python 代码 | UI 拖拽 + JSON | Temporal 更适合代码密集型流程 |
| **类型安全** | ✅ 强类型 | ❌ JSON 字符串 | 编译时检查,减少运行时错误 |
| **调试体验** | ✅ IDE 断点调试 | ⚠️ 查看日志 | Temporal 可直接在 PyCharm/VSCode 调试 |
| **版本控制** | ✅ Git 直接管理 | ⚠️ 需导出/导入 | Workflow 即代码,天然支持 Git |
| **重试机制** | ✅ 原生支持 | ⚠️ 需手动配置 | Temporal 提供多种重试策略 |
| **状态管理** | ✅ 自动持久化 | ⚠️ 数据库存储 | Temporal 保证状态不丢失 |
| **并行执行** | ✅ asyncio 原生支持 | ⚠️ 手动定义依赖 | Python 协程天然并行 |
| **学习曲线** | 📈 较陡 (需理解 Workflow 概念) | 📉 UI 直观 | DolphinScheduler 更适合运维人员 |

### 适用场景

**选择 Temporal 如果:**
- 流程逻辑复杂,需要编程实现
- 团队熟悉 Python,希望代码化管理
- 需要强类型和 IDE 支持
- 需要复杂的错误处理和重试逻辑

**选择 DolphinScheduler 如果:**
- 业务人员需要通过 UI 配置流程
- 主要是定时任务和简单 DAG
- 需要可视化的 DAG 编辑器
- 运维友好,易于监控

---

## 架构设计

### 目录结构

```
backend/temporal/
├── activities/                  # Activities (原子操作)
│   ├── spatial_activities.py    # 空间算子 Activities
│   └── io_activities.py         # 文件 IO Activities
├── workflows/                   # Workflows (流程编排)
│   ├── buffer_analysis.py       # 缓冲区分析
│   ├── overlay_analysis.py      # 空间叠加
│   └── complex_pipeline.py      # 复杂流水线
├── examples/                    # 示例脚本
│   ├── run_buffer_workflow.py
│   ├── run_overlay_workflow.py
│   └── run_complex_workflow.py
├── worker.py                    # Temporal Worker (执行器)
├── client.py                    # Temporal Client (调用方)
└── config.py                    # 配置管理
```

### 核心概念

```
┌─────────────────────────────────────────────────────────┐
│                    Temporal Server                       │
│  - 任务调度                                              │
│  - 状态管理                                              │
│  - 事件日志                                              │
└─────────────┬────────────────────────┬──────────────────┘
              │                        │
      ┌───────▼────────┐       ┌───────▼────────┐
      │  Worker 1      │       │  Worker 2      │
      │  执行 Activity │       │  执行 Activity │
      └────────────────┘       └────────────────┘
              │                        │
      ┌───────▼────────────────────────▼───────┐
      │      GeoPandas 空间算子                │
      │  buffer / overlay / reproject ...      │
      └────────────────────────────────────────┘
```

**Activity** = 单个空间操作 (缓冲区、叠加、投影转换等)
**Workflow** = Activities 的编排 (定义执行顺序、重试策略、并行逻辑)
**Worker** = 执行 Activities 的进程 (可多实例部署)
**Client** = 发起 Workflow 的客户端 (API/CLI/定时任务)

---

## 快速开始

### 1️⃣ 安装依赖

```bash
cd backend/temporal

# 创建虚拟环境
python3 -m venv venv
source venv/bin/activate  # Linux/Mac
# 或 venv\Scripts\activate  # Windows

# 安装依赖
pip install temporalio geopandas shapely pyproj
```

### 2️⃣ 启动 Temporal Server

```bash
# 返回项目根目录
cd /Users/pampa/code/addp/labs/dolphin

# 启动 Temporal Server (Docker)
docker-compose -f docker-compose-temporal.yml up -d

# 检查服务状态
docker-compose -f docker-compose-temporal.yml ps

# 访问 Web UI
open http://localhost:8080  # Temporal UI
```

**等待 1-2 分钟** 让 Temporal Server 完全启动。

### 3️⃣ 启动 Worker

```bash
cd backend/temporal

# 启动 Worker (监听任务队列)
python worker.py
```

输出示例:
```
🚀 启动 Temporal Worker
📡 Temporal Server: localhost:7233
📋 Task Queue: spatial-analysis
⚙️  Max Concurrent Activities: 10
✅ 成功连接到 Temporal Server
📝 注册 Workflows: 3 个
🎯 Worker 已启动，等待任务...
```

### 4️⃣ 运行示例 Workflow

**打开新终端**,执行示例脚本:

```bash
cd backend/temporal

# 示例1: 缓冲区分析
python examples/run_buffer_workflow.py \
  --input ../../data/sample.geojson \
  --output ../../output/buffer_result.geojson \
  --distance 100 \
  --min-area 1000

# 示例2: 空间叠加
python examples/run_overlay_workflow.py \
  --input ../../data/layer1.geojson \
  --clip ../../data/boundary.geojson \
  --output ../../output/clipped.geojson \
  --type intersection

# 示例3: 复杂流水线
python examples/run_complex_workflow.py \
  --config examples/pipeline_config.json
```

### 5️⃣ 查看执行结果

访问 Temporal UI 查看 Workflow 执行历史:
- URL: http://localhost:8080
- 可以看到每个 Activity 的执行时间、输入输出、重试次数
- 支持回放 Workflow 用于调试

---

## 使用示例

### 示例 1: 缓冲区分析

**场景**: 对道路数据生成 100 米缓冲区,过滤小于 1000 平方米的图斑。

```python
import asyncio
from client import buffer_analysis

async def main():
    result = await buffer_analysis(
        input_file="data/roads.geojson",
        output_file="output/roads_buffer.geojson",
        buffer_distance=100.0,  # 米
        min_area=1000.0         # 平方米
    )
    print(result)

asyncio.run(main())
```

**工作流步骤**:
1. 验证输入文件 ✅
2. 投影转换 (WGS84 → Web Mercator) 🌐
3. 缓冲区分析 📏
4. 面积过滤 📐
5. 添加质心坐标 📍
6. 转回 WGS84 🌐
7. 保存结果 💾

### 示例 2: 空间叠加

**场景**: 用行政边界裁剪地块数据。

```python
from client import overlay_analysis

result = await overlay_analysis(
    input_file="data/parcels.geojson",
    clip_layer="data/admin_boundary.geojson",
    output_file="output/parcels_clipped.geojson",
    overlay_type="intersection"  # intersection/union/difference
)
```

### 示例 3: 复杂流水线

**场景**: 多步骤空间分析 - 缓冲区 + 叠加 + 过滤 + 简化。

```python
from client import complex_pipeline

pipeline_config = {
    "buffer_distance": 50.0,      # 缓冲区距离
    "clip_layers": [              # 多个裁剪图层 (并行处理)
        "data/boundary1.geojson",
        "data/boundary2.geojson"
    ],
    "min_area": 500.0,            # 面积过滤
    "simplify_tolerance": 5.0,    # 几何简化
    "source_crs": "EPSG:4326",
    "compute_crs": "EPSG:3857"
}

result = await complex_pipeline(
    input_file="data/buildings.geojson",
    output_file="output/result.geojson",
    pipeline_config=pipeline_config
)
```

**特点**:
- ✅ 多个裁剪图层 **并行处理** (asyncio.gather)
- ✅ 每步自动重试 (最多 3 次)
- ✅ 中间结果自动传递
- ✅ 详细的步骤日志

---

## 核心组件

### Activities (activities/)

**空间算子 Activities** (`spatial_activities.py`):

| Activity | 功能 | 参数 |
|----------|------|------|
| `buffer_activity` | 缓冲区分析 | distance, segments |
| `reproject_activity` | 投影转换 | target_crs, source_crs |
| `overlay_activity` | 空间叠加 | clip_layer, how |
| `filter_by_area_activity` | 面积过滤 | min_area, max_area |
| `add_centroid_activity` | 添加质心 | - |
| `simplify_activity` | 几何简化 | tolerance |
| `union_activity` | 几何合并 | input_paths |

**IO Activities** (`io_activities.py`):

| Activity | 功能 |
|----------|------|
| `validate_file_exists` | 验证文件存在性 |
| `read_geospatial_file` | 读取地理数据 |
| `write_geospatial_file` | 写入地理数据 |

### Workflows (workflows/)

**1. BufferAnalysisWorkflow** - 缓冲区分析流水线
**2. OverlayAnalysisWorkflow** - 空间叠加流水线
**3. ComplexSpatialPipeline** - 复杂多步骤流水线

### 自定义 Workflow

创建自定义 Workflow:

```python
from temporalio import workflow
from datetime import timedelta
from activities import buffer_activity, reproject_activity

@workflow.defn(name="my_custom_workflow")
class MyCustomWorkflow:
    @workflow.run
    async def run(self, params: dict) -> dict:
        # Step 1: 投影转换
        step1 = await workflow.execute_activity(
            reproject_activity,
            args=[params['input'], "EPSG:3857"],
            start_to_close_timeout=timedelta(minutes=5),
            retry_policy={"maximum_attempts": 3}
        )

        # Step 2: 缓冲区
        step2 = await workflow.execute_activity(
            buffer_activity,
            args=[step1['output_path'], 100],
            start_to_close_timeout=timedelta(minutes=10)
        )

        return {"success": True, "output": step2['output_path']}
```

---

## 与 DolphinScheduler 对比

### 功能对比

| 功能 | Temporal | DolphinScheduler |
|-----|----------|------------------|
| **DAG 定义** | Python 代码 | UI 拖拽 + JSON |
| **参数传递** | 强类型 `@dataclass` | JSON 字符串 |
| **调试方式** | IDE 断点调试 | 日志查看 |
| **重试策略** | 原生支持 (多种策略) | 需配置 |
| **并行执行** | `asyncio.gather()` | 定义依赖关系 |
| **状态管理** | 事件溯源 (Event Sourcing) | 数据库存储 |
| **运维复杂度** | 需要 PostgreSQL/Cassandra | 轻量 (内置数据库) |
| **UI 可视化** | ✅ 有 (Temporal UI) | ✅ 有 (功能更丰富) |
| **学习曲线** | 📈 陡峭 | 📉 平缓 |

### 性能对比

**Temporal**:
- ✅ 单 Worker 可处理 1000+ 并发 Activities
- ✅ 支持水平扩展 (启动多个 Worker)
- ⚠️ 需要维护 PostgreSQL/Cassandra

**DolphinScheduler**:
- ✅ UI 操作简单,易于监控
- ✅ 内置资源管理和告警
- ⚠️ 大规模并发性能不如 Temporal

### 何时选择哪个?

**Temporal 适合**:
- 开发团队主导,需要代码化管理
- 复杂业务逻辑,需要强类型和单元测试
- 需要细粒度的错误处理和重试
- Python 重度用户

**DolphinScheduler 适合**:
- 运维团队主导,需要 UI 配置
- 定时任务为主,DAG 相对简单
- 需要完善的监控和告警
- 多语言任务 (Shell/SQL/Python/Spark)

---

## API 参考

### TemporalSpatialClient

```python
from client import TemporalSpatialClient

client = TemporalSpatialClient()
await client.connect()

# 缓冲区分析
result = await client.run_buffer_analysis(
    input_file="...",
    output_file="...",
    buffer_distance=100.0,
    min_area=1000.0
)

# 空间叠加
result = await client.run_overlay_analysis(
    input_file="...",
    clip_layer="...",
    output_file="...",
    overlay_type="intersection"
)

# 复杂流水线
result = await client.run_complex_pipeline(
    input_file="...",
    output_file="...",
    pipeline_config={...}
)
```

### 环境变量

```bash
# Temporal Server 配置
TEMPORAL_HOST=localhost:7233
TEMPORAL_NAMESPACE=default
TEMPORAL_TASK_QUEUE=spatial-analysis

# Worker 配置
TEMPORAL_MAX_ACTIVITIES=10
TEMPORAL_MAX_WORKFLOWS=5

# 日志级别
LOG_LEVEL=INFO
```

---

## 故障排除

### Worker 无法连接到 Temporal Server

```bash
# 检查 Docker 容器状态
docker-compose -f docker-compose-temporal.yml ps

# 查看日志
docker-compose -f docker-compose-temporal.yml logs -f temporal

# 重启服务
docker-compose -f docker-compose-temporal.yml restart
```

### Activity 执行失败

- 查看 Worker 日志,定位错误原因
- 检查输入文件是否存在
- 验证坐标系是否正确

### Workflow 卡住不动

- 检查 Worker 是否正在运行
- 查看 Temporal UI 的 Workflow 历史
- 确认 Task Queue 名称是否正确

---

## 下一步

1. **添加更多空间算子**: 空间连接、最近邻分析等
2. **集成 PostGIS**: 直接从数据库读写数据
3. **性能优化**: 大数据量场景的分块处理
4. **监控告警**: 集成 Prometheus + Grafana
5. **Web API**: 通过 FastAPI 暴露 Workflow 接口

---

## 参考资料

- [Temporal 官方文档](https://docs.temporal.io/)
- [Temporal Python SDK](https://github.com/temporalio/sdk-python)
- [GeoPandas 文档](https://geopandas.org/)
- [DolphinScheduler vs Temporal 对比](https://docs.temporal.io/kb/temporal-vs-airflow)

---

📝 **Author**: ADDP Team
📅 **Last Updated**: 2025-11-19
