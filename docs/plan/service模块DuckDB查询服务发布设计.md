# Service 模块 DuckDB 查询服务发布设计

> 状态：设计确认
> 日期：2026-04-18
> 范围：Service 模块、common/duckdb、Develop 模块（共享）

---

## 一、背景与目标

湖仓一体化第一、二阶段完成后，ADDP 平台已能识别和管理 MinIO/S3 上的 Parquet 表格资源（table），并在 Develop 模块的 SQL 工作台中支持 DuckDB 联邦查询。

本设计的目标是：**将湖表查询能力融入 Service 模块现有的查询服务发布框架**，让用户能够像发布关系型表服务一样，将湖表发布为对外可访问的标准化数据服务（REST API）。

**核心原则**：与现有查询服务框架融为一体，非必要不单独建立新框架。

---

## 二、现有框架回顾

Service 模块的查询服务（`service.query_services` 表）已支持两种 `config_type`：

| config_type | 数据源 | 执行引擎 | 说明 |
|---|---|---|---|
| `table` | 关系型表 | dbbridge 直连 | 用户选引擎 → 选 schema → 选 table |
| `sql` | 关系型表（SQL） | dbbridge 直连 | 用户选引擎 → 写 SQL |

查询执行器（`query_executor_service.go`）通过 `engine_id` 获取连接信息，经 `dbbridge` 连接池执行 SQL。

---

## 三、设计决策与选型思路

### 3.1 是否新增 config_type？

**讨论过程**：

最初考虑新增 `lake`、`duckdb` 等 config_type，以区分湖表查询和 DuckDB SQL 查询。但深入讨论后发现：

- `lake` 模式和 `table` 模式的**用户操作完全一致**（选引擎 → 选表），只是选的表类型不同
- `sql` 模式和 DuckDB SQL 模式的**用户操作完全一致**（选引擎 → 写 SQL），只是后台执行引擎不同
- `config_type` 应该表达**用户意图**（选表 vs 写 SQL），而不是技术实现细节

**结论**：`config_type` 保持 `table` 和 `sql` 不变，不新增类型。执行引擎由 `engine_id` 对应的 `engine_type` 在运行时自动决定。

### 3.2 执行引擎如何路由？

**`table` 模式**：
- `engine_type` 为 `minio`/`s3` → 走 DuckDB 执行路径（湖表）
- `engine_type` 为 `postgresql`/`mysql` 等 → 走 dbbridge 执行路径（关系型表）

**`sql` 模式**：
- `engine_type` 为 `duckdb`（虚拟引擎）→ 走 DuckDB 联邦查询执行路径
- `engine_type` 为关系型 → 走 dbbridge 执行路径

### 3.3 湖表的 schema_name 和 table_name 如何设定？

**问题**：MinIO/S3 上的湖表有两种模式（来自湖仓一体化设计）：
- **模式 A（目录即表）**：`bucket/warehouse/orders/part-001.parquet` → 逻辑表 `orders`
- **模式 B（文件即表）**：`bucket/exports/orders.parquet` → 逻辑表 `orders`

对关系型数据库，`engine_id + schema + table` 能唯一确定并打开数据。湖表也应达到同等精度。

**命名约定**（参照数据库的 schema.table 逻辑）：

| 字段 | 含义 | 模式 A 示例 | 模式 B 示例 |
|---|---|---|---|
| `engine_id` | MinIO/S3 引擎（含 bucket 信息） | engine=1 | engine=1 |
| `schema_name` | bucket 与 table_name 之间的中间路径 | `warehouse` | `exports` |
| `table_name` | DuckDB 最终打开的目标名称 | `orders`（目录名） | `orders`（文件名，无扩展名） |

**路径重建**：
- 模式 A：`s3://bucket/{schema_name}/{table_name}/*.parquet`
- 模式 B：`s3://bucket/{schema_name}/{table_name}.parquet`
- schema_name 为空时：`s3://bucket/{table_name}/` 或 `s3://bucket/{table_name}.parquet`

**解决重名问题**：同一 bucket 内不同子目录下的同名表，通过 `schema_name` 区分，`schema_name + table_name` 在 bucket 内唯一。

