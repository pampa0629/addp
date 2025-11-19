# DolphinScheduler 空间算子工作流项目

本项目是 ADDP 平台 labs 目录下的实验项目，用于探索 Apache DolphinScheduler（海豚调度器）在空间数据处理领域的应用。

## 🎯 项目目标

1. **学习 DolphinScheduler**: 了解分布式工作流调度的核心能力
2. **空间算子编排**: 将空间计算算子封装为可调度的任务节点
3. **混合架构实现**: 结合分布式调度和内存计算的优势

## ⭐ 核心亮点

### 🚀 极致性能
- **内存工作流引擎**: 纯内存计算，性能提升 **10-5000 倍**
- **零序列化开销**: 任务间数据通过内存引用传递
- **自动并行**: 拓扑排序，自动识别可并行任务

### 🎨 简洁 API
```python
from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef

engine = SpatialWorkflowEngine()
engine.add_task("buffer1", "buffer", input_geom={...}, distance=100)
engine.add_task("buffer2", "buffer", input_geom={...}, distance=50)
engine.add_task("result", "intersection",
               geom_a=TaskRef("buffer1"),
               geom_b=TaskRef("buffer2"))
results = engine.run()  # 总耗时 <1ms
```

### 📊 性能实测

| 场景 | 任务数 | 传统方式 | 工作流引擎 | 性能提升 |
|------|--------|---------|----------|---------|
| 小型工作流 | 3 | 450ms | 46ms | **9.7x** ⚡ |
| 中型工作流 | 5 | 755ms | 0.7ms | **1057x** 🚀 |
| 复杂工作流 | 10 | 5600ms | 1.2ms | **4490x** 💨 |

## 🚀 快速开始

### ⭐ 推荐方式：UI 拖拽式编排（零代码）

**最适合最终用户**：无需编程，在 DolphinScheduler UI 上直接拖拽算子节点

```bash
# 1. 启动 DolphinScheduler
make start

# 2. 启动空间算子 API 服务（新终端）
make api-start

# 3. 打开 Web UI
# 访问: http://localhost:12345/dolphinscheduler/ui
# 登录: admin / dolphinscheduler123

# 4. 在 UI 中拖拽 HTTP 任务节点进行编排
```

**完整指南**: 查看 [UI_BASED_WORKFLOW_GUIDE.md](UI_BASED_WORKFLOW_GUIDE.md) ⭐

### 方式 2：快速体验演示环境

体验完整的 DolphinScheduler + 空间分析工作流：

```bash
# 1. 启动演示环境（DolphinScheduler + 空间算子引擎）
make demo

# 2. 测试空间算子工作流引擎
make demo-test

# 3. 打开 Web UI 创建工作流
# 访问: http://localhost:12345/dolphinscheduler/ui
# 用户名: admin  密码: dolphinscheduler123
```

**详细步骤**: 查看 [QUICKSTART.md](QUICKSTART.md)

### 方式 3: 本地运行示例（无需 Docker）

```bash
cd backend
pip install shapely==2.0.2

# 运行综合示例
python3 examples/comprehensive_demo.py

# 性能对比测试
python3 examples/performance_test.py
```

### 方式 4: DolphinScheduler 调度（开发者）

```bash
# 1. 启动 DolphinScheduler
make start

# 2. 打开 Web UI
make web  # http://localhost:12345/dolphinscheduler/ui

# 3. 登录
#    用户名: admin
#    密码: dolphinscheduler123

# 4. 上传空间算子
# 一键上传空间算子代码到 DolphinScheduler
./upload_to_dolphin.sh
```

### 测试空间算子

```bash
# 运行自动化测试
./test_spatial_operators.sh

# 测试单个算子
python3 backend/spatial/operator_executor.py '{
  "operator": "buffer",
  "params": {
    "input_geom": {"type": "Point", "coordinates": [116.404, 39.915]},
    "distance": 100.0,
    "segments": 8
  }
}'
```

## 📚 核心文档

### 推荐阅读顺序（按用户类型）

#### 最终用户（业务分析师、GIS 工程师）
| 文档 | 说明 |
|------|------|
| [API_VERIFICATION.md](API_VERIFICATION.md) ⭐ | API 服务功能验证报告 |
| [UI_BASED_WORKFLOW_GUIDE.md](UI_BASED_WORKFLOW_GUIDE.md) ⭐ | UI 拖拽式编排完整指南（零代码） |
| [UI_SOLUTION_SUMMARY.md](UI_SOLUTION_SUMMARY.md) | 新旧方案对比和架构设计 |
| [QUICKSTART.md](QUICKSTART.md) | 5 分钟快速入门 |

