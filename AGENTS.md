# AGENTS.md instructions for /Users/pampa/code/addp

<INSTRUCTIONS>

## 交流语言

始终使用简体中文与用户交流。

## 项目定位

ADDP（All Domain Data Platform）是基于 Data Fabric、面向全域数据的企业级数据平台。平台以统一抽象连接和管理各类数据引擎、计算引擎、数据类型与数据格式，服务于研究、教学和架构验证。

## 工作原则

ADDP 处于积极开发阶段，当前更重视概念统一、规范遵从、架构优雅和代码简洁，默认不兼容旧实现，任何兼容分支都视为缺陷。
以下规则优先级最高，和其他内容冲突时，以此为准。

0，最高优先级规则
- 默认不兼容旧实现，不保留兼容分支、兼容字段、兼容 query 或双轨路线。
- 不确定时先停下来确认，不要自行猜测、补丁式兜底或绕路实现。
- 涉及概念、术语或规范冲突时，先修订文档，再实现代码。
- 任何变更都必须给出最小但足够的测试或验证命令。
- 只允许单一技术路线，旧路径必须删除，不能旁路共存。
- 本地共享 `addp-postgres` 只允许使用 `addp_test` 和 `addp_iam_test` 两个测试 database。测试必须走根 `Makefile` 或 `scripts/test/` 的标准入口，禁止为单次验证直接执行 `createdb`、`CREATE DATABASE`、`dropdb` 或 `DROP DATABASE`；如现有标准入口不能满足隔离要求，先完善标准入口及自动清理，再运行测试。GitHub Actions 独占 PostgreSQL Service 不受本地库名限制，但仍必须由 workflow 创建并随 Job 销毁。

一，文档优先，统一概念
- 优先阅读并遵守仓库规范，尤其是 `docs/spec/` 和 `docs/concepts/`。遇到核心概念边界时，先查对应规范和术语表，再看实现。
- 术语表是稳定词汇表。新增概念、统一命名、收敛歧义时，先补术语表，再谈代码实现。
- 面对已有实现，先回到概念和规范，查清事实来源、消费链路与模块边界，再讨论取舍和迁移方案，最后才动代码，避免用局部修补覆盖尚未理解的系统设计。
- 遇到概念不清、架构不合理、代码与规范冲突或影响长期演进的问题，先停下来与用户讨论确认。
- 各模块、目录保持边界清晰，不得为完成任务跨越边界

二，回到本质，保持克制
- 从原始需求和问题本质出发，不套用惯例或模板。
- 动机、目标或设计路径不清晰时，主动追问；路径明显不优时，直接指出并给出更简单的方案。
- 修复问题要追根因，彻底解决问题，避免“头疼医头脚疼医脚”。
- 没有明确需求和价值时，不新增实体、抽象、配置或流程。
- 输出只保留影响决策和执行的信息。

三，避免重复，拒绝硬编码
- 优先复用或抽取通用能力到 `common/`、`common-frontend/`，避免重复逻辑。
- 同一语义职责、相同输入输出与交互契约的前端能力只允许一个规范实现；新建组件前必须检索现有实现，业务模块只保留领域组合或协议适配，替换后必须删除旧路径并增加必要的唯一所有权测试。详细规则以 `docs/spec/addp开发原则.md` 为准。
- 不硬编码单一数据假设，例如不要默认空间字段名一定是 `geom`。
- 临时脚本和临时文档放到操作系统临时目录，保持项目树整洁。
- 单人开发阶段，人工开发始终直接使用 `main`，不创建功能分支。
- Renovate 自动化升级分支是唯一例外，必须使用 `renovate/` 前缀，并在合并或关闭后自动删除。
- 未经用户明确授权，不提交代码；除上述 Renovate 例外外，不创建分支。

四，效率优先，充分沟通
- 每次启动开发工作，特别是在有工作清单的情况下，持续开发，中间不要停，直到完成全部任务或者遇到必须要讨论的地方。
- 完成既定工作后，主动推荐下一步的工作内容。
- 架构层面的设计、根因必须主动提出讨论。

