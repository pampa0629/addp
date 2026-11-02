# Develop 开发工作台

> ADDP 平台的开发工作台，提供 capability 驱动查询、算子工作流和 Notebook 任务管理

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、执行流程、API 详解和常见场景
- [开发与验证](CLAUDE.md#开发与验证) - 监督器与事件、查询保护和导出的分版本历史证据及未覆盖项
- [查询与执行专题](CLAUDE.md#关键架构) - 查询预检、保护、关系写入、导出、运行监督与 Copilot 的职责分工
- [执行策略](../docs/spec/configuration/执行与模块策略.md#develop-执行策略) - 平台上限、租户默认超时、热更新与执行快照；[验证与证据](../docs/spec/configuration/执行与模块策略.md#develop-验证入口与证据边界)分别说明门禁职责和历史验收范围。

## 🎯 核心功能

- **查询工作台**: 数据资源树 + Monaco Editor + 表格/图结果预览；右下角 AI 助手按当前 Query Engine 生成候选查询语言
- **GIS 工作流**: 可视化编辑和执行空间计算工作流，按所选运行时动态发现算子；右下角 AI 助手生成候选工作流。能力与算子契约见[算子发现服务](CLAUDE.md#算子发现服务)。
- **Notebook 任务**: 创建／上传、交互编辑、下载和显式重绑定 Notebook 引擎，并查看统一执行历史；右下角 AI 助手确认 Session 数据源后生成 Python/GeoPandas 单元。
- **算子管理**: 聚合工作流运行时动态算子，供工作流编辑器使用
- **统一执行历史**: 所有执行记录写入公共执行表，并在 Monitor 中统一筛选和回溯；Develop 只保留领域结果详情

Notebook 交互 Session 与排队执行的授权分开。当前一次性 `script` execution 领取后以 `develop.script.authorization_unavailable` 失败，不派发 Jupyter Runtime；交互 Session 不进入该执行队列。当前能力、未完成的排队运行授权和真实验收边界见[执行监督与授权](../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)与[会话访问规则](CLAUDE.md#执行授权与效果)。

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

以下命令从仓库根目录执行，环境前提见[部署指南](../docs/guide/addp部署和开发步骤.md#快速开始)。

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Develop 模块
bash scripts/dev/start.sh -develop
```

开发首选地址为后端 `http://localhost:8185`、前端 `http://localhost:5178`；实际地址以启动输出和 `.dev-state/ports.env` 为准，规则见[端口规范](../docs/spec/addp端口分配.md)。

`-develop` 不隐式启动 Workflow、Jupyter 等 Runtime；联调时按[模块与 Runtime 选择指南](../scripts/docs/模块选择启动指南.md#显式选择-runtime-联调)显式选择。Develop 后端与前端在开发模式使用原生进程，产品部署保留正式容器镜像；支持平台与各 Runtime 的准备要求见[引擎运行方式](../engines/README.md#本地开发运行方式)。

### 方式 2: Docker 部署

本地产品容器通过标准入口启动整套平台与内置 Runtime：

```bash
bash scripts/local/start.sh
```

镜像准备、Compose 归属、停止与数据卷边界见[Local README](../scripts/local/README.md#脚本说明)；生产服务器按[Prod README](../scripts/prod/README.md#首次部署)准备与启动。平台与 Runtime 分属 `addp-platform`、`addp-runtimes`，根 Compose 只发布 Nginx 统一入口，访问地址按 `ADDP_PUBLIC_ORIGIN` 与[端口及公开入口规范](../docs/spec/addp端口分配.md#前端-docker-端口规则)确认。容器状态或启动成功不代替 Notebook 授权、公开 HTTPS 与业务结果验收。

## 📡 主要 API 端点

```
即时查询:    POST /api/v1/develop/executions
执行详情:    GET /api/v1/develop/executions/{execution_id}
任务定义:    GET/POST /api/v1/develop/task-definitions
存储引擎绑定: GET /api/v1/develop/task-definitions/{id}/storage-engine-bindings
存储引擎重绑定: PUT /api/v1/develop/task-definitions/{id}/storage-engine-bindings/{source_engine_id}
任务执行:    POST /api/v1/develop/task-definitions/:id/execute
算子发现:   GET /api/v1/develop/workflow-engines/{workflow_engine_id}/operators
领域执行投影: GET /api/v1/develop/executions
Notebook引擎: GET /api/v1/develop/notebook-engines
Kernel发现: GET /api/v1/develop/notebook-engines/{engine_id}/kernels
Notebook:   GET /api/v1/develop/notebooks
Notebook上传: POST /api/v1/develop/notebooks/upload
Notebook运行绑定: PUT /api/v1/develop/notebooks/{id}/runtime-binding
Notebook Copilot: POST /api/v1/develop/notebook-copilot-sessions/{session_id}/generate
```

Notebook、查询和工作流 Copilot 都只生成待确认的候选结果。Notebook Copilot 的数据源范围严格限定为当前交互 Session 可访问的实时 Engine Catalog：首次请求先按业务角色列出候选，存在歧义时逐角色确认；确认后才读取字段、几何列和 CRS 等事实并生成代码。生成的 Notebook 单元只允许通过同源 JupyterLab bridge 插入，不自动执行。

完整 API 文档请查看 [CLAUDE.md#常见开发场景](./CLAUDE.md#常见开发场景)

## 🔐 认证方式

- 用户 API 使用 System 签发的短期 User Access Token，并通过 canonical AuthContext 解析
- 普通用户 JSON API 请求须携带 `Authorization: Bearer <token>` 头；Notebook Session 的 HTTP／WebSocket 代理和 Kernel 能力按[会话访问规则](CLAUDE.md#执行授权与效果)分别校验，不使用 Runtime 直连入口。
- 数据按租户自动隔离

## 🐛 常见问题

### 查询执行超时怎么办？

按当前策略调整 `timeout` 或优化查询。初始策略为默认 30 秒、最大 5 分钟；实际默认值可由租户覆盖，最大值和 `query_result_limit` 由平台维护，以[执行策略](../docs/spec/configuration/执行与模块策略.md#develop-执行策略)为准。新 execution 冻结有效超时与预览上限，后续配置变化不改写已有快照；执行步骤见 [SQL 查询场景](./CLAUDE.md#场景-1执行-sql-查询)。

### 工作流执行失败？

先定位本人有权读取的失败 execution，查看[失败排查](./CLAUDE.md#场景-3调试工作流执行失败)，分别核对受控诊断、Develop 实例正文与 Runtime 输出；执行读取权和模块正文授权独立判断。启停或注册失败另见[启动脚本排查](../scripts/docs/启动脚本使用指南.md#排查失败)。

### 内存不足错误？

按[内存与准入说明](CLAUDE.md#3-geopython-workflow-内存管理)核对实际输入、部署限制、Develop 并发与 Runtime 额度；GDAL 缓存不等于进程总内存上限，运行时选择与 Spark 显式绑定也按该节确认。

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（核心架构、执行流程、常见场景）
- [GeoPython Workflow 运行说明](../engines/geopython-workflow/docs/geopython-workflow容器管理指南.md) - 原生开发与产品容器部署
- **[system/CLAUDE.md](../system/CLAUDE.md)** - 数据库连接与认证
- [共享数据库桥接目录](../common/dbbridge/) - 数据库桥接实现
- [执行记录保留与清理](CLAUDE.md#5-执行记录清理) - 30 天事件维护、180 天概览目标、只读评估与历史验收边界
- [任务定义、执行快照与并发缺口](frontend/docs/tables/dev_tasks表.md#25-定义覆盖执行快照与并发缺口) - 当前定义、冻结输入与正式服务端版本的区别

---

Copyright © 2025 ADDP Team