#### 开发者
| 文档 | 说明 |
|------|------|
| [FINAL_IMPLEMENTATION_REPORT.md](FINAL_IMPLEMENTATION_REPORT.md) | 完整实施报告 |
| [WORKFLOW_ENGINE_GUIDE.md](WORKFLOW_ENGINE_GUIDE.md) | 工作流引擎详解 |
| [HYBRID_ARCHITECTURE.md](HYBRID_ARCHITECTURE.md) | 混合架构设计 |
| [DOLPHIN_INTEGRATION_GUIDE.md](DOLPHIN_INTEGRATION_GUIDE.md) | DolphinScheduler 集成指南 |
| [IMPLEMENTATION_SUMMARY.md](IMPLEMENTATION_SUMMARY.md) | 实施总结 |
| [CLAUDE.md](CLAUDE.md) | 技术架构和开发指南 |

## 🧩 系统架构

```
┌─────────────────────────────────────────────────────────────┐
│  DolphinScheduler Web UI (拖拽编排)                         │
│  - 可视化 DAG 设计                                          │
│  - 参数配置表单                                             │
│  - 实时监控和日志                                           │
└───────────────────┬─────────────────────────────────────────┘
                    │ Python 任务调用
┌───────────────────▼─────────────────────────────────────────┐
│  空间算子执行层 (Python)                                    │
│  - operator_registry.py    (8 个预定义算子)                │
│  - operator_executor.py    (算子执行器)                    │
│  - operators.py            (基于 Shapely 的实现)           │
└─────────────────────────────────────────────────────────────┘
```

## 🔧 已实现的空间算子

| 算子代码 | 名称 | 分类 | 描述 |
|---------|------|------|------|
| `buffer` | 缓冲区分析 | 几何处理 | 对几何对象创建指定距离的缓冲区 |
| `intersection` | 几何相交 | 几何处理 | 计算两个几何对象的交集 |
| `union` | 几何合并 | 几何处理 | 合并多个几何对象为一个 |
| `centroid` | 计算质心 | 几何处理 | 计算几何对象的质心点 |
| `contains` | 包含关系判断 | 空间关系 | 判断几何 A 是否包含几何 B |
| `intersects` | 相交关系判断 | 空间关系 | 判断两个几何对象是否相交 |
| `distance` | 距离计算 | 空间关系 | 计算两个几何对象之间的最短距离 |
| `spatial_join` | 空间连接 | 数据处理 | 基于空间关系连接两个数据表 |

## 📖 使用示例

### 示例 1: 单算子执行

```bash
# 计算天安门 100 米缓冲区
curl -X POST http://localhost:8093/api/operators/buffer/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input_geom": {
      "type": "Point",
      "coordinates": [116.404, 39.915]
    },
    "distance": 100.0,
    "segments": 16
  }'
```

### 示例 2: 在 DolphinScheduler 中创建工作流

参见 [DEMO_SCRIPT.md](DEMO_SCRIPT.md) 中的完整演示流程。

**工作流示例**: 北京市中心区域分析

```
天安门缓冲区(1000m)  ──┐
                       ├──→ 计算交集面积
故宫缓冲区(800m)    ──┘
```

## 🛠️ 技术栈

| 类型 | 技术 |
|------|------|
| 调度引擎 | Apache DolphinScheduler 3.2.1 |
| 空间计算 | Shapely 2.0.2 (Python) |
| 后端 API | Go 1.23 + Gin (可选) |
| 前端 | Vue 3 + Element Plus (可选) |
| 容器化 | Docker + Docker Compose |

## 📁 项目结构

```
dolphin/
├── backend/
│   ├── spatial/
│   │   ├── operator_registry.py     # 算子注册中心
│   │   ├── operator_executor.py     # 算子执行器
│   │   ├── operators.py             # 算子实现
│   │   └── workflow_builder.py      # 工作流构建器
│   ├── cmd/server/main.go           # Go 后端服务（可选）
│   └── requirements.txt             # Python 依赖
├── frontend/
│   └── WorkflowEditor.vue           # 可视化编排界面（可选）
├── docker-compose.yml               # DolphinScheduler 部署配置
├── Makefile                         # 常用命令
├── upload_to_dolphin.sh             # 一键上传脚本
├── test_spatial_operators.sh        # 自动化测试脚本
└── docs/                            # 文档目录
```

## 🎓 学习路径

### 第 1 阶段: 熟悉 DolphinScheduler（1-2 天）
- ✅ 阅读 [QUICKSTART.md](QUICKSTART.md)
- ✅ 完成 Phase 1-3（创建项目、工作流、Python 任务）
- ✅ 理解 DAG、任务依赖、参数传递

### 第 2 阶段: 空间算子基础（1-2 天）
- ✅ 运行 `./test_spatial_operators.sh` 测试所有算子
- ✅ 阅读 [SPATIAL_OPERATOR_GUIDE.md](SPATIAL_OPERATOR_GUIDE.md)
- ✅ 手动执行单个算子，理解输入输出格式

