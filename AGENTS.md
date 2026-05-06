# AGENTS.md instructions for /Users/pampa/code/addp

<INSTRUCTIONS>

## 交流语言

始终使用简体中文与用户交流。

## 项目定位

ADDP（全域数据平台）是一个采用微服务架构的企业数据平台。后端主要使用 Go + Gin + GORM，前端使用 Vue 3，基础设施包括 PostgreSQL、Redis、MinIO、Meilisearch 等。各模块通过 `common/` 和 `common-frontend/` 共享通用代码。

## 工作原则

ADDP 当前处于积极开发阶段，且核心是作为研究和教学使用，因此特别看重概念层的统一、遵从规范、架构设计优雅、无重复代码逻辑；不需要为兼容性做任何妥协和额外处理。具体包括：
- 优先阅读并遵守仓库中的规范文档，尤其是 `docs/spec/` 和 `docs/concepts/` 下的文档。
- 遇到用户不合理的设计和实现思路时，要敢于提出质疑，直到和用户探讨清楚。
- 当代码实现与文档规范不一致，或遇到设计不合理、规范和代码冲突、语义边界不清，或继续修改会影响架构方向、模块职责、长期演进时，必须先停下来和用户讨论确认。
- 修复问题时追根因，不做临时补丁，避免负负得正；涉及架构层面的根因应主动提出讨论。
- 讨论确认涉及概念和规范层面后，应先修订规范，再基于规范开发，不得绕过讨论继续实现。
- 重复是万恶之源，应避免重复代码，优先复用或抽取到 `common/`、`common-frontend/`。
- 不要硬编码单一数据假设，例如不要默认空间字段名一定是 `geom`。
- 不得为兼容保留任何旧代码、旧数据和旧逻辑，过时的内容应即刻删除。
- 临时脚本和临时文档应放到操作系统临时目录，保持项目树整洁。
- 未经用户允许，不得创建分支提交；提交代码前需得到明确授权。

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
| 新增数据类型/数据格式 | `docs/spec/addp数据格式扩展指南.md` |
| 故障和问题修复 | `docs/guide/addp常见故障排查.md` |
| 核心概念与全局视图 | `docs/concepts/addp核心概念关系图.md` |
| 模块划分与系统结构 | `docs/concepts/addp模块架构图.md` |
| 部署与开发启动 | `docs/guide/addp部署和开发步骤.md` |
| 存储路径和指纹 | `docs/spec/addp存储引擎路径体系规范.md`、`docs/spec/addp路径统一和指纹计算.md` |
| 登录认证 | `docs/spec/addp登录认证的统一要求.md`、`docs/concepts/addp登录认证的原理说明.md` |
| 工作流计算引擎 | `docs/spec/addp工作流计算引擎接口规范.md` |
| Transfer 引擎插件迁移后续事项 | `docs/next/engine-plugin-transfer后续事项.md` |

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
- `agent/`: AI 对话助手。
- `copilot/`: AI 辅助生成能力，支持 SQL、工作流和图谱抽取。
- `graph/`: 知识图谱、本体建模、图谱构建。
- `engines/`: 工作流和 Notebook 相关引擎。
- `business/`: 业务数据基础设施样例与启动脚本。
- `nginx/`: 生产/容器化反向代理配置。
- `docs/`: 平台级概念、规范、计划和故障排查文档。

## 文档维护

- ADDP 整体文档放在 `docs/`，总入口为 `docs/README.md`。
- 概念文档放在 `docs/concepts/`。
- 规范文档放在 `docs/spec/`。
- 操作指南放在 `docs/guide/`。
- 计划文档放在 `docs/plan/`。
- 模块文档放在对应模块目录下。
- 未得到用户同意前，不要新增需要长期保留的文档或脚本。
- 如果某个问题需要反复修复，或以后可能反复遇到，应主动建议把根因和修复思路记录到 `docs/guide/addp常见故障排查.md`。
- UML 相关设计应在 Markdown 文档中使用 Mermaid 代码块。

</INSTRUCTIONS>
