# 查询工作台与查询任务功能说明

[返回模块入口](../../README.md) · [模块架构](../../CLAUDE.md#关键架构) · [验证与历史证据](../../CLAUDE.md#开发与验证)

阅读导航：[操作步骤](#操作步骤)、[接口导航](#后端-api)、[数据库归属](#数据库结构)、[访问方式](#访问方式)、[能力与验收](#能力与验收边界)。

## 概述

查询工作台按 Engine 的 `capabilities.compute.query` 提供查询语言和结果形态。即时查询与保存的 `query` 任务共享统一 execution；任务可手动执行或由 Orchestrator 显式调用，Develop 不提供自身定时调度。

| 契约主题 | 权威正文 |
| --- | --- |
| 查询目标、原生／联邦来源、模板与语句诊断 | [查询工作台与预检](../../CLAUDE.md#查询工作台与预检) |
| 参数定义、默认值、本次覆盖与关系输入 | [TaskProvider 参数契约](../../../docs/spec/tasks/TaskProvider接口.md#标准响应体) |
| 查询保护、当次结果与默认历史 | [查询保护与结果交付](../../CLAUDE.md#查询保护与结果交付) |
| 同 Engine 既有表写入与编排端口 | [固定目标写入](../../CLAUDE.md#固定目标写入契约) |
| 全量导出、实例能力与交付认证 | [查询全量导出](../../CLAUDE.md#查询全量导出)、[角色选择与独立导出](../../CLAUDE.md#角色选择与独立导出) |
| 超时、预览上限与三类执行并发 | [执行策略](../../../docs/spec/configuration/执行与模块策略.md#develop-执行策略) |

## 操作步骤

### 1. 编写与保存查询

1. 打开查询工作台，选择声明 query capability 的真实 Engine；普通查询与 DuckDB 等联邦 Runtime 均使用真实 Engine ID。
2. 从左侧资源树确认查询上下文。小屏将资源树收入抽屉；原生 Engine 与联邦 Source Engine 的展示范围沿上述工作台契约。
3. 可为所选数据项生成真实查询模板，再编辑查询。模板须完成只读验证；没有可查询数据或资源发现失败时明确报错，不返回 `SELECT 1`、占位集合名等静态兜底。
4. 在唯一“查询参数”面板维护参数，可保存默认值，也可留待执行时填写。数据表参数通过资源选择器绑定已有表，SQL 以裸参数名引用；语法、类型和必填校验沿参数契约。
5. 点击“保存为任务”，填写名称、显示名称、描述、标签和超时配置；请求结构见[创建任务 API](tables/dev_tasks表.md#41-post-apiv1developtask-definitions---创建开发任务)。离开页面或加载其他任务前对未保存内容进行防丢失确认。

MongoDB database 可以作为执行范围，具体集合由合法 MQL command 声明；它与 Copilot 的具体数据项事实输入承担不同职责，AI 上下文与集合发现沿[Copilot 资源事实](../../CLAUDE.md#copilot-范围与资源事实)，不把 database 容器当作具体集合提交。

### 2. 执行与管理任务

- 工作台优先执行 Monaco 当前选区，没有选区时执行全文；确认本次参数后创建 ad-hoc execution，并在当前页面轮询结果。
- 所有已保存任务在 `/develop/tasks` 管理，按 `dev_type=query` 筛选、搜索、编辑、执行或删除。服务端对同租户任务名称做重名预查，不据此推定数据库唯一约束，字段说明见[任务表](tables/dev_tasks表.md#21-核心字段)。
- 本次参数覆盖不改写任务默认值；数据表参数具备有效绑定后可直接只读预览，Orchestrator 可分别绑定直接上游资源，并单独指定结果目标。
- 有界执行队列、领取与失联收敛由[执行监督契约](../../../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)定义。HTTP 受理不等于运行或成功；任务依赖由 Orchestrator 显式编排，不复制一套 Develop 调度器。

### 3. 查看结果与导出

工作台通过当次结果入口展示有界预览、结果类型、计数、耗时和截断状态；图结果由当前 Provider 的能力与实际响应决定。统一 Monitor 查看安全执行概览，Develop 本人专业详情承载领域结果与日志，两者[分别授权](../../CLAUDE.md#专业执行结果读取)。当次结果与持久历史的保护规则不同，历史不保存临时原值或角色所选结果。临时原值展示随其截止时间清空，等待复核响应不会延长展示时间；已过期的原值不会因迟到响应重新出现，需要时重新执行查询取得当前允许的结果。

预览不提供“下载预览”旁路。单 Engine 表格查询成功且实例支持当前语言的流式读取时，可在结果区选择“导出全部结果”，填写格式和文件名；DuckDB 联邦查询在统一流式 Provider 接入前不开放该入口。页面从原 execution 的冻结快照创建导出会话、轮询并按会话要求完成认证下载，不选择业务存储、不展示 Transfer task ID，也不从预览行自行生成 CSV。

CSV 使用原结果有序列名构造同名字段投影，`unknown` 表示“不执行字段类型转换”，不根据预览样本猜测类型。角色专用导出在交付前复核当前 User Bearer，Resource Ticket 不能替代完整角色资格；具体失败分类与下载方式沿上述独立导出契约。临时导出不触发 Meta scan，保存为业务 DataItem 属于独立能力。

## 后端 API

下表路径相对 `/api/v1/develop`，用于定位当前主链路；认证、权限和响应以现行模块契约及 Swagger 为准。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /engines`、`GET /engines/{id}/sample-query` | 发现真实查询 Engine，为可选 `locator` 生成真实模板 |
| `POST /executions` | 提交即时查询，请求使用 `dev_type=query`；统一 execution 使用 `task_type=query`、`source_task_id=null` |
| `GET /executions/{execution_id}/result` | 工作台轮询当次结果，并按当前身份、保护与有效期复核 |
| `GET /executions/{execution_id}` | 本人历史专业详情，持久预览采用默认保护 |
| `POST /executions/{execution_id}/exports` | 从成功查询快照创建全量导出会话 |
| `GET /exports/{id}`、`GET /exports/{id}/file` | 当前用户的会话状态和文件交付 |

任务 CRUD、列表筛选与手动执行请求统一见[任务表 API](tables/dev_tasks表.md#四api-端点说明)；创建、更新都不保存 Source Engine 连接或 Token，执行目标与冻结来源沿模块契约。查询参数不通过字符串插值替换字段名或 SQL 片段。 开发调用顺序及当次／历史读取示例见[SQL 开发场景](../../CLAUDE.md#场景-1执行-sql-查询)。

## 数据库结构

- [dev_tasks 表](tables/dev_tasks表.md)维护任务当前定义、模型字段和索引声明；[定义快照与并发缺口](tables/dev_tasks表.md#25-定义覆盖执行快照与并发缺口)区分当前内容、冻结输入及尚未接入的任务 `version`。
- [数据库架构](数据库架构.md)维护软引用、导出会话和支撑存储。统一执行事实归 `common.task_executions`，安全摘要与专业 DTO 沿[执行详情规范](../../../docs/spec/tasks/执行监控.md#执行详情与安全投影)，不在操作指南复制建表 SQL。

## 访问方式

| 模式 | 查询工作台 | 任务管理 |
| --- | --- | --- |
| 模块独立访问 | `/sql` | `/tasks` |
| Console 与统一产品入口 | `/develop/sql` | `/develop/tasks` |

本地开发首选模块前端 `http://localhost:5178`、Console `http://localhost:5170`；实际 origin 以启动输出和 `.dev-state/ports.env` 为准。产品部署经 Nginx 统一入口，公开 origin 使用 `ADDP_PUBLIC_ORIGIN`，域名与 HTTPS 反向代理按[端口及公开入口规范](../../../docs/spec/addp端口分配.md#前端-docker-端口规则)配置，不把 Gateway 开发端口当作生产前端地址。编辑任务的 URL 恢复与权限落点沿[前端任务路由](../../CLAUDE.md#前端任务路由)。

## 能力与验收边界

超时和预览上限由平台策略及租户允许的默认值解析，创建 execution 时冻结；任务字段的默认值不等于当前查询的有效预算。同租户归属、页面可打开或连接成功均不代替当前用户的数据与操作权限。预览行数、完整导出结果、默认历史、原值暂存和角色方式分别按各自契约验证。

参数化、CSV 全量导出和有界执行队列已有对应主路径；结果通知、队列优先级和任务定义修订历史作为后续设计候选，不算当前能力或已验收承诺。队列预算不等于优先级，定义 `version` 并发控制也不等于修订历史。

标准入口、环境前提及固定版本的失败、修复、清理和未覆盖项统一见[Develop 验证与历史证据](../../CLAUDE.md#开发与验证)。确定性门禁不代替真实 System／Gateway／Worker、文件完整交付与公开 HTTPS 验收，未执行或未覆盖项不计为通过。

## 技术实现导航

- [QueryEditor.vue](../src/views/QueryEditor.vue)：编辑、参数、本次结果与全量导出；[TaskManagement.vue](../src/views/TaskManagement.vue)、[SaveQueryDialog.vue](../src/components/SaveQueryDialog.vue)承载任务管理与保存。
- [query.js](../src/api/query.js)：查询发现、模板、预检与任务保存；[execution.js](../src/api/execution.js)：统一执行、当次／历史结果及导出会话调用。
- 后端服务、Handler 与执行监督器的职责统一见[模块实现导航](../../CLAUDE.md#重要文件位置)，存储迁移沿上述数据库架构。
