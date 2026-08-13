# Quality 模块 CLAUDE.md

本文件为 Claude Code 在 `quality/` 目录下工作时提供指导。

## 必读规范

- [ADDP 数据质量规范](../docs/spec/addp数据质量规范.md)：Quality 的规则契约、模块边界、PostgreSQL 方言、执行授权、评分、问题状态机和不支持范围的唯一标准。
- [ADDP 任务体系规范](../docs/spec/addp任务体系规范.md)：统一 execution、TaskProvider 和持久执行生命周期。
- 涉及 API、Swagger、认证或前端时，继续遵守仓库根目录 `AGENTS.md` 指向的对应规范。

`docs/plan/` 中的早期数据治理规划只作背景参考，不得覆盖上述正式规范。

## 模块概述

**Quality 模块** 是 ADDP 平台的数据质量管理中心，负责：

- 规则应用管理（RuleApplication）：将 Standard 模块定义的数据元质量规则，映射到具体数据库的表字段
- 检查任务管理（CheckTask）：定义和执行确定 PostgreSQL 引擎、Schema、表范围的质量检查任务
- 质量检查执行：通过持久 worker、安全 SQL 编译和 Execution Authorization 执行规则并计算表级/字段级质量评分
- 问题工单管理（Issue）：对检查失败的规则自动生成问题工单，支持状态流转（待处理 → 已解决/已忽略）
- 执行记录查询：读取 `common.task_executions` 查看历史执行记录及详细结果

**端口**:
- 后端: `8182`（环境变量 `QUALITY_BACKEND_PORT`）
- 前端: `5183`（开发环境）/ `8113`（Docker 环境）

**数据库 Schema**: `quality`

## 目录结构

```
quality/
├── authorization/
│   └── permissions.yaml                     # Quality owner Permission Manifest
├── backend/
│   ├── cmd/server/main.go                    # 应用入口
│   ├── go.mod                                # github.com/addp/quality
│   └── internal/
│       ├── api/
│       │   ├── router.go                     # 路由配置（/api/v1/quality 前缀）
│       │   ├── rule_application_handler.go   # 规则应用 CRUD
│       │   ├── check_task_handler.go         # 检查任务 CRUD + 手动执行
│       │   ├── execution_handler.go          # 执行记录查询
│       │   └── issue_handler.go              # 问题工单查询和状态更新
│       ├── config/config.go                  # 配置加载（基于 common.BaseConfig）
│       ├── models/
│       │   ├── rule_application.go           # 规则应用模型
│       │   ├── check_task.go                 # 检查任务模型
│       │   └── issue.go                      # 问题工单模型
│       ├── repository/
│       │   ├── rule_application_repo.go
│       │   ├── check_task_repo.go
│       │   └── issue_repo.go
│       └── service/
│           ├── rule_engine.go                # 规则加载与应用服务
│           ├── check_task_service.go         # 检查任务 CRUD 服务
│           ├── check_executor.go             # 持久 worker、Execution Authorization、评分与 Issue 协调
│           ├── sql_generator.go              # 规则→SQL 转换器（6 种规则类型）
│           └── issue_service.go              # 问题工单服务
└── frontend/
    └── src/
        ├── api/
        │   ├── client.js                     # API 客户端初始化（Axios）
        │   ├── auth.js                       # 认证相关
        │   └── quality.js                    # Quality 模块所有 API 接口
        ├── store/
        │   └── auth.js                       # Pinia 认证存储
        ├── components/
        │   └── Layout.vue                    # 双模式布局（Console 嵌入/独立访问）
        └── views/
            ├── Login.vue
            ├── RuleApplicationList.vue       # 规则应用配置页
            ├── CheckTaskList.vue             # 检查任务列表
            ├── ExecutionList.vue             # 执行记录列表
            ├── ExecutionDetail.vue           # 执行详情（评分、规则明细）
            └── IssueList.vue                 # 质量问题工单列表
```

## 数据库表结构

所有模型使用 PostgreSQL Schema `quality`（通过 GORM 的 `TableName()` 方法指定）。

### `quality.rule_applications` — 规则应用（字段-规则映射）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 主键 |
| tenant_id | int64 | 租户 ID，带索引 |
| element_id | int64 | 引用 `standard.elements.id`（数据元） |
| engine_id | int64 | 目标数据库引擎 ID |
| schema_name | string | 目标 Schema 名 |
| table_name | string | 目标表名 |
| column_name | string | 目标字段名 |
| rule_config | JSONB | 质量规则快照（从数据元获取并存储，避免规则变更影响历史检查） |
| enabled | bool | 是否启用（默认 true） |
| created_by / updated_by | int64 | 操作人 |

