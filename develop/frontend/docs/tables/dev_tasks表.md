# dev_tasks 表结构和 API 说明

[返回数据库架构](../数据库架构.md) · [返回模块入口](../../../CLAUDE.md#数据库文档)

阅读导航：[字段与索引](#二表结构定义)、[Content](#23-推荐的-content-结构)、[Engine 配置](#24-推荐的-executionconfig-结构)、[快照与并发缺口](#25-定义覆盖执行快照与并发缺口)、[类型映射](#三devtype-与-taskprovider-task_type-边界)、[用户 API](#四api-端点说明)。

## 一、表结构概览

`develop.dev_tasks` 表是 Develop 模块的开发任务定义表，统一存储 SQL 查询、工作流、脚本等开发任务。Notebook 是 `script` 任务的当前实现形态，通过 `content.notebook_path` 标识。

---

## 二、表结构定义

### 2.1 核心字段

以下按当前 [DevTask 模型](../../../backend/internal/models/dev_task.go)列出类型及 GORM 声明；数值类型保留 Go 类型，实际 PostgreSQL 列类型、默认值和索引需核对目标数据库。

| 字段名 | 模型类型 | 声明 | 说明 |
| --- | --- | --- | --- |
| `id` | `uint` | 主键 | 开发任务 ID |
| `tenant_id` | `uint` | NOT NULL，联合索引 | 租户 ID |
| `name` | `string` | 长度 255，NOT NULL | 租户内名称；服务端先检查重名，模型未声明唯一索引 |
| `display_name` | `string` | 长度 255 | 显示名称（前端优先显示） |
| `dev_type` | `string` | 长度 50，NOT NULL，联合索引 | Develop 内部类型，见[类型映射](#三devtype-与-taskprovider-task_type-边界) |
| `content` | `DevTaskContent` | JSONB，NOT NULL | 各开发类型的定义，见 2.3 |
| `execution_config` | `DevTaskContent` | JSONB | 运行 Engine 等配置，见 2.4 |
| `editor_layout` | `JSONMap` | JSONB，NOT NULL，默认 `{}` | DAG 坐标和视口，不参与执行 |
| `timeout` | `int` | 默认 300 | 定义中的秒数；查询有效预算以[执行策略快照](../../../../docs/spec/configuration/执行与模块策略.md#develop-执行快照与部署边界)为准 |
| `description` | `string` | TEXT | 描述信息 |
| `tags` | `pq.StringArray` | TEXT[] | 标签数组 |
| `created_by` | `*uint` | 可空 | 创建者 ID |
| `updated_by` | `*uint` | 可空 | 更新者 ID |
| `created_at` | `time.Time` | GORM 创建时间 | 创建时间 |
| `updated_at` | `time.Time` | GORM 更新时间 | 更新时间，不是客户端并发版本 |
| `deleted_at` | `gorm.DeletedAt` | 普通索引 | NULL 表示未删除 |
| `status` | `string` | 长度 50，默认 `active`，索引 | `active` / `inactive` / `archived` |
| `last_execution_id` | `*string` | 长度 36，可空 | 软引用统一 execution UUID |
| `last_execution_status` | `string` | 长度 50 | 最近执行摘要，不是完整执行历史 |
| `last_run_at` | `*time.Time` | 可空 | 最近运行时间 |

详情响应中的 `execution_contract` 由当前任务与能力生成，模型以 `gorm:"-"` 标记，不是持久列；执行预览与运行事实归 `common.task_executions`，读取规则见[专业结果边界](../../../CLAUDE.md#专业执行结果读取)。

### 2.2 数据库索引

| 当前模型声明 | 字段 | 用途 |
| --- | --- | --- |
| `idx_dev_tasks_tenant_type` | `tenant_id, dev_type` | 租户与类型过滤 |
| `idx_dev_tasks_status` | `status` | 状态过滤 |
| `DeletedAt` 的未命名 `index` 标签 | `deleted_at` | 软删除查询，名称由 GORM 命名策略生成 |

启动通过 [InitDatabase](../../../backend/internal/repository/database.go) 调用 `AutoMigrate(&models.DevTask{}, ...)`。表中只列当前模型声明；实库索引、历史残留及唯一性须专项核验，不能从服务端重名预查推定数据库唯一约束。

### 2.3 推荐的 Content 结构

#### Query 类型（dev_type='query'）

```json
{
  "query_type": "sql",
  "query": "SELECT * FROM cities WHERE population > 1000000"
}
```

查询定义、`query_parameters[]`、关系参数和角色选择字段以[Develop 任务契约](../../../../docs/spec/tasks/模块任务契约.md#develop-开发任务)与[TaskProvider 参数契约](../../../../docs/spec/tasks/TaskProvider接口.md#标准响应体)为准。结果预览上限由[执行策略](../../../../docs/spec/configuration/执行与模块策略.md#develop-执行快照与部署边界)冻结，不在 `content` 保存 `limit`。

#### Workflow 类型（dev_type='workflow'）

```json
{
  "workflow_definition": {
    "tasks": [
      {
        "id": "load_1",
        "operator": "load",
        "params": {
          "locator": "addp://engine/1/path/public/cities?type=table"
        },
        "depends_on": []
      },
      {
        "id": "save_1",
        "operator": "save",
        "params": {
          "target_parent_locator": "addp://engine/1/path/public?type=schema",
          "target_name": "cities_result"
        },
        "depends_on": ["load_1"]
      }
    ]
  },
  "inputs": {
    "source_locator": "addp://engine/1/path/public/cities?type=table"
  }
}
```

`workflow_definition.tasks[]` 必须遵循工作流计算引擎接口规范：每个任务显式包含 `id`、`operator`、`params`、`depends_on`，无依赖时 `depends_on` 写空数组 `[]`。

`locator`、`target_parent_locator` 等标准 ResourceLocator 是存储 Engine 绑定，`execution_config.engine_id` 是工作流 Runtime；删除后的显式重绑、字段清理和原子替换见[重绑定 API](#45-put-apiv1developtask-definitionsidstorage-engine-bindingssource_engine_id---重绑定存储-engine)。实际算子与公开参数通过[算子发现服务](../../../CLAUDE.md#算子发现服务)确定。

工作流编辑器布局单独保存在顶层 `editor_layout`：

```json
{
  "nodes": {
    "load_1": { "x": 120, "y": 240 }
  },
  "viewport": {
    "zoom": 1,
    "translate_x": 0,
    "translate_y": 0
  }
}
```

`editor_layout` 是纯展示状态。TaskProvider 执行器只读取 `content.workflow_definition` 和 `execution_config`，不得读取 `editor_layout`。

### 2.4 推荐的 ExecutionConfig 结构

普通 SQL 查询任务：

```json
{
  "engine_id": 1
}
```

DuckDB 联邦查询任务：

```json
{
  "engine_id": 2
}
```

DuckDB 是 System 中 `tenant_id=NULL`、`is_builtin=true` 的真实联邦查询计算引擎。查询目标发现统一通过 `/api/v1/develop/engines` 返回真实 Engine，不存在虚拟 `engine_id=0`、空 Engine 或独立查询模式。

`execution_config` 保存 Runtime Engine ID，不保存 Source Engine ID 列表、连接信息或 Token。来源解析与每次执行的授权见[查询工作台](../../../CLAUDE.md#查询工作台与预检)和[执行授权与效果](../../../CLAUDE.md#执行授权与效果)。

### 2.5 定义覆盖、执行快照与并发缺口

#### 当前定义与执行快照

`dev_tasks` 保存任务的当前定义。普通更新提交非空 `content` 时整体替换该 JSONB 内容，不为每次编辑生成独立定义修订；任务模型和更新 DTO 均没有任务级 `metadata` 字段，不能按旧建议在此前端自建版本。

每次执行把内容、执行配置、超时和有效参数冻结到自己的 `common.task_executions.execution_config`；监督器从该 execution 还原运行输入，不重新读取可变任务定义。该快照固定的是本次输入，不是任务定义的版次或恢复接口，也不替代当前执行授权复核；队列、租约、授权与 Notebook 当前限制沿[执行监督契约](../../../../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。

#### 正式并发要求与当前缺口

可变主资源的服务端正整数 `version`、事务校验／递增、`409/resource_version_conflict` 和前端保留草稿要求，以[资源版本与并发更新规范](../../../../docs/spec/api/资源版本与并发更新.md#资源版本与并发更新)为准。并发版本不代表业务版次或历史定义修订。

当前 `DevTask`、更新 DTO 和普通任务更新没有接入该 `version` 契约，普通更新仍调用无版本条件的 `Save`。工作流存储 Engine 重绑只使用本次服务端读取的 `updated_at` 做条件更新，冲突返回其既有 HTTP 409；它只保护该次读取至写入之间的窗口，不校验客户端编辑基线，也不能代表普通更新或全部任务写入已符合版本规范。此处记录实现缺口，不建立规范例外或前端替代路线。

#### 验证与证据边界

标准门禁与分版本结果见[Develop 验证入口](../../../../docs/spec/configuration/执行与模块策略.md#develop-验证入口与证据边界)。执行快照、重绑局部 CAS 或现有门禁通过，不得计为旧版本 409、冲突无副作用、版本迁移或前端草稿保留已验收；这些缺口按正式规范分别验证，不将本节事实说明作为实现已完成的证据。

---

## 三、DevType 与 TaskProvider task_type 边界

`dev_type` 是 Develop 私有表字段，只用于 Develop 内部任务定义管理、查询过滤和执行分派。TaskProvider 标准接口、Orchestrator 编排引用、Monitor 回查和 `common.task_executions` 使用统一字段 `task_type`。Develop 对外暴露的 `task_type` 与内部 `dev_type` 一一映射，但外部模块不得直接依赖 `develop.dev_tasks.dev_type`。

| 内部 `dev_type` / 对外 `task_type` | 定义内容归属 |
| --- | --- |
| `query` | `content.query_type`、`content.query`，见[Query 结构](#query-类型dev_typequery) |
| `workflow` | `content.workflow_definition` 和可选 `content.inputs`，见[Workflow 结构](#workflow-类型dev_typeworkflow) |
| `script` | 当前 Notebook 形态使用 `content.notebook_path`，运行时绑定见[文件与运行时重绑](../../../CLAUDE.md#文件与运行时重绑) |

类型声明不表示一次性 Notebook 已可派发；当前领取拒绝、内容摘要及交互／排队授权的区别见[排队执行与内容摘要](../../../CLAUDE.md#排队执行与内容摘要)，正式要求沿[执行监督契约](../../../../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。

---

## 四、API 端点说明

本节为用户任务定义入口，认证与执行授权沿[模块授权规则](../../../CLAUDE.md#执行授权与效果)；服务间 TaskProvider 路由及其专用身份、Permission 另见[数据库架构](../数据库架构.md#taskprovider)。任务最近执行摘要不授予本人专业结果读取权限。

### 4.1 POST /api/v1/develop/task-definitions - 创建开发任务

要求 `develop.task.create`；保存角色导出方式时还需按现行规则复核相关资格与条件权限。

**请求体**：

```json
{
  "name": "查询大城市",
  "display_name": "查询人口大于100万的城市",
  "dev_type": "query",
  "content": {
    "query_type": "sql",
    "query": "SELECT * FROM cities WHERE population > 1000000"
  },
  "execution_config": {
    "engine_id": 1
  },
  "description": "查询所有大城市",
  "tags": ["城市", "人口"]
}
```

**响应**（200 OK）：返回完整 DevTask 对象

---

### 4.2 GET /api/v1/develop/task-definitions - 查询开发任务列表

要求 `develop.task.read`。**查询参数**：

- `page`：从 1 开始的页码
- `page_size`：每页数量（1–100）
- `dev_type`：按类型过滤
- `status`：按状态过滤
- `tag`：按标签过滤
- `keyword`：搜索名称或描述

返回 `{items,total,page,page_size}`；当前请求 DTO 没有 `engine_id` 筛选字段，不能据 Swagger 参数列表推定已经生效。

---

### 4.3 POST /api/v1/develop/task-definitions/{id}/execute - 执行开发任务

要求 `develop.task.execute`，执行效果、数据权限和 Notebook 专项权限仍由服务端独立核验。可选请求体 `parameters` 只覆盖本次执行；默认值和必填校验见[参数契约](../../../../docs/spec/tasks/TaskProvider接口.md#标准响应体)，不改写任务定义。

**响应**（202 Accepted，中文提示示例）：

```json
{
  "message": "执行已启动",
  "execution_id": "uuid-xxxx"
}
```

HTTP 受理提示不代表已经领取或执行成功；durable pending、lease 和终态沿[执行监督契约](../../../../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。实际状态从 `GET /api/v1/develop/executions/{execution_id}` 回查，结果读取还须满足[本人专业读取边界](../../../CLAUDE.md#专业执行结果读取)。

### 4.4 GET /api/v1/develop/task-definitions/{id}/storage-engine-bindings - 查询存储 Engine 绑定

要求 `develop.task.read`。返回任务当前 ResourceLocator 引用的 Engine、引用数量、资源类型、可用状态及候选，只读观察，不改变任务绑定。

### 4.5 PUT /api/v1/develop/task-definitions/{id}/storage-engine-bindings/{source_engine_id} - 重绑定存储 Engine

要求 `develop.task.update`。Engine 删除后保留任务与旧 Locator，继续执行前须显式选择替代 Engine。请求体：

```json
{
  "target_engine_id": 15
}
```

Develop 校验任务归属、目标 Engine 生命周期和存储能力，并以目标 Engine 的 `storage.catalog_model` 校验全部 Locator type 的路径语义兼容性后，一次原子替换任务中指向 `source_engine_id` 的全部标准 ResourceLocator：只替换 Engine ID、保留 path/type，并清除旧 Meta 的 `node_id/item_id`；不改写 Runtime `execution_config.engine_id`。响应返回更新后的任务和替换数量；本次服务端读取至写入之间发生并发修改时返回 `409 Conflict`。其时间戳 CAS 与正式 `version` 的差异见[当前并发缺口](#正式并发要求与当前缺口)，不能据此认定通用任务版本契约已接通。

---

## 五、相关文档

- [数据库架构](../数据库架构.md) - 表归属、软引用与 TaskProvider 入口
- [模块架构](../../../CLAUDE.md#关键架构) - 查询保护、固定目标写入、全量导出与执行监督
- [标准验证与历史证据](../../../CLAUDE.md#开发与验证) - 门禁职责、固定版本、失败与修复、清理及未覆盖项
