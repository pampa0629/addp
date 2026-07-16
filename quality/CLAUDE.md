# Quality 模块 CLAUDE.md

本文件为 Claude Code 在 `quality/` 目录下工作时提供指导。

## 模块概述

**Quality 模块** 是 ADDP 平台的数据质量管理中心，负责：

- 规则应用管理（RuleApplication）：将 Standard 模块定义的数据元质量规则，映射到具体数据库的表字段
- 检查任务管理（CheckTask）：定义和执行数据质量检查任务，可指定引擎、Schema、表的检查范围
- 质量检查执行：通过 SQL 生成引擎将质量规则转换为 SQL 查询，异步执行并计算表级/字段级质量评分
- 问题工单管理（Issue）：对检查失败的规则自动生成问题工单，支持状态流转（待处理 → 已解决/已忽略）
- 执行记录查询：读取 `common.task_executions` 查看历史执行记录及详细结果

**端口**:
- 后端: `8182`（环境变量 `QUALITY_BACKEND_PORT`）
- 前端: `5183`（开发环境）/ `8113`（Docker 环境）

**数据库 Schema**: `quality`

## 目录结构

```
quality/
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
│           ├── check_executor.go             # 质量检查执行引擎（异步）
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
| schema_name | string | 可选：限定检查的 Schema |
| table_name | string | 可选：限定检查的表（空则检查整个 Schema） |
| enabled | bool | 是否启用 |
| last_run_at | timestamp? | 最近执行时间 |
| next_run_at | timestamp? | 下一次计划执行时间（当前未启用调度，保持为空） |
| last_execution_id | string | 最近一次 `common.task_executions.execution_id` |
| last_execution_status | string | 最近一次执行状态 |
| created_by / updated_by | int64 | 操作人 |

### `quality.issues` — 质量问题工单

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 主键 |
| tenant_id | int64 | 租户 ID |
| execution_id | string | 关联的执行记录（`common.task_executions.execution_id`） |
| rule_application_id | int64 | 关联的规则应用 |
| rule_type | string | 规则类型（not_null/unique/format/length/value_range/allowed_values） |
| column_name / table_name / schema_name | string | 问题字段定位 |
| engine_id | int64 | 所属引擎 |
| failed_count / total_count | int64 | 失败行数 / 总行数 |
| pass_rate | float64 | 通过率（0-100） |
| detail | JSONB | 详情（含错误信息等） |
| status | string | `open` / `resolved` / `ignored` |

> **说明**: 执行记录不在 quality schema，写入 `common.task_executions`，module 标记为 `quality`，执行结果（质量评分、规则明细等）存储在 `metadata` JSONB 字段中。

## API 端点（`/api/v1/quality`）

### 规则应用
```
GET    /api/v1/quality/rule-applications          # 列表（支持过滤：engine_id, schema_name, table_name）
POST   /api/v1/quality/rule-applications          # 创建（传入 element_id，后端自动获取质量规则快照）
GET    /api/v1/quality/rule-applications/:id      # 详情
PUT    /api/v1/quality/rule-applications/:id      # 更新
DELETE /api/v1/quality/rule-applications/:id      # 删除
```

### 检查任务
```
GET    /api/v1/quality/check-tasks                # 列表
POST   /api/v1/quality/check-tasks                # 创建
GET    /api/v1/quality/check-tasks/:id            # 详情
PUT    /api/v1/quality/check-tasks/:id            # 更新
DELETE /api/v1/quality/check-tasks/:id            # 删除
POST   /api/v1/quality/check-tasks/:id/run        # 手动触发执行（异步，立即返回 execution_id）
```

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
在任务定义行锁保护下原子创建 common.task_executions（status: pending，started_at 为空）
    ↓
异步 goroutine 执行（doCheck）：
    1. 原子推进 execution 与 check_tasks 最近执行摘要为 running，并写真实 started_at
    2. 加载该任务关联的所有 rule_applications
    3. 对每条规则调用 SQLGenerator 生成检查 SQL
    4. 通过 dbbridge 获取目标引擎连接
    5. 执行 SQL，计算 failedCount 和 totalCount
    6. 汇总字段级评分和表级综合质量评分
    7. 为失败规则创建 Issue 工单
    ↓
在同一事务更新 common.task_executions 终态和
check_tasks.last_run_at、last_execution_id、last_execution_status
```

### SQL 生成器（`sql_generator.go`）

支持 6 种基础规则类型：