五，完成标准
- 新逻辑只能有一条主路径
- 旧路径删除完成
- 关键测试通过
- Swagger/文档同步完成
- 每次代码、功能、Bug 修复、依赖或环境变更，都必须在实施前识别受影响的测试层级和 CI/CD 门禁；实现、测试、根 `Makefile` 标准入口、`.github/workflows/` 编排及相关文档必须在同一次变更中同步完成，不能把 CI/CD 适配留到推送失败后再补。
- 新增模块、测试入口、构建方式、数据库或外部服务依赖时，必须确认现有自动发现或路径登记能够命中；不能命中时必须同步修改 CI 注册和平台一致性检查。仅说明“以后接入 CI”不算完成。
- 交付前必须先运行与改动范围匹配的最小充分门禁；若受环境限制无法运行，应明确列出未验证项、原因以及将由哪个已有 CI 门禁验证，不能把已知可在本地发现的问题留给 CI 通知。

## 技术架构
微服务架构，后端主要使用 Go + Gin + GORM，前端使用 Vue 3，基础设施包括 PostgreSQL、Redis、MinIO、Meilisearch 等。各模块通过 `common/` 和 `common-frontend/` 共享通用代码。

## 文档总入口

- `docs/README.md`

## 必读文档导航

遇到以下场景时，主动阅读对应文档：

| 场景 | 必读文档 |
| --- | --- |
| 开发原则与编码规范 | `docs/spec/addp开发原则.md` |
| API 设计、响应格式、HTTP 状态码、Swagger | 必须阅读 `docs/spec/addp-API设计规范.md`、`docs/spec/addp-Swagger集成指南.md` |
| 环境配置、密钥、`.env` | `docs/spec/addp配置介绍.md` |
| 端口 | `docs/spec/addp端口分配.md` |
| Go/前端依赖版本 | `docs/spec/addp技术栈规约.md` |
| 共享模块使用 | `docs/concepts/addp共享模块介绍.md` |
| 创建新模块 | `docs/spec/addp新模块开发指南.md` |
| 国际化、语言切换、翻译文件、双语 Swagger | `docs/concepts/addp国际化体系图.md`、`docs/spec/addp国际化开发规范.md` |
| 引擎体系、插件接口、Provider 边界 | `docs/concepts/addp引擎体系图.md`、`docs/spec/addp引擎插件接口规范.md` |
| 引擎能力声明、capabilities | `docs/spec/addp引擎能力声明规范.md` |
| 新增存储引擎/数据库 | `docs/spec/addp数据引擎扩展指南.md`、`docs/spec/addp引擎插件接口规范.md`、`docs/spec/addp引擎能力声明规范.md` |
| 新增数据类型/数据格式 | `docs/spec/addp数据类型与文件格式扩展指南.md` |
| 核心概念与全局视图 | `docs/concepts/addp核心概念关系图.md` |
| 核心概念统一 | `docs/concepts/addp术语表.md`、`docs/spec/addp存储引擎路径体系规范.md`、`docs/spec/addp路径统一和指纹计算.md`、`docs/spec/addp元数据attributes规范.md` |
| 模块划分与系统结构 | `docs/concepts/addp模块架构图.md` |
| 部署与开发启动 | `docs/guide/addp部署和开发步骤.md` |
| 存储路径和指纹 | `docs/spec/addp存储引擎路径体系规范.md`、`docs/spec/addp路径统一和指纹计算.md` |
| 登录认证 | `docs/spec/addp登录认证的统一要求.md`、`docs/concepts/addp登录认证的原理说明.md` |
| 工作流计算引擎 | `docs/spec/addp工作流计算引擎接口规范.md` |
| 本地数据库测试、PostgreSQL 门禁 | `scripts/infra/README.md` 的“PostgreSQL database 清单”、对应的 `scripts/test/*-postgres-gate.sh` |

模块相关问题优先阅读对应目录下的 `CLAUDE.md`；如果模块还有 `docs/` 或 `README.md`，按 `CLAUDE.md` 中的导航继续阅读。Gateway 路由相关问题还需阅读 `gateway/docs/gateway架构说明.md`。

## 前端开发约定

- 新增前端页面时，阅读 `common-frontend/docs/addp前端风格设计规范.md`（如存在）以及 `common-frontend/README.md`。
- 新增或修改用户可见文本、语言切换、翻译文件时，阅读 `docs/concepts/addp国际化体系图.md` 和 `docs/spec/addp国际化开发规范.md`。
- 前端应集成到 `console` 模块中，遵循 Console 统一入口和 iframe 模块集成模式。
- 不要硬编码颜色，应使用 ADDP 主题风格 CSS 和共享前端能力。
- 仅修改前端代码时，通常无需重启后端服务。
- `common-frontend` 不应保留自己的 `node_modules`，各前端模块需通过 `overrides` 保持 Vue 单一实例。

