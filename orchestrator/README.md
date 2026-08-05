# Orchestrator 工作流编排模块

> 全域数据平台的工作流编排中枢，支持 DAG 编排、跨模块任务调用和定时调度

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、开发指南、常见场景和故障排查

## 🎯 核心功能

- **DAG 工作流编排**: 有向无环图任务编排，支持复杂的任务依赖关系
- **动态任务调用**: 从 System 模块动态加载 TaskProvider，无需硬编码模块 URL
- **执行参数来源**: 支持任务默认值、Step 固定值和从直接依赖步骤声明的稳定输出中选择
- **定时调度**: 基于 Cron 表达式的自动工作流触发
- **执行追踪**: 详细记录每次执行的步骤结果、错误和耗时

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Orchestrator 模块
bash scripts/dev/start.sh -orchestrator
```

- 后端: http://localhost:8084
- 前端: http://localhost:5175

### 方式 2: Docker 部署

```bash
cd orchestrator
docker-compose up -d
```

## 📡 主要 API 端点

```
编排管理:    POST/GET/PUT/DELETE /api/v1/orchestrations
执行管理:    GET /api/v1/executions
手动触发:    POST /api/v1/orchestrations/{id}/execute
定时调度:    PUT /api/v1/orchestrations/{id}/schedule
```

完整 API 文档请查看 [CLAUDE.md#API端点](./CLAUDE.md#api-端点)

## 🏗️ 工作流编排流程

```
定义编排 (DAG)
    ↓
手动触发 / 定时触发 (Scheduler)
    ↓
创建执行实例
    ↓
Executor 解析 DAG → 拓扑排序
    ↓
逐步执行各步骤 (支持并发)
    ↓
记录执行结果 (成功/失败/超时)
```

## 🔗 动态任务提供者支持

Orchestrator 通过 System 模块的 TaskProvider 注册表实现：

- **无代码扩展**: 新增任务类型由 owner 模块注册 TaskProvider capabilities，无需修改 Orchestrator
- **统一接口**: 所有任务提供者通过统一 TaskProvider endpoint 调用
- **清晰边界**: Orchestrator 引用任务定义，不拥有业务任务定义

## 🐛 常见问题

### 检测到循环依赖？

检查步骤的 `depends_on` 字段，确保不存在环形依赖。例如：

```json
❌ 错误: {"id": "A", "depends_on": ["B"]}, {"id": "B", "depends_on": ["A"]}
✅ 正确: {"id": "A", "depends_on": []}, {"id": "B", "depends_on": ["A"]}
```

### 任务提供者配置缓存？

TaskProviderRegistry 缓存任务提供者配置 5 分钟。修改模块 TaskProvider 注册信息后，可重启 Orchestrator 立即生效：

```bash
bash scripts/dev/restart.sh -orchestrator
```

### 更多问题？

详见 [CLAUDE.md#常见开发场景](./CLAUDE.md#常见开发场景) 和 [CLAUDE.md#注意事项](./CLAUDE.md#注意事项)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（架构、场景、故障排查）
- **[docs/DATA_STRUCTURES.md](./docs/DATA_STRUCTURES.md)** - 数据结构和 API 详解
- **[docs/PARAMETER_TEMPLATING.md](./docs/PARAMETER_TEMPLATING.md)** - 参数模板化功能
- **[../system/CLAUDE.md](../system/CLAUDE.md)** - System 模块（引擎管理）
- **[../meta/CLAUDE.md](../meta/CLAUDE.md)** - Meta 模块（元数据扫描）
- **[../transfer/CLAUDE.md](../transfer/CLAUDE.md)** - Transfer 模块（数据传输）
- **[../manager/CLAUDE.md](../manager/CLAUDE.md)** - Manager 模块（数据管理）

---

Copyright © 2025 ADDP Team