| 规则类型 | 校验逻辑 | 失败 SQL 逻辑 |
|----------|---------|-------------|
| `not_null` | 非空检查 | `COUNT(*) - COUNT(column)` |
| `unique` | 唯一性检查 | `COUNT(*) - COUNT(DISTINCT column)` |
| `format` | 格式校验（正则） | `column IS NOT NULL AND column !~ 'pattern'` |
| `length` | 长度范围 | `LENGTH(column) NOT BETWEEN min AND max` |
| `value_range` | 数值范围 | `column NOT BETWEEN min AND max` |
| `allowed_values` | 枚举值 | `column NOT IN (values)` |

### 执行结果 JSON 结构

```json
{
  "quality_score": 86.67,
  "total_rules": 15,
  "passed_rules": 13,
  "failed_rules": 2,
  "field_scores": [
    { "column": "mobile_phone", "score": 95.5, "passed": 3, "failed": 1 }
  ],
  "rule_details": [
    {
      "rule_application_id": 123,
      "rule_type": "format",
      "column": "mobile_phone",
      "table": "users",
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
- **System 模块**: JWT 认证、引擎配置查询（`SYSTEM_URL`）
- **Standard 模块**: 获取数据元的质量规则定义（`STANDARD_URL`）
- **Common**: `common.task_executions`（统一执行记录）、`dbbridge`（数据库连接桥）
- **Redis**: 认证缓存（`CachedSystemAuthMiddleware`，5 分钟 TTL）

**被依赖**:
- **Monitor 模块**: 通过 `common.task_executions` 中 `module='quality'` 的记录统一监控质量检查执行情况

**未来规划**:
- **Meta 模块**: 元数据扫描完成后触发质量检查（事件驱动）
- **Model 模块**: 质量评分反馈，完善数据模型的质量 SLA

## 配置项

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `QUALITY_BACKEND_PORT` | `8182` | 后端服务端口 |
| `SYSTEM_URL` | `http://localhost:8180` | System 模块地址 |
| `STANDARD_URL` | `http://localhost:8110` | Standard 模块地址 |
| `META_URL` | `http://localhost:8082` | Meta 模块地址 |
| `INTERNAL_API_KEY` | - | 内部服务调用密钥 |
| `ENABLE_SERVICE_INTEGRATION` | `true` | 是否启用跨模块服务集成 |
| `REDIS_HOST` / `REDIS_PORT` | - | Redis 连接配置（用于认证缓存） |

## 特殊设计

### 规则配置快照

创建 RuleApplication 时，后端从 Standard 模块拉取该数据元的最新 `quality_rules`，以 JSONB 格式快照保存到 `rule_config` 字段。这样即使 Standard 中的规则后续变更，也不影响已有规则应用的检查行为，保证历史可追溯。

### 问题工单状态流转

```
open（待处理）
    ├─→ resolved（已解决）：数据问题已修复
    └─→ ignored（已忽略）：已知问题，暂不处理
```

仅 `open` 状态的工单可以更新状态；`resolved` 和 `ignored` 状态不可互转。

### 异步检查，立即返回

`POST /check-tasks/:id/run` 立即返回 `execution_id`，实际检查在后台异步执行。前端可通过轮询 `GET /executions/:execution_id` 获取结果。

### 前端双模式布局

`Layout.vue` 通过 `window.self !== window.top` 判断是否在 Console iframe 中运行：
- **Console 嵌入模式**：仅渲染内容区域
- **独立访问模式**：完整 Header + Sidebar + 内容布局

## 开发注意事项

1. **新增功能**: `models` → `repository` → `service` → `handler` → `router.go`

2. **数据库连接**: 检查执行时使用 `common/dbbridge` 获取目标引擎的动态连接，而非 Quality 自身的 PostgreSQL 连接

3. **重启服务**:
   ```bash
   bash scripts/dev/restart.sh -quality
   ```
   修改 common 后需全量重启：
   ```bash
   bash scripts/dev/restart.sh -all
   ```

4. **前端 API 统一入口**: 所有 API 调用集中在 [frontend/src/api/quality.js](frontend/src/api/quality.js)

5. **规则类型扩展**: 新增规则类型需同步修改 `sql_generator.go` 中的 `GenerateCheckSQL()` 方法，以及前端的规则类型枚举和展示逻辑

6. **当前 MVP 状态**: 暂未实现定时调度（Cron）和事件触发，仅支持手动执行；趋势分析、报告导出等功能规划在 Phase 2