### `quality.check_tasks` — 检查任务定义

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 主键 |
| tenant_id | int64 | 租户 ID |
| name / description | string | 任务名称 / 描述 |
| engine_id | int64 | 目标引擎 ID |
| schema_name | string | 必填：目标 Schema |
| table_name | string | 必填：目标表 |
| last_run_at | timestamp? | 最近执行时间 |
| last_execution_id | string | 最近一次 `common.task_executions.execution_id` |
| last_execution_status | string | 最近一次执行状态 |
| created_by / updated_by | int64 | 操作人 |

### `quality.issues` — 质量问题工单

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 主键 |
| tenant_id | int64 | 租户 ID |
| execution_id / last_execution_id | string | 首次发现和最近观测到该问题的 execution |
| rule_application_id | int64 | 关联规则应用；与 `tenant_id` 共同构成当前问题唯一身份 |
| rule_type | string | 数据库存储字段；API JSON 字段名为 `type` |
| severity / message | string | 规则严重级别和说明 |
| column_name / table_name / schema_name | string | 问题字段定位 |
| engine_id | int64 | 所属引擎 |
| failed_count / total_count | int64 | 失败行数 / 总行数 |
| pass_rate | float64 | 通过率（0-100） |
| detail | JSONB | 详情（含错误信息等） |
| status | string | `open` / `resolved` / `ignored` |
| resolved_at / resolved_by / resolution_note | nullable | 人工或自动解决事实；人工处理必须提供说明 |
| last_observed_at | timestamp? | 最近一次规则观测时间 |

> **说明**: 执行记录不在 quality schema，写入 `common.task_executions`，module 标记为 `quality`，执行结果（质量评分、规则明细等）存储在 `metadata` JSONB 字段中。

## API 端点（`/api/v1/quality`）

### 规则应用
```
GET    /api/v1/quality/rule-applications          # 列表（支持过滤：engine_id, schema_name, table_name；返回 Standard 当前数据元摘要投影）
GET    /api/v1/quality/rule-applications/element-candidates # 创建页数据元候选（Quality 服务身份投影）
POST   /api/v1/quality/rule-applications          # 创建（传入 element_id，后端自动获取质量规则快照）
GET    /api/v1/quality/rule-applications/:id      # 详情
PUT    /api/v1/quality/rule-applications/:id      # 显式启停，请求体必须为 {"enabled": true|false}
DELETE /api/v1/quality/rule-applications/:id      # 删除
```

规则应用创建页不得由浏览器直连 Standard 搜索数据元；候选统一通过 Quality API 返回 `id/name/code/quality_rules`，权限只依赖 `quality.rule_application.create`。

### 检查任务
```
GET    /api/v1/quality/check-tasks                # 列表
POST   /api/v1/quality/check-tasks                # 创建
GET    /api/v1/quality/check-tasks/:id            # 详情
PUT    /api/v1/quality/check-tasks/:id            # 更新
DELETE /api/v1/quality/check-tasks/:id            # 删除
POST   /api/v1/quality/check-tasks/:id/run        # 手动触发执行（异步，立即返回 execution_id）
```

检查任务创建和更新必须通过 System 实时 Catalog 选择并校验 PostgreSQL Schema/表；Quality 只持久化 `engine_id + schema_name + table_name`，不保存 CatalogPath，也不依赖 Meta 扫描状态。

### TaskProvider 标准入口
```
GET    /api/v1/quality/tasks                       # 列表，task_type 仅支持 check
GET    /api/v1/quality/tasks/:task_type/:id        # 详情
POST   /api/v1/quality/tasks/:task_type/:id/execute # 执行
```

### 执行记录（只读，读 `common.task_executions`）
```
GET    /api/v1/quality/executions                 # 列表（分页）
GET    /api/v1/quality/executions/:execution_id   # 详情及结果（含质量评分、字段评分、规则明细）
```

### 问题工单
```
GET    /api/v1/quality/issues                     # 列表（支持过滤：status, engine_id）
GET    /api/v1/quality/issues/:id                 # 详情
PUT    /api/v1/quality/issues/:id/status          # 更新状态（resolved / ignored，仅对 open 状态有效）
```

### 健康检查
```
GET    /health                                 # 服务健康检查
```

## 核心执行流程

### 质量检查执行（`check_executor.go`）

```
触发（POST /run）
    ↓
生成 execution_id（UUID）
    ↓
在任务定义行锁保护下创建 common.task_executions（status: pending），并冻结启用的 RuleApplication 快照到 execution_config
    ↓
签发 Execution Authorization：手动执行使用 User Access Token，编排子执行从 parent execution 派生
    ↓
持久 worker 使用 FOR UPDATE SKIP LOCKED 领取已授权的 pending execution：
    1. 写 running、started_at、attempt 和 worker lease，并在执行期间续租
    2. 通过 System 的 ExecutionEngineAccess 获取授权后的 PostgreSQL 连接事实
    3. 严格解析 execution_config 中的版本化规则快照
    4. 通过 SQLGenerator 安全引用标识符、绑定所有规则参数并执行聚合检查
    5. 计算规则、字段和表级评分；结果写入 execution.metadata
    6. 以 tenant_id + rule_application_id 对 Issue 做幂等 reconcile
    7. 校验 lease owner 后原子写 execution 终态和 CheckTask 最近执行摘要
    ↓
worker 崩溃后由 lease 恢复：未达 max_attempts 返回 pending，达到上限写 failed
```

