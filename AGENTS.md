# AGENTS.md instructions for /Users/pampa/code/addp

<INSTRUCTIONS>

## 交流语言

始终使用简体中文与用户交流。

## 项目定位

ADDP（全域数据平台）是一个采用微服务架构的企业数据平台。后端主要使用 Go + Gin + GORM，前端使用 Vue 3，基础设施包括 PostgreSQL、Redis、MinIO、Meilisearch 等。各模块通过 `common/` 和 `common-frontend/` 共享通用代码。

## 工作原则

- 优先阅读并遵守仓库中的规范文档，尤其是 `docs/spec/` 和 `docs/concepts/` 下的文档。
- 当代码实现与文档规范不一致时，先和用户核实，再修订文档规范，并基于规范开发。
- ADDP 当前处于积极开发阶段，无需为了向后兼容保留旧逻辑；过时实现应大胆删除。
- 修复问题时追根因，不做临时补丁；涉及架构层面的根因应主动提出讨论。
- 避免重复代码，优先复用或抽取到 `common/`、`common-frontend/`。
- 不要硬编码单一数据假设，例如不要默认空间字段名一定是 `geom`。
- 临时脚本和临时文档应放到操作系统临时目录，保持项目树整洁。
- 未经用户允许，不得创建分支提交；提交代码前需得到明确授权。

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
| 引擎体系、插件接口、Provider 边界 | `docs/concepts/addp引擎体系图.md`、`docs/spec/addp引擎插件接口规范.md` |
| 引擎能力声明、capabilities | `docs/spec/addp引擎能力声明规范.md` |
| 新增存储引擎/数据库 | `docs/spec/addp数据引擎扩展指南.md`、`docs/spec/addp引擎插件接口规范.md`、`docs/spec/addp引擎能力声明规范.md` |
| 新增数据类型/数据格式 | `docs/spec/addp数据格式扩展指南.md` |
| 故障和问题修复 | `docs/addp常见故障排查.md` |
| 核心概念与全局视图 | `docs/addp核心概念关系图.md` |
| 模块划分与系统结构 | `docs/addp模块架构图.md` |
| 部署与开发启动 | `docs/addp部署和开发步骤.md` |
| 存储路径和指纹 | `docs/spec/addp存储引擎路径体系规范.md`、`docs/spec/addp路径统一和指纹计算.md` |
| 登录认证 | `docs/spec/addp登录认证的统一要求.md`、`docs/concepts/addp登录认证的原理说明.md` |
| 工作流计算引擎 | `docs/spec/addp工作流计算引擎接口规范.md` |
| Transfer 引擎插件迁移后续事项 | `docs/next/engine-plugin-transfer后续事项.md` |

模块相关问题优先阅读模块自己的 `CLAUDE.md` 或模块文档：

| 模块 | 文档 |
| --- | --- |
| System | `system/CLAUDE.md` |
| Gateway | `gateway/CLAUDE.md`、`gateway/docs/gateway架构说明.md` |
| Manager | `manager/CLAUDE.md` |
| Meta | `meta/CLAUDE.md` |
| Transfer | `transfer/CLAUDE.md` |
| Orchestrator | `orchestrator/CLAUDE.md` |
| Develop | `develop/CLAUDE.md` |
| Service | `service/CLAUDE.md` |
| Model | `model/CLAUDE.md` |
| Standard | `standard/CLAUDE.md` |
| Agent | `agent/CLAUDE.md` |
| Graph | `graph/CLAUDE.md` |
| Quality | `quality/CLAUDE.md` |
| Scripts | `scripts/CLAUDE.md` |

## 前端开发约定

- 新增前端页面时，阅读 `common-frontend/docs/addp前端风格设计规范.md`（如存在）以及 `common-frontend/README.md`。
- 前端应集成到 `console` 模块中，遵循 Console 统一入口和 iframe 模块集成模式。
- 不要硬编码颜色，应使用 ADDP 主题风格 CSS 和共享前端能力。
- 仅修改前端代码时，通常无需重启后端服务。
- `common-frontend` 不应保留自己的 `node_modules`，各前端模块需通过 `overrides` 保持 Vue 单一实例。

## 后端与 API 约定

- 新增或修改 API 前，必须阅读 `docs/spec/addp-API设计规范.md` 和 `docs/spec/addp-Swagger集成指南.md`。
- API 返回结构、`data` 字段语义、错误格式和 Swagger 说明必须符合规范。
- API 路由、Handler、DTO、Swagger 注解和生成文档必须同步更新。
- API 修改后必须运行 `bash scripts/swagger/gen-swagger.sh <module>` 和 `bash scripts/swagger/check-route-coverage.sh <module>`。
- 不允许只改路由或 Handler 而留下旧 Swagger path。
- 后端通用能力优先放到 `common/`，不要在各模块重复实现。
- 各服务遵循 Handler -> Service -> Repository -> Database 的分层思路。

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
- `agent/`: AI 对话助手。
- `graph/`: 知识图谱、本体建模、图谱构建。
- `engines/`: 工作流和 Notebook 相关引擎。
- `docs/`: 平台级概念、规范、计划和故障排查文档。

## 文档维护

- ADDP 整体文档放在 `docs/`。
- 概念文档放在 `docs/concepts/`。
- 规范文档放在 `docs/spec/`。
- 计划文档放在 `docs/plan/`。
- 模块文档放在对应模块目录下。
- 未得到用户同意前，不要新增需要长期保留的文档或脚本。
- 如果某个问题需要反复修复，或以后可能反复遇到，应主动建议把根因和修复思路记录到 `docs/addp常见故障排查.md`。
- UML 相关设计应在 Markdown 文档中使用 Mermaid 代码块。

</INSTRUCTIONS>