## 后端与 API 约定

- 新增或修改 API 前，必须阅读 `docs/spec/addp-API设计规范.md` 和 `docs/spec/addp-Swagger集成指南.md`。
- 新增或修改后端用户可见错误消息、Swagger 双语注解时，必须阅读 `docs/spec/addp国际化开发规范.md`。
- API 返回结构、错误格式、Swagger 同步、覆盖校验、后端分层和共享能力归属等细则以以上两份规范文档为准。

## 启动与验证

基础启动：

```bash
bash scripts/infra/up.sh
bash scripts/dev/start.sh
```

模块选择启动示例：

```bash
bash scripts/dev/start.sh -system
```

后端代码修改后，不要用 `go run` 或 `go build` 直接运行临时二进制，也不要自行创建 `bin` 目录。按模块重启验证：

```bash
./scripts/dev/restart.sh -<模块名>
```

在 Codex 等命令结束后会回收后台进程的托管执行环境中，需要保持服务存活时使用 `bash scripts/dev/keepalive.sh ...`；原因和边界见 `docs/guide/addp常见故障排查.md` 的“Codex 等托管命令环境中 restart.sh 退出后服务立刻不可用”。注意 `keepalive.sh restart -<模块名>` 会先停止整套 ADDP 开发环境，再只启动指定模块及其依赖；如果用户已在外部终端运行全套服务，不要由 Codex 执行局部 restart 接管。

如果修改了 `common/` 中的代码，使用：

```bash
./scripts/dev/restart.sh -all
```

常用访问地址：

- Console: `http://localhost:5170`
- Gateway: `http://localhost:8000`
- System Backend: `http://localhost:8180`

核心基础设施端口：

- PostgreSQL: `15432`
- Redis: `16379`
- MinIO: `19000-19001`（infra）/ `9002-9003`（business）

## 仓库结构速览

- `common/`: 后端共享库。
- `common-frontend/`: 前端共享库。
- `common-python/`: Python 后端共享客户端与工具。
- `console/`: 控制台统一入口。
- `system/`: 用户认证、租户、日志、系统管理。
- `gateway/`: API 网关和路由转发。
- `manager/`: 数据管理、目录展示、数据预览。
- `meta/`: 元数据解析、存储、查询、扫描。
- `transfer/`: 数据导入、导出、同步。
- `orchestrator/`: 工作流编排、任务调度。
- `develop/`: SQL 工作台、Notebook、算子工作流。
- `service/`: 数据服务发布与外部服务注册。
- `monitor/`: 执行监控、统计分析、健康检查。
- `standard/`: 数据标准、数据元、指标、术语、码值。
- `model/`: 数据建模、逻辑表、分层和模型关系。
- `quality/`: 数据质量规则、检测任务和问题治理。
- `asset/`: 数据资产目录、发布、授权申请和评价。
- `portal/`: 用户侧数据资产门户和资产申请入口。
- `agent/`: AI 对话智能体。
- `copilot/`: AI 辅助生成能力，支持算子工作流生成和图谱抽取。
- `graph/`: 知识图谱、本体建模、图谱构建。
- `engines/`: 工作流和 Notebook 相关引擎。
- `business/`: 业务数据引擎部署、启动和样例生成脚本。
- `scripts/`: 开发、编译、打包和部署等脚本。
- `nginx/`: 生产/容器化反向代理配置。
- `docs/`: 平台级概念、规范、计划和故障排查文档。

## 文档维护

- ADDP 整体文档放在 `docs/`，总入口为 `docs/README.md`。
- 概念文档放在 `docs/concepts/`。
- 规范文档放在 `docs/spec/`。
- 操作指南放在 `docs/guide/`。
- 远期计划文档放在 `docs/plan/`。
- 正在积极开发中的文档放在 `docs/next/`。
- 模块文档放在对应模块目录下。
- 未得到用户同意前，不要新增需要长期保留的文档或脚本。
- 如果某个问题需要反复修复，或以后可能反复遇到，应主动建议把根因和修复思路记录到 `docs/guide/addp常见故障排查.md`。
- UML 相关设计应在 Markdown 文档中使用 Mermaid 代码块。

</INSTRUCTIONS>
