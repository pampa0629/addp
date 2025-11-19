# Scheduler 架构设计文档

本目录包含 ADDP Scheduler (调度编排引擎) 的完整架构设计文档。

## 📚 文档列表

### 1. [SCHEDULER_ARCHITECTURE.md](SCHEDULER_ARCHITECTURE.md)
**主架构设计文档** - 完整的系统设计方案

**内容**：
- 背景和动机（为什么需要 Scheduler）
- 设计目标（核心目标和非功能目标）
- 架构概览（完整架构图和数据流）
- 核心设计原则（4 大原则）
- 详细设计（4 层架构详解）
- 完整代码示例（可直接使用）
- 部署配置（Docker Compose）
- 迁移路线图（6 阶段实施计划）
- 监控和可观测性
- 与 DolphinScheduler 对比

**适合阅读者**：
- 架构师：了解整体设计
- 开发人员：参考代码实现
- 运维人员：了解部署配置

---

### 2. [SCHEDULER_DEPENDENCIES.md](SCHEDULER_DEPENDENCIES.md)
**依赖关系详解文档** - 模块间依赖关系分析

**内容**：
- 核心依赖关系图（可视化展示）
- 详细依赖关系表（Scheduler ↔ 各模块）
- Scheduler 与 Worker 的关系
- 完整依赖关系详解（编译时 + 运行时）
- 数据流和控制流（时序图）
- 依赖关系总结
- 部署依赖顺序
- 常见问题 (FAQ)

**适合阅读者**：
- 架构师：理解模块依赖
- 开发人员：了解接口调用
- 测试人员：理解测试边界

---

## 🎯 核心概念

### Scheduler 是什么？

**Scheduler** 是 ADDP 平台的**调度编排引擎**，负责：
- 编排复杂的多步工作流（ETL、数据扫描、血缘分析等）
- 通过 HTTP API 调用各模块服务
- 管理工作流状态、重试、超时等

**技术实现**：
- 底层基于 Temporal（可替换）
- 对外提供统一的 Scheduler API
- 命名与技术框架解耦

### 为什么命名为 Scheduler 而非 Temporal？

1. ✅ **技术中立**：不绑定特定框架
2. ✅ **易于理解**：业务人员容易理解
3. ✅ **可替换性**：将来可换底层实现（Cadence、自研等）

---

## 📖 阅读顺序建议

### 快速了解（15 分钟）
1. 阅读 [SCHEDULER_ARCHITECTURE.md](SCHEDULER_ARCHITECTURE.md) 的"背景和动机"
2. 查看"架构概览"中的架构图
3. 阅读"优势总结"

### 深入理解（1 小时）
1. 完整阅读 [SCHEDULER_ARCHITECTURE.md](SCHEDULER_ARCHITECTURE.md)
2. 重点关注"详细设计"章节
3. 查看代码示例

### 实施开发（2-3 小时）
1. 阅读"迁移路线图"
2. 参考"完整代码示例"
3. 查看"部署配置"

### 理解依赖（30 分钟）
1. 阅读 [SCHEDULER_DEPENDENCIES.md](SCHEDULER_DEPENDENCIES.md)
2. 查看依赖关系图
3. 理解时序图

---

## 🚀 快速开始

### 验证 POC（1-2 天）

```bash
# 1. 部署 Temporal Server
cd /Users/pampa/code/addp/labs/dolphin
docker-compose up -d temporal-server

# 2. 创建 Scheduler 项目
mkdir -p scheduler/{cmd/worker,workflows,activities,config}

# 3. 实现简单的 HTTP 工作流
# （参考 SCHEDULER_ARCHITECTURE.md 第 6 节代码示例）

# 4. 测试工作流
go run cmd/worker/main.go
```

### 完整实施（2-3 个月）

按照 [SCHEDULER_ARCHITECTURE.md](SCHEDULER_ARCHITECTURE.md) 第 8 节"迁移路线图"：
1. 阶段 1：基础设施准备（1 周）
2. 阶段 2：Scheduler Worker 开发（2 周）
3. 阶段 3：Transfer 模块集成（3 周）
4. 阶段 4：Meta 模块集成（3 周）
5. 阶段 5：监控和可观测性（2 周）
6. 阶段 6：生产部署（1 周）

---

## 🔗 相关资源

### ADDP 平台文档
- 主平台架构：`/Users/pampa/code/addp/CLAUDE.md`
- Transfer 模块：`/Users/pampa/code/addp/transfer/CLAUDE.md`
- Meta 模块：`/Users/pampa/code/addp/meta/CLAUDE.md`

### 技术文档
- Temporal 官方文档：https://docs.temporal.io/
- Temporal Go SDK：https://github.com/temporalio/sdk-go
- Asynq 文档：https://github.com/hibiken/asynq

### 其他调度器参考
- Apache Airflow：https://airflow.apache.org/
- Cadence：https://cadenceworkflow.io/
- DolphinScheduler：https://dolphinscheduler.apache.org/

---

## 📝 文档维护

**当前状态**: ✅ 设计完成，待实施

**更新日志**：
- 2025-01-20: 初始版本，完整架构设计

**贡献者**：
- 架构设计：Claude (Anthropic)
- 需求提出：ADDP 团队
- 验证环境：DolphinScheduler 学习实验室

---

**最后更新**: 2025-01-20