### SQL 生成器（`sql_generator.go`）

支持 6 种基础规则类型：

| 规则类型 | `params` | 语义 |
|----------|----------|------|
| `not_null` | `{}` | 值不得为 NULL |
| `unique` | `{}` | 非 NULL 值不得重复 |
| `format` | `pattern` | 非 NULL 文本匹配 PostgreSQL 正则 |
| `length` | `min` / `max` 至少一个 | 非 NULL 文本长度位于闭区间 |
| `value_range` | `min` / `max` 至少一个 | 非 NULL 数值位于闭区间 |
| `allowed_values` | 非空 `values` | 非 NULL 值属于枚举集合 |

规则文档唯一版本为 `addp.quality.rules/v1`。schema、table、column 只通过 PostgreSQL dialect 引用，正则、边界和枚举值只通过绑定参数进入 SQL；不得拼接自定义 SQL。

### 执行结果 JSON 结构

```json
{
  "schema_version": "addp.quality.execution-result/v1",
  "quality_score": 86.67,
  "total_rules": 15,
  "passed_rules": 13,
  "failed_rules": 2,
  "field_scores": [
    { "column": "mobile_phone", "score": 95.5, "rule_count": 4 }
  ],
  "rule_details": [
    {
      "rule_application_id": 123,
      "type": "format",
      "severity": "error",
      "message": "手机号格式不正确",
      "column": "mobile_phone",
      "table": "users",
      "schema": "public",
      "pass_rate": 95.5,
      "failed_count": 450,
      "total_count": 10000,
      "passed": false
    }
  ]
}
```

## 前端路由

```
/quality/rule-applications          # 规则应用配置列表
/quality/check-tasks                # 检查任务列表（含手动执行入口）
/quality/executions                 # 执行记录列表
/quality/executions/:execution_id   # 执行详情（评分卡片、字段评分表、规则明细表）
/quality/issues                     # 问题工单列表（含状态过滤和处理操作）
```

## 模块依赖关系

**依赖**:
- **System 模块**: 认证、PostgreSQL 引擎校验、Execution Authorization 和 ExecutionEngineAccess（`SYSTEM_URL`）
- **Standard 模块**: 获取数据元的质量规则定义（`STANDARD_URL`）
- **Common**: 规则契约、`common.task_executions`、查询方言和数据库连接桥
- **Redis**: 租户/引擎删除时的资源回收事件

**被依赖**:
- **Monitor 模块**: 通过 `common.task_executions` 中 `module='quality'` 的记录统一监控质量检查执行情况

当前不支持事件触发、定时调度、取消、自定义 SQL、跨字段规则和自动映射；需要扩展时先修改正式数据质量规范。

## 配置项

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `QUALITY_BACKEND_PORT` | `8182` | 后端服务端口 |
| `SYSTEM_URL` | `http://localhost:8180` | System 模块地址 |
| `STANDARD_URL` | `http://localhost:8110` | Standard 模块地址 |
| `QUALITY_SERVICE_CLIENT_SECRET` | - | Quality Confidential OAuth Client Secret |
| `REDIS_HOST` / `REDIS_PORT` | - | Redis 连接配置（用于认证缓存） |

## IAM Permission 所有权

Quality 是以下 Permission 的唯一 owner：

- `quality.rule_application.*`
- `quality.check_task.*`
- `quality.issue.*`

机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Quality 服务启动时不向 System 动态注册 Permission。

Quality 的执行历史是 `common.task_executions` 的跨模块统一投影，首批内置 Role 使用 `monitor.execution.read` 读取该类全局执行事实；Quality 不重复定义同义的 `quality.execution.read`。

## 特殊设计

### 规则配置快照

创建 RuleApplication 时，后端从 Standard 拉取严格版本化的 `addp.quality.rules/v1` 文档，只保留启用规则并写入 `rule_config`。触发任务时再次把当前启用 RuleApplication 冻结到 execution 的 `execution_config`；worker 只消费 execution 快照，不回读实时配置。

RuleApplication 只保存 `element_id` 和规则快照，不复制数据元名称或编码。列表 API 使用 Quality 租户服务身份按当前页 ID 集合从 Standard 批量解析 `element: {id, name, code}`；这是当前展示投影，不是历史事实。浏览器不直接调用 Standard 来补全列表，也不保留搜索缓存到裸 ID 的展示旁路。

