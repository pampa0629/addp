# Transfer 前端

本页维护前端开发、页面职责与验证导航；模块能力、支持矩阵和未支持项见[模块 README](../README.md)，运行与 API 契约见[模块说明](../CLAUDE.md)。

阅读导航：[操作起点](QUICK_START.md)、[开发与部署](#开发)、[页面入口](#主要页面)、[认证与 API](#api-访问)、[配置检查](#向导配置检查)、[验证与证据](#验证与证据)。

## 开发

前端使用 Vue 3、Pinia、Element Plus 和 Vite。依赖版本与共享库规则沿[技术栈规约](../../docs/spec/addp技术栈规约.md)、[common-frontend](../../common-frontend/README.md)，依赖准备沿[开发脚本说明](../../scripts/dev/README.md)。

开发启动与就绪检查统一见[模块快速开始](../docs/快速开始.md#一启动)，测试和构建不要求重启个人开发服务。`5176` 是 Transfer 前端的开发首选端口，实际监听以标准启动输出与 `.dev-state/ports.env` 为准；端口与公开入口的区别见[端口规范](../../docs/spec/addp端口分配.md)。

容器构建与部署沿[部署指南](../../docs/guide/addp部署和开发步骤.md)及[Prod README](../../scripts/prod/README.md)。[Dockerfile](Dockerfile) 使用仓库根目录作为构建上下文，消费声明的 Node 版本；不以模块目录的独立构建示例替代根部署流程。单模块映射与根 Compose 统一 Nginx 入口分别沿[前端 Docker 端口规则](../../docs/spec/addp端口分配.md#前端-docker-端口规则)。

## 主要页面

以下是模块内部路由与源码入口；Console 公开地址、权限落点、创建成功落点和历史栈规则统一见[前端公开路由](../CLAUDE.md#前端公开路由)及[路由事实源规范](../../docs/spec/addp前端路由与可恢复状态规范.md#二公开路由事实源)。不手工在 Console 中登记独立 iframe URL。

| 页面职责 | 内部路由 | 源码 |
| --- | --- | --- |
| 任务列表及创建入口 | `/tasks` | [TaskList.vue](src/views/TaskList.vue) |
| 创建／编辑向导 | `/tasks/create`、`/tasks/:id/edit` | [TaskWizard.vue](src/views/TaskWizard/TaskWizard.vue)、[向导状态](src/views/TaskWizard/useTaskWizardState.js) |
| 单任务配置与执行历史 | `/tasks/:id/detail` | [TaskDetail.vue](src/views/TaskDetail.vue) |
| 单次专业执行详情 | `/executions/:execution_id` | [ExecutionDetail.vue](src/views/ExecutionDetail.vue) |

向导按源、目标、字段映射、任务配置、确认五个步骤组织；字段映射控件见[FieldMappingEditor.vue](src/components/FieldMappingEditor.vue)，行为与配置沿[field_mapping 契约](../docs/transfer-基本概念及配置说明.md#五field_mapping-transform)。全局执行监控归 Monitor，Transfer 保留单任务历史和专业详情，分工见[模块页面职责](../CLAUDE.md#前端公开路由)。

## API 访问

前端复用 [createAPIClient](src/api/client.js) 与 [createAuthStore](src/store/auth.js)，通过共享 Browser AuthSession 管理登录、恢复和退出。Access Token 仅保存在 JavaScript 运行时内存中；用户资料缓存不代表已认证状态。存储、Console／iframe 与独立模式边界统一见[认证规范](../../docs/spec/addp登录认证的统一要求.md#三browser-authsession)和[令牌存储要求](../../docs/spec/addp登录认证的统一要求.md#二浏览器令牌存储)。

[任务 API 适配](src/api/tasks.js) 复用共享 `/api/v1` Client；用户任务、模块一次性执行与 TaskProvider 身份不能混用，入口和权限见[任务表 API](../docs/tables/tasks表.md#五api)与[执行记录 API](../docs/tables/task_executions表.md#六api)。不维护模块级 `.env` 或 `.env.local`，开发与部署参数由仓库根配置和标准脚本注入，见[配置规范](../../docs/spec/addp配置介绍.md)。

格式与能力统一引用[format 声明与来源](../docs/transfer-基本概念及配置说明.md#format-声明与来源)、[raw copy 目标继承](../docs/transfer-基本概念及配置说明.md#raw-copy-目标继承与一致性)和[当前支持矩阵](../docs/transfer-任务语义与同步模式.md#三当前支持矩阵)，不以页面组件或样本结果推断额外能力。

### 向导配置检查

任务向导最后一步提供手动“检查配置”，以保存时同一份完整请求调用 `POST /api/v1/transfer/task-config-checks`。检查需要当前租户的 `transfer.task.create`、`meta.catalog.read`、`system.engine.read` 三项权限；只展示已有结构和引擎事实的报告，不保存、执行任务或扫描数据。报告分别展示 supported、unsupported、unknown、字段配置位置、未检查项和运行时仍需校验项；请求失败独立显示错误。定义受支持不代表配置可执行，检查不改变现有保存行为。配置、用户、租户或所需权限变化后清除报告，并丢弃迟到的旧响应。

## 验证与证据

标准验证为 `make test-transfer-frontend`，包含单测、受控 API 夹具的浏览器交互和构建。浏览器通过共享启动器分配独立回环端口，使用合成配置，不访问个人登录态或业务服务；Platform CI 的 Transfer 前端矩阵执行同一入口并收集失败证据。

标准入口、环境前提与 Oracle CDC 专项缺口见[模块验证说明](../README.md#启动与验证)，分层与隔离规则见[测试与验收规范](../../docs/spec/addp测试与验收规范.md)。当前版本与后续增强的固定源码、失败与修复、清理及未覆盖项按[分版本证据索引](../CLAUDE.md#专项约束与分版本证据)认定；构建、受控浏览器和真实账号／Console／Hosted 验收分别报告，未运行、失败或跳过项不计为通过。

### 历史预检证据

以下保留固定提交与运行的原始结论，仅证明所列夹具和权限范围，不作为当前 HEAD 已重跑的证明：

真实只读链路复用手动 Hosted `transfer-relational-sql-etl` suite：普通用户经 Console 登录，在未保存的创建向导中验证 string→string 受支持、string→json 尚无法确定、非法 JSON 默认值不支持。预检段使用当次夹具的已保存源结构，目标为专用合成表名；拦截除预检和正常认证刷新外的修改请求及业务预览，核对任务与执行身份集合不变，取消草稿，并由夹具核对目标表未创建。`system.engine.read` 不允许租户自定义角色持有；预检由独立临时用户通过已有租户基础设施管理员内置角色取得该权限，另赋 `transfer.task.create`、`meta.catalog.read`，仅引用当次合成夹具。原传输用户权限不扩张，不修改生产权限规范。确定性门禁为 `make test-transfer-online-runner`，由现有 `make test-online-runner` 与 Platform CI 聚合；新增预检段已在 [Hosted run 38194247437](https://github.com/pampa0629/addp/actions/runs/38194247437)（提交 `e5d196e1d`）首次通过：真实三态响应、零禁止请求、任务／执行身份集合不变、草稿取消、独立身份、目标表未创建及应用／夹具／Infra 清理均由上传证据核对。该 T4 证明当次合成夹具的 Console 预检链路，不证明 MongoDB 采样输出完整或业务配置可执行。