### 3.4 模式 A 和模式 B 如何区分？

**问题**：`schema_name="exports"`, `table_name="orders"` 仍有歧义——可能是目录 `exports/orders/`，也可能是文件 `exports/orders.parquet`。

**讨论过的方案**：
1. 新增 `config_type = 'lake_dir'` / `'lake_file'`：语义清晰，但违背"config_type 不表达技术细节"的原则
2. 存入 `data_config.lake_mode`：`data_config` 本来就随记录加载，存放路径格式细节语义自然

**结论**：在 `data_config` 中新增 `lake_mode` 字段（`"directory"` 或 `"file"`），由创建服务时从 Meta 的 `Attributes.mode` 自动读取并写入。`config_type` 保持简洁，不被技术细节干扰。

### 3.5 DuckDB 代码如何共享？

Develop 模块已有完整的 DuckDB 实现（`duckdb_service.go`、`sql_rewriter.go`）。

**方案对比**：
- **跨模块 HTTP 调用**：Service 调用 Develop 的 DuckDB API → 增加服务间依赖，延迟高，不适合服务发布场景
- **提取到 common/duckdb/**：DuckDB 挂载逻辑提取为共享库，Service 和 Develop 各自引入 → 符合 DRY 原则，无跨模块依赖

**结论**：将 DuckDB 核心逻辑（引擎挂载、SQL 改写）提取到 `common/duckdb/`，Service 模块直接引入。DuckDB 是嵌入式库（CGO），两个模块各自编译，互不影响。

### 3.6 空间数据支持

湖表（Parquet）中可能包含 WKT/WKB 格式的空间字段（如 Transfer 模块写出的带空间信息的 Parquet）。

**结论**：
- 第一阶段：仅支持 REST Query（JSON/CSV），不支持 OGC Features
- 第三阶段：检测 `data_config.fields` 中的空间字段，启用 OGC Features 协议

---

## 四、架构设计

### 4.1 整体执行流程

```
GET /api/query/{serviceName}
    ↓
QueryHandler.QueryData()
    ↓
加载 query_services 记录
    ↓
获取 engine 信息（engine_type）
    ↓
config_type = 'table'?
    ├─ engine_type = minio/s3
    │     读 data_config.lake_mode
    │     重建 S3 路径
    │     → DuckDB 执行
    └─ engine_type = 关系型
          schema.table
          → dbbridge 执行

config_type = 'sql'?
    ├─ engine_type = duckdb
    │     SQL 改写（三段式 → read_parquet）
    │     → DuckDB 联邦查询执行
    └─ engine_type = 关系型
          直接执行 sql_query
          → dbbridge 执行
```

### 4.2 data_config 扩展

湖表服务的 `data_config` 在现有结构基础上新增 `lake_mode`：

```json
{
  "lake_mode": "directory",
  "fields": [
    {"name": "id", "type": "bigint"},
    {"name": "name", "type": "varchar"},
    {"name": "geom", "type": "geometry"}
  ],
  "geometry": {
    "has_geometry": true,
    "column": "geom",
    "srid": 4326
  }
}
```

关系型表的 `data_config` 不含 `lake_mode` 字段，两者互不干扰。

### 4.3 common/duckdb 共享模块

从 Develop 模块提取以下逻辑到 `common/duckdb/`：

```
common/duckdb/
├── engine.go        ← DuckDB 连接管理、引擎挂载（httpfs/postgres/mysql）
├── rewriter.go      ← SQL 改写（三段式 → read_parquet）
└── executor.go      ← 查询执行、结果集转换
```

Develop 模块和 Service 模块均引用 `common/duckdb/`，各自编译。

### 4.4 DuckDB 虚拟引擎（不注册到 System）

DuckDB 是嵌入式库（CGO），不是独立运行的引擎服务，**不注册到 System 模块**（注册到 System 意味着可独立连接的服务，会产生误导）。

各模式的处理方式：

**`table` 模式（湖表）**：无需感知 DuckDB 引擎。`engine_id` 指向 MinIO/S3 引擎，`engine_type = 'minio'/'s3'` 自动触发 DuckDB 执行路径，与 System 无关。

**`sql` 模式（DuckDB SQL，第二阶段）**：
- 引擎选择器末尾追加 DuckDB 虚拟条目（不来自 System，与 Develop 模块保持一致）
- 用户选择 DuckDB 虚拟引擎时，`engine_id` 存为 null
- 后端判断：`config_type = 'sql'` 且 `engine_id IS NULL` → DuckDB 联邦查询执行路径
- 需将 `engine_id` 字段改为可空（去掉 NOT NULL 约束）

---

## 五、服务发布流程

### 5.1 湖表服务发布（table 模式 + MinIO/S3 引擎）

```
用户操作：
1. 选择 MinIO/S3 存储引擎
2. 浏览表格列表（前端调 Meta API，过滤 item_type='table' 且 `item.format` 为 parquet/orc/avro）
3. 选择目标湖表
4. 填写服务名称、访问控制等基本信息
5. 提交

后台处理：
1. 从 Meta 获取湖表信息（physical_path、lake_mode、fields）
2. 按命名约定设置 schema_name（中间路径）和 table_name（最终名称）
3. 将 lake_mode 和 fields 写入 data_config
4. 创建 query_services 记录（config_type='table'）
5. 返回服务端点
```

### 5.2 DuckDB SQL 服务发布（sql 模式 + DuckDB 引擎）

```
用户操作：
1. 选择 DuckDB 虚拟引擎
2. 编写 DuckDB SQL（支持三段式跨源查询）
3. 填写服务名称、访问控制等基本信息
4. 提交

后台处理：
1. 验证 SQL 语法（可选）
2. 创建 query_services 记录（config_type='sql', engine_id=DuckDB引擎ID）
3. 返回服务端点
```

---

## 六、实施计划

### 第一阶段：湖表查询服务（table 模式）

**目标**：用户能将 MinIO/S3 上的表格资源发布为 REST Query 服务。

**后端**：
1. `common/duckdb/` — 从 Develop 模块提取 DuckDB 核心逻辑
2. `service/backend/internal/service/query_executor_service.go` — 新增 DuckDB 执行路径（engine_type 路由）
3. `service/backend/internal/service/query_service_service.go` — 创建服务时从 Meta 读取湖表信息，写入 data_config

**前端**：
- 表选择器支持 MinIO/S3 引擎，展示表格列表（调 Meta API）
- 其余 UI 与关系型表发布流程完全一致

**输出格式**：JSON、CSV（第一阶段）

### 第二阶段：DuckDB SQL 查询服务（sql 模式）

**目标**：用户能将 DuckDB 联邦 SQL 发布为 REST Query 服务。

**后端**：
1. `query_executor_service.go` — sql 模式新增 DuckDB 执行路径
2. SQL 改写逻辑复用 `common/duckdb/rewriter.go`

**前端**：
- 引擎选择器中展示 DuckDB 虚拟引擎
- SQL 编辑器支持三段式语法提示（从 Meta 获取湖表列表）

### 第三阶段：空间数据支持

**目标**：湖表中的空间字段支持 OGC Features 协议。

**后端**：
1. 创建服务时检测 `data_config.fields` 中的空间字段
2. 自动启用 OGC Features 协议
3. DuckDB 查询结果中的 WKT/WKB 字段转换为 GeoJSON

---

## 七、关键文件清单

**新增**：
- `common/duckdb/engine.go` — DuckDB 引擎挂载
- `common/duckdb/rewriter.go` — SQL 改写
- `common/duckdb/executor.go` — 查询执行

**修改**：
- `service/backend/internal/service/query_executor_service.go` — 新增 DuckDB 执行路径
- `service/backend/internal/service/query_service_service.go` — 创建湖表服务时读取 Meta 信息
- `service/backend/internal/models/query_service.go` — IsLakeTable() 等辅助方法
- `develop/backend/internal/service/duckdb_service.go` — 重构为调用 common/duckdb
- `develop/backend/internal/service/sql_rewriter.go` — 重构为调用 common/duckdb

**数据库**：
- 无需新增字段，`data_config` JSONB 扩展 `lake_mode` 字段即可