规则应用与检查任务前端都读取 `active,disabled` PostgreSQL 用于历史绑定名称回显，表格同时显示引擎名称和 ID；创建或更新表单只允许 `active` 引擎，提交前再次校验生命周期。`deleting` 不进入正常展示或选择。

### 问题工单状态流转

```
open（待处理）
    ├─→ resolved（已解决）：数据问题已修复
    └─→ ignored（已忽略）：已知问题，暂不处理
```

同一租户、同一 RuleApplication 始终只有一个当前问题。规则失败时创建或重开为 `open`，后续检查通过时自动变为 `resolved`。人工只能将 `open` 更新为 `resolved` 或 `ignored`，且必须提交处理说明；终态之间不可互转。

RuleApplication 是当前配置，execution metadata 才是历史事实。存在已冻结该规则应用的 `pending|running` execution 时禁止删除；其余删除必须在同一事务中清理对应 Issue，并保留已完成 execution 历史。

规则应用创建使用 System 实时 Catalog 级联选择 schema、table 和 column；表字段通过 Catalog facts 按需读取，后端保存前按当前 Tenant 和 Engine 再次校验三者归属。Catalog 只是创建时选择与校验来源，持久身份仍是 `engine_id + schema_name + table_name + column_name`，不保存第二套 `item_id` 或 ResourceLocator，也不依赖 Meta 扫描快照。

规则应用启停只影响未来 execution；已有 `pending|running` execution 继续消费冻结快照。手动停用不改变已有 Issue，停止检查不等于问题已解决或已忽略。重新启用前必须重新校验绑定 Engine 仍为当前 Tenant 的 active PostgreSQL Engine，停用不依赖 Engine 可用性。更新请求必须显式提供布尔 `enabled`，repository 只更新启用状态与审计字段，不使用整行 `Save`。

failed execution 必须在 `error_details.code` 写数据质量规范定义的稳定领域错误码；原始数据库、SQL、连接和外部服务错误只写服务日志。前端按错误码本地化展示失败原因，不显示持久化的英文安全摘要或内部错误。

### 异步检查，立即返回

`POST /check-tasks/:id/run` 在创建 pending execution 并成功附加 Execution Authorization 后返回 `execution_id`。持久 worker 随后领取执行；前端通过轮询 `GET /executions/:execution_id` 获取状态和 `metadata` 结果。HTTP 请求不启动业务 goroutine。

### 前端双模式布局

`Layout.vue` 通过 `window.self !== window.top` 判断是否在 Console iframe 中运行：
- **Console 嵌入模式**：仅渲染内容区域
- **独立访问模式**：完整 Header + Sidebar + 内容布局

## 开发注意事项

1. **新增功能**: `models` → `repository` → `service` → `handler` → `router.go`

2. **数据库连接**: worker 只能使用 System `ExecutionEngineAccess` 返回的授权引擎事实创建连接，不得直接读取引擎密钥或绕过 Execution Authorization

3. **重启服务**:
   ```bash
   bash scripts/dev/restart.sh -quality
   ```
   修改 common 后需全量重启：
   ```bash
   bash scripts/dev/restart.sh -all
   ```

4. **前端 API 统一入口**: 所有 API 调用集中在 [frontend/src/api/quality.js](frontend/src/api/quality.js)

5. **规则类型扩展**: 先修改 `docs/spec/addp数据质量规范.md` 和 `common/dataquality`，再同步 Standard 编辑器、Quality SQL 编译器、Swagger 和测试；不得保留旧规则结构兼容分支

6. **执行约束**: v1 仅支持 active PostgreSQL 引擎和手动触发；无规则、非法快照、SQL 错误、授权失败都必须进入 failed，不得以空结果成功

## 前端公开路由

- 模块内 Router 使用 `/rule-applications`、`/check-tasks`、`/executions`、`/issues` 等无模块前缀路径；Console 公开 URL 统一加 `/quality` 前缀。
- 执行详情唯一使用 `/executions/:execution_id`，参数名与 Task Execution 领域身份一致，不接受 `id` 别名。
- 规则应用列表使用 `engine_id`、`schema_name`、`table_name`、`page`、`page_size` 恢复筛选和分页，默认值省略。
- 执行记录列表使用 `status`、`page`、`page_size` 恢复筛选和分页；列表进入详情使用 `push` 并保留同名 query，详情返回 `/executions` 使用 `replace` 恢复原列表上下文。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`。
- 检查任务列表使用 `page`、`page_size` 恢复分页，使用 `create=1` 恢复创建弹窗、使用 `task_id` 恢复编辑弹窗；创建和编辑保留分页上下文，默认列表省略 query，TaskProvider `create_url` / `edit_url` 必须使用同一契约。
