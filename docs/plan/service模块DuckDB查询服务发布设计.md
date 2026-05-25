# Service 模块 DuckDB 查询服务发布设计

> 状态：实现已按 `object_table` 配置模型收口
> 日期：2026-05-25
> 范围：Service 模块、common/duckdb、Develop 模块（共享）

---

## 一、背景与目标

ADDP 已能把 MinIO/S3 上的 Parquet、ORC、Avro 等表格资源识别为标准 table item。Service 模块需要把这些对象存储表像关系型表一样发布为 REST Query 服务。

核心目标：

- 不新增 `config_type`，继续使用 `table` 表达“选择一个表发布查询服务”的用户意图。
- 不再引入或暴露 `lake_table` item type。
- 不再使用 `lake_mode` 作为执行依据。
- 对象存储表的执行事实来自 Meta 标准 attributes，尤其是 `storage.physical_path`、`item.format`、`item.layout`。
- DuckDB 只是执行实现，不能反向污染 item 类型和元数据模型。

---

## 二、执行路由

Service 查询服务仍保留两类 `config_type`：

| config_type | 用户意图 | 执行路径 |
| --- | --- | --- |
| `table` | 发布选中的表 | 关系型表走 dbbridge；对象存储表走 DuckDB |
| `sql` | 发布用户编写的 SQL | 关系型 SQL 走 dbbridge；DuckDB SQL 后续按联邦查询路径扩展 |

`table` 模式下是否走 DuckDB 不由 item type 决定，而由服务配置中的 `data_config.object_table` 决定。

```json
{
  "object_table": {
    "physical_path": "bucket/path/orders",
    "format": "parquet",
    "layout": "whole"
  }
}
```

该配置在创建服务时从 Meta item 的标准 attributes 读取：

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
| `attributes.item.format` | `parquet`、`orc`、`avro` |
| `attributes.item.layout` | `single` 或 `whole` |
| `attributes.storage.physical_path` | 必须存在 |

Service 前端只允许选择标准 table 语义资源，提交时把对象存储表所需的执行事实写入 `data_config.object_table`。

---

## 四、DuckDB 共享模块

DuckDB 公共能力放在 `common/duckdb`：

```text
common/duckdb/
├── engine.go        # DuckDB 连接管理、引擎挂载
├── rewriter.go      # SQL 引用改写、对象存储表映射
└── executor.go      # 查询执行、结果集转换
```

当前公共语义：

- `IsObjectTableEngine(engineType)`：判断 MinIO/S3 这类对象存储引擎。
- `IsObjectTableItem(item)`：按标准 attributes 判断对象存储表。
- `BuildObjectTableMap(...)`：基于 Meta 已确认的 `storage.physical_path` 构建 DuckDB rewrite 映射。
- `BuildReadParquetExpr(physicalPath)`：把物理路径转为 DuckDB `read_parquet(...)` 表达式。

不再提供基于 `schema/table/lake_mode` 猜路径的工具函数。

---

## 五、Service 发布流程

### 5.1 对象存储表服务发布

```text
用户操作：
1. 选择 MinIO/S3 存储引擎
2. 浏览可发布的 table 资源
3. 选择目标对象存储表
4. 填写服务名称、访问控制等基本信息
5. 提交

后台处理：
1. 从 Meta item attributes 读取 physical_path、format、layout、fields
2. 写入 data_config.object_table
3. 创建 query_services 记录，config_type='table'
4. 查询执行时通过 DuckDB read_parquet(...) 读取数据
```

### 5.2 关系型表服务发布

关系型表仍走既有 dbbridge 路径。它不包含 `data_config.object_table`。

---

## 六、Develop 联邦查询

Develop 的 DuckDB 数据源列表同样不暴露 `lake_table`：

- 对象存储引擎下，通过 `duckdb.IsObjectTableItem` 枚举对象存储表。
- 返回的 `TableRef.ItemType` 始终为 `table`。
- 通过 `physical_path`、`format`、`layout` 表示表的来源和读取方式。
- 样例 SQL 优先选择带 `physical_path` 的对象存储表。

Swagger 对外描述统一为“对象存储表 + 关系型表”。

---

## 七、验收标准

- 业务代码不再判断 `item_type == "lake_table"`。
- 业务代码不再使用 `lake_mode`。
- Service 前端不再暴露 `lake_table` 可选节点类型。
- Develop Swagger 不再出现 `"table" 或 "lake_table"`。
- Service 查询服务基于 `data_config.object_table.physical_path` 执行 DuckDB 查询。
- Develop 联邦查询基于 `BuildObjectTableMap` 进行 SQL rewrite。
- 针对性测试与构建通过：
  - `go test ./common/duckdb ./develop/backend/internal/... ./service/backend/internal/...`
  - `npm run build --prefix develop/frontend`
  - `npm run build --prefix service/frontend`