### 第 3 阶段: 集成与编排（2-3 天）
- ✅ 运行 `./upload_to_dolphin.sh` 上传算子代码
- ✅ 按照 [DEMO_SCRIPT.md](DEMO_SCRIPT.md) 创建演示工作流
- ✅ 理解参数传递机制（变量池）

### 第 4 阶段: 扩展与优化（可选）
- ⬜ 添加新的空间算子（如热力图、路径规划）
- ⬜ 集成数据库数据源（PostgreSQL/PostGIS）
- ⬜ 开发前端可视化界面
- ⬜ 实现结果地图展示

## 🔍 常用命令

```bash
# 启动和停止
make start              # 启动 DolphinScheduler
make stop               # 停止服务
make restart            # 重启服务

# 监控和日志
make logs               # 查看实时日志
make stats              # 查看资源使用情况
make web                # 打开 Web UI

# 测试
./test_spatial_operators.sh         # 测试所有算子
./upload_to_dolphin.sh              # 上传算子到 DolphinScheduler

# 开发
cd backend && pip install -r requirements.txt   # 安装依赖
cd backend/cmd/server && go run main.go         # 启动 Go 服务
```

## 💡 核心优势

### ✅ 用户体验
- **无需编程**: 拖拽即可完成空间分析
- **可视化编排**: DAG 图清晰展示数据流
- **并行执行**: 自动识别并行任务（性能提升）
- **实时监控**: 查看每个算子的执行状态和日志

### ✅ 技术优势
- **模块化设计**: 算子独立封装，易于扩展
- **标准化接口**: 统一的 JSON 输入输出
- **分布式调度**: 利用 DolphinScheduler 的集群能力
- **容器化部署**: Docker 一键启动

## 📌 下一步计划

### 短期（1-2 周）
- [ ] 完善算子参数验证
- [ ] 添加数据库数据源集成（PostGIS）
- [ ] 实现结果可视化（地图展示）

### 中期（1 个月）
- [ ] 开发自定义 DolphinScheduler 插件（更友好的 UI）
- [ ] 添加更多空间算子（热力图、插值、聚类）
- [ ] 集成机器学习算子（空间预测）

### 长期（3 个月）
- [ ] 与 ADDP 主系统集成（作为空间分析模块）
- [ ] 支持大规模数据处理（分布式计算）
- [ ] 开发算子市场（用户自定义算子）

## 🤝 贡献

本项目是实验性质的 lab 项目，欢迎提交 Issue 和 PR。

## 📄 许可证

本项目遵循 ADDP 平台的开源协议。

## 🆕 Temporal Python SDK 集成 (NEW!)

除了 DolphinScheduler,本项目现已集成 **Temporal Python SDK**,提供代码优先的工作流编排方式!

### 🎯 Temporal vs DolphinScheduler

| 特性 | Temporal | DolphinScheduler |
|-----|----------|------------------|
| 定义方式 | Python 代码 | UI 拖拽 |
| 类型安全 | ✅ 强类型 | ❌ JSON |
| 调试体验 | ✅ IDE 断点 | ⚠️ 日志查看 |
| 适用场景 | 开发团队主导 | 运维团队主导 |

### 🚀 快速开始 (Temporal)

```bash
# 1. 启动 Temporal Server
docker-compose -f docker-compose-temporal.yml up -d

# 2. 启动 Worker
cd backend/temporal
pip install -r requirements.txt
python worker.py

# 3. 运行示例 (新终端)
python examples/run_buffer_workflow.py \
  --input ../../data/sample.geojson \
  --output ../../output/buffer_result.geojson \
  --distance 100

# 4. 访问 Temporal UI
open http://localhost:8080
```

### 📖 详细文档

完整的 Temporal 集成文档: [backend/temporal/README.md](backend/temporal/README.md)

**核心特性**:
- ✅ 代码即 DAG - Python 函数定义工作流
- ✅ 自动重试 - 内置容错机制
- ✅ 类型安全 - 完整 Python 类型提示
- ✅ IDE 友好 - 支持断点调试

---

## 🔗 相关链接

- [Apache DolphinScheduler 官网](https://dolphinscheduler.apache.org/)
- [Temporal 官方文档](https://docs.temporal.io/)
- [Shapely 文档](https://shapely.readthedocs.io/)
- [GeoJSON 规范](https://geojson.org/)
- [ADDP 平台主仓库](https://github.com/addp/addp)

---

**开始体验**:
- DolphinScheduler: `make start && make web` 🚀
- Temporal: 查看 [backend/temporal/README.md](backend/temporal/README.md) 📖