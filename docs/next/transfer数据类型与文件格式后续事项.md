# Transfer 数据类型与文件格式后续事项

更新时间：2026-05-09

本文记录 Transfer 与数据类型、文件格式、引擎能力组合相关的后续事项。

正式概念入口：

- [ADDP 格式与数据类型总体模型](addp格式与数据类型总体模型.md)
- [ADDP 格式与数据类型 Provider 消费者调研](addp格式与数据类型Provider消费者调研.md)
- [ADDP Format Capability 与 Data Type Provider 接口草案](addp格式Capability与DataTypeProvider接口草案.md)

## 核心判断

Transfer 不能继续只按 connector type 或具体格式名路由。

合理模型是：

```text
TransferEndpoint
  Engine capability
  Format capability(optional)
  Data type provider
```

其中：

- engine 负责连接、catalog、对象流、原生表读写。
- format 负责编码、解码、组件规则、提交边界。
- data type provider 负责 table / document / media / container / graph 的平台语义。

## 现状问题

当前 Transfer 仍存在这些耦合：

- reader / writer 同时处理存储访问和格式解析。
- S3 / MinIO 等对象存储读写器里混有 CSV、JSON、Parquet、Shapefile 等格式分支。
- 前端和后端任务配置仍容易把 engine、format、data_type 混成一个选择项。
- 空间字段、schema 和组件资源存在重复推断风险。

这些问题不需要一次性重构，但后续所有改动都应朝组合模型收敛。

## 批量读取模型

读取侧应拆成两步：

1. engine provider 打开资源、列举对象、读取组件。
2. format provider 把资源流解码为平台 DataBatch。

典型场景：

| 来源 | engine 能力 | format 能力 | 输出 |
|---|---|---|---|
| PostgreSQL table | SQL batch read | 无 | DataBatch |
| S3 CSV | object read | CSV batch read | DataBatch |
| S3 Shapefile | object read + component read | Shapefile batch read | DataBatch |
| NFS Parquet 目录 | list + file read | Parquet multi-file read | DataBatch |

## 批量写入模型

写入侧也应拆成两步：

1. format provider 把 DataBatch 编码成目标格式资源。
2. engine provider 把目标资源提交到存储或原生表。

典型场景：

| 目标 | format 能力 | engine 能力 | 结果 |
|---|---|---|---|
| PostgreSQL table | 无 | SQL batch write | 原生表 |
| S3 Parquet | Parquet batch write | object write | 对象文件 |
| S3 Shapefile | Shapefile component write | object component commit | 多组件文件 |
| GeoPackage 文件 | GeoPackage write | object write | 容器文件 |

## 双能力校验

Transfer planner 在生成执行计划前，至少要校验：

- source engine 是否能读取目标资源。
- source format 是否能解码为目标 data type。
- target engine 是否能写入目标位置。
- target format 是否能编码目标 data type。
- multi / whole item 是否具备组件读取和提交能力。
- 空间字段、SRID、geometry encoding 是否明确。
- schema 是否来自 Meta 标准 attributes 或 source provider。

## 提交边界

单文件格式和多组件格式必须区别处理：

- 单文件格式：format writer 产出一个资源，engine writer 提交一个对象或文件。
- 多组件格式：format writer 产出一组资源，engine writer 必须整体提交。
- whole scope 格式：planner 必须确认 manifest、分区、辅助文件和 commit policy。

Shapefile 这类格式不能只写 `.shp`，必须按组件集合提交。

## 与 attributes 的关系

Transfer 应消费已入库标准 attributes：

- `attributes.item.organization`
- `attributes.item.data_type`
- `attributes.item.format`
- `attributes.item.component_files`
- `attributes.type_info.table.fields`
- `attributes.capabilities.spatial`
- `attributes.format_info.<format>`

Transfer 不应重复推断：

- item 组织方式。
- 组件文件集合。
- 字段类型。
- 空间字段名。

## 后续任务

1. 扫描 Transfer 中按 connector type 或具体格式硬编码的 reader / writer 创建路径。
2. 梳理任务配置中 engine、format、data_type 三个维度的来源。
3. 先为对象存储文件读写定义 planner 过渡层。
4. 优先覆盖 CSV、JSON、Shapefile、Parquet 四类 table 场景。
5. 再处理 SQLite、GeoPackage、Excel 等 container 场景。
6. 最后处理 whole scope 和并行写入策略。

## 暂不做

- 不立刻替换现有 `pipeline.Reader` / `pipeline.Writer`。
- 不在 Transfer 内重新实现 Meta detector。
- 不让前端直接决定空间字段或组件文件集合。
- 不把 `json + spatial` 重新命名成独立顶层格式。

