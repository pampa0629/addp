# Service 模块 DuckDB 查询服务发布设计

> 状态：已收口为独立 DuckDB Query Runtime
> 更新日期：2026-08-03
> 范围：Service、Develop、`engines/duckdb` 与共享引擎插件契约

---

## 一、背景与目标

ADDP 已能把 MinIO/S3 上的 Parquet 表格资源识别为标准 table 语义资源。Service 模块需要把这些对象存储表像关系型表一样发布为 REST Query 服务。

核心目标：

- 不新增 `config_type`，继续使用 `table` 表达“选择一个表发布查询服务”的用户意图。
- 不再引入或暴露 `lake_table` item type。
- 不再使用 `lake_mode` 作为执行依据。
- 对象存储表的执行事实来自 Meta 标准 attributes，尤其是 `storage.physical_path`、`item.format`、`item.layout`。
- DuckDB 是独立 Query Runtime，不能反向污染 item 类型和元数据模型。

---

## 二、执行路由

Service 查询服务仍保留两类 `config_type`：

| config_type | 用户意图 | 执行路径 |
| --- | --- | --- |
| `table` | 发布选中的表 | 关系型表通过 `engine_id` 走 dbbridge；Parquet 对象表通过 `engine_id + runtime_engine_id` 走 DuckDB Runtime |
| `sql` | 发布用户编写的 SQL | 单引擎 SQL 使用 `engine_id`；联邦 SQL 使用 `runtime_engine_id`，Source Engine 在发布时冻结 |

`table` 模式下是否走 DuckDB 不由 item type 决定，而由发布快照中的对象表执行描述符决定。对象表服务同时保存 Source Engine 和 DuckDB Runtime，不能通过字段为空或 SQL 内容推断执行路径。

```json
{
  "source_snapshot": {
    "object_table": {
      "physical_path": "bucket/path/orders",
      "format": "parquet",
      "layout": "whole"
    },
    "federated_source_engine_ids": [9],
    "federated_object_tables": {
      "Business_MinIO": {
        "orders": "bucket/path/orders"
      }
    }
  }
}
```

该快照在发布服务时从 Meta item 的标准 attributes 读取并冻结：

- `attributes.storage.physical_path`
- `attributes.item.format`
- `attributes.item.layout`
- `attributes.item.data_type`

如果这些 attributes 缺失，应修复扫描并重扫，不为旧数据保留兼容分支。

---

## 三、对象存储表识别原则

对象存储表不是独立 item type。它是以下事实的组合：

| 事实 | 要求 |
| --- | --- |
| `meta_item.item_type` | `object` 或 `file` |
| `attributes.item.data_type` | `table` |
| `attributes.item.format` | 当前 DuckDB Runtime 只接受 `parquet` |
| `attributes.item.layout` | `single` 或 `whole` |
| `attributes.storage.physical_path` | 必须存在 |

Service 前端只允许选择标准 table 语义资源，后端在发布时把对象表执行描述符、Source Engine ID 和对象表映射写入唯一的 `source_snapshot`。

---

## 四、独立 DuckDB 计算引擎

DuckDB 原生依赖与执行实现只存在于 `engines/duckdb`：

```text
engines/duckdb/
├── internal/duckdb/   # DuckDB 会话、挂载、改写和扩展管理
├── internal/runtime/  # Execution Authorization 消费与查询执行
└── internal/api/      # 受控 HTTP Runtime API
```

共享层只保留稳定契约与 HTTP Provider：

- `common/engine/plugin` 定义 `FederatedQueryRuntimeProvider` 和请求/结果契约。
- `common/engine/plugins/duckdb` 只负责通过 HTTP 调用真实 Runtime。
- Develop、Service 不链接 DuckDB 原生库，也不维护第二套执行实现。
- Runtime 固定预装 `httpfs`、`postgres_scanner`、`mysql_scanner`、`spatial`；请求期禁止下载扩展。

不再提供基于 `schema/table/lake_mode` 猜路径的工具函数。

---

## 五、Service 发布流程

### 5.1 对象存储表服务发布

```text
用户操作：
1. 选择 MinIO/S3 存储引擎
2. 浏览并选择可发布的 Parquet table 资源
3. 选择真实 DuckDB Runtime Engine
4. 填写服务名称、访问控制等基本信息
5. 提交并发布

后台处理：
1. 从 Meta item attributes 读取 physical_path、format、layout、fields
2. 写入 `source_snapshot.object_table`，冻结 Source Engine ID 与对象表映射
3. 创建 `query_services` 记录，`config_type='table'`，同时保存 `engine_id` 与 `runtime_engine_id`
4. 每次执行由 Service 签发 Execution Authorization，DuckDB Runtime 消费授权后读取 Parquet
```

### 5.2 关系型表服务发布

关系型表仍走既有 dbbridge 路径。它的 `source_snapshot` 不包含 `object_table`。

---

## 六、Develop 联邦查询

Develop 的 DuckDB 数据源列表同样不暴露 `lake_table`：

- 对象存储引擎下，通过实时 Catalog 和标准 attributes 枚举 Runtime 确实支持的 Parquet 对象表。
- 返回的 `TableRef.ItemType` 始终为 `table`。
- 通过 `physical_path`、`format`、`layout` 表示表的来源和读取方式。
- 样例 SQL 来自当前 Source Engine 的真实数据；候选必须由 DuckDB Runtime 实际返回非空结果后才可展示。
- 查询任务统一通过 `execution_config.engine_id` 绑定真实 DuckDB Runtime Engine。

Swagger 对外描述统一为“对象存储表 + 关系型表”。

---

## 七、验收标准

- 业务代码不再判断 `item_type == "lake_table"`。
- 业务代码不再使用 `lake_mode`。
- Service 前端不再暴露 `lake_table` 可选节点类型。
- Develop Swagger 不再出现 `"table" 或 "lake_table"`。
- Service 发布快照冻结 Source Engine ID 与对象表映射，执行时不重新按名称绑定。
- Develop、Service 都只通过 `FederatedQueryRuntimeProvider` 调用 `engines/duckdb`。
- Service SQL 发布表单按 Engine capability 发现普通 SQL Engine 与联邦 Query Runtime；每个样例都从当前业务 Catalog 构造，并使用用户来源 Execution Authorization 真实执行返回非空后展示。
- DuckDB Runtime 镜像构建阶段预下载扩展，请求处理阶段不联网安装。
- 针对性测试与构建通过：
  - `cd engines/duckdb && GOWORK=off go test ./...`
  - `cd common && go test ./engine/plugin ./engine/plugins/duckdb ./dbbridge`
  - `cd develop/backend && go test ./internal/service ./internal/api ./cmd/server`
  - `cd service/backend && go test ./internal/service ./internal/api ./cmd/server`
  - `npm run build --prefix develop/frontend`
  - `npm run build --prefix service/frontend`
