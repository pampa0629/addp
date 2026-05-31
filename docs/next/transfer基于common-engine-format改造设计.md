# Transfer 基于 common engine / format 的后续推进

更新时间：2026-05-30

本文只保留 Transfer 基于 `common/engine`、`common/format`、`common/contentio` 改造后的后续推进事项。table 类型 Transfer 主链路已经归档到正式文档，不再在本文展开。

## 一、已归档的稳定结论

table 类型 Transfer 主链路已经完成，可以作为稳定能力继续演进：

- native table、encoded single file/object、encoded multi refs、encoded whole scope 都走统一 table reader / writer 链路。
- Transfer 不再维护私有 reader / writer 插件体系，不兼容旧任务 JSON。
- Transfer 只负责任务、planner、policy、transform、worker、checkpoint、日志、指标、重试和写后 Meta scan。
- 具体 engine-native 读写归 `common/engine`。
- 具体格式和 data type reader / writer 归 `common/format`。
- content 定位和读写归 `common/contentio`，engine 到 contentio 的适配归 `common/engine/contentadapter`。
- checkpoint 当前是 observable / restartable，不是 checkpoint resumable。

正式文档入口：

| 内容 | 文档 |
|---|---|
| Transfer 当前架构 | [transfer/docs/design.md](../../transfer/docs/design.md) |
| Transfer 任务配置 | [transfer/docs/transfer-基本概念及配置说明.md](../../transfer/docs/transfer-基本概念及配置说明.md) |
| Transfer 数据库架构 | [transfer/docs/数据库架构.md](../../transfer/docs/数据库架构.md) |
| 任务表 | [transfer/docs/tables/tasks表.md](../../transfer/docs/tables/tasks表.md) |
| 执行记录 | [transfer/docs/tables/task_executions表.md](../../transfer/docs/tables/task_executions表.md) |
| format provider / field_selection / resume marker | [docs/spec/addp数据类型与格式能力规范.md](../spec/addp数据类型与格式能力规范.md) |
| contentio single / multi / scope 调用边界 | [docs/spec/addp内容IO抽象规范.md](../spec/addp内容IO抽象规范.md) |
| engine provider 边界 | [docs/spec/addp引擎插件接口规范.md](../spec/addp引擎插件接口规范.md) |
| engine capability 声明 | [docs/spec/addp引擎能力声明规范.md](../spec/addp引擎能力声明规范.md) |

## 二、仍然不足

| 方向 | 当前状态 |
|---|---|
| document / media raw copy | 只完成初步设想，尚未实现。 |
| container child table transfer | Excel sheet、SQLite table、GeoPackage layer 等 child table 尚未进入 Transfer 主链路。 |
| row_filter / predicate | 已讨论价值，暂缓；等待明确需求后再设计统一语义。 |
| checkpoint resumable | marker 观测和持久化已有，但恢复主链路尚未开启。 |
| 并行读取 | PostgreSQL cursor session 已有，分区并行读取、稳定快照和多 worker 协调未设计。 |
| 数据库写侧增强 | Doris Stream Load、ClickHouse 排序键 / 分区键 / 原生批量接口、PostgreSQL 更复杂 schema evolution 等仍待策略确认。 |
| stream / CDC | 尚无稳定 `StreamReadableProvider`、`CDCReadableProvider`、change event / offset 标准。 |
| transform 扩展 | `field_mapping` 已稳定；过滤、派生字段、表达式、空间坐标转换尚未设计。 |

## 三、document / media raw copy 初步设想（待确认）

本节只记录下一步实现前的设计草案，讨论清楚后再动代码。第一版目标不是做文档解析、媒体转码或格式转换，而是先把 `document` / `media` 这类 non-table data type 的最小 Transfer 主链路打通。

### 3.1 第一版目标

第一版只支持 encoded single content 的原样复制：

```text
source engine ContentReadableProvider
  -> contentadapter / contentio.Reader
  -> io.Copy
  -> contentadapter / contentio.Writer
  -> target engine ContentWritableProvider
```

适用范围：

| 维度 | 第一版取值 |
|---|---|
| `data_type` | `document`、`media`；`unknown` 是否纳入需讨论确认。 |
| `representation` | `encoded`。 |
| `layout` | `single`。 |
| `resource.kind` | `file` 或 `object`。 |
| source capability | engine 必须能提供 `ContentReadableProvider`。 |
| target capability | engine 必须能提供 `ContentWritableProvider`。 |
| 转换语义 | 不转换格式、不改变 data type、不解析正文、不转码、不生成缩略图。 |
| 写入模式 | 第一版只允许 `overwrite`；`append` 显式拒绝。 |

这里的 raw copy 使用 engine 内容流能力，不是 `common/format` 的 table reader / writer，也不是 `DocumentTextReader`、`MediaInfoProvider` 或 `BinaryContentReader`。format descriptor 仍用于识别 `data_type` / `format` / `layout`，但 executor 不进入格式解码。

### 3.2 任务 JSON 口径

document / media raw copy 继续使用统一 endpoint 结构，不新增 Transfer 私有 source / target 字段：

```json
{
  "mode": "batch",
  "source": {
    "engine": {"scope": "system", "id": 1},
    "resource": {
      "kind": "object",
      "path": {"path": "docs/a.pdf"}
    },
    "data_type": "document",
    "representation": "encoded",
    "format": "pdf"
  },
  "target": {
    "engine": {"scope": "system", "id": 2},
    "resource": {
      "kind": "file",
      "path": {"path": "backup/a.pdf"}
    },
    "data_type": "document",
    "representation": "encoded",
    "format": "pdf",
    "policy": {"write_mode": "overwrite"}
  }
}
```

建议第一版要求 target path 是完整 file / object 路径，不把目录 / prefix 当作目标路径自动拼接源文件名。UI 可以在创建任务时辅助带出 basename，但 planner / executor 不做目录语义猜测。

target 的 `data_type` / `format` 有两种可选口径，需要讨论确认：

| 方案 | 规则 | 取舍 |
|---|---|---|
| A：target 必填一致 | target 必须显式声明与 source 相同的 `data_type` 和 `format`。 | 最严格，任务 JSON 自描述强，但 UI / API 调用要重复字段。 |
| B：target 可继承 source | target 未声明时由 planner 继承 source 的 `data_type` 和 `format`；显式声明时必须一致。 | 更符合 raw copy 语义，减少重复，但 planner 需要负责规范化。 |

当前建议选 B：raw copy 本质是不改变内容语义；target 显式写了不同 `data_type` 或 `format` 时直接拒绝，不做隐式转换。

### 3.3 planner 规则

planner 对 source 侧优先消费已入库 Meta item 和标准 attributes：

- 使用 `attributes.item.data_type`、`attributes.item.layout`、`attributes.item.format` 判断是否进入 raw copy。
- 使用 `meta_item.full_name` / `attributes.storage.physical_path` / endpoint resource 还原 engine catalog path。
- `type_info.document`、`type_info.media`、`capabilities.extraction` 只作为展示和后续扫描事实，不参与 raw copy 执行决策。
- `layout=multi`、`layout=whole`、container child、native document / native media 暂不进入第一版 raw copy。

planner 对 target 侧只做资源和策略规范化：

- target engine 必须支持 stream write。
- target resource 必须能映射为 single file / object content。
- `overwrite` 由 Transfer policy 处理；common engine 只暴露删除或创建内容能力。
- target path 第一版必须是完整路径，不根据 format 自动补后缀；raw copy 不是格式写出，Transfer 不应为 document / media 硬编码扩展名。

### 3.4 executor 规则

executor 的主路径保持极简：

1. 基于 source engine resolver 和 catalog path 构造 `contentio.Reader`。
2. 基于 target engine resolver 和 catalog path 构造 `contentio.Writer`。
3. `overwrite` 时先按 Transfer policy 删除目标资源；如果目标 engine 没有 `DeleteResource`，需要讨论是拒绝 overwrite，还是依赖 `CreateContent` 覆盖语义。
4. 打开 source content 与 target content 后执行流式 `io.Copy`。
5. target writer close 成功后，才认为写出成功并触发 checkpoint / metrics / 写后 Meta scan。

checkpoint 第一版仍按 restartable 处理：失败后清理目标并从头重跑，不做 byte-range resumable。即使 source 支持 range read，也不在第一版引入 `RangeReadableProvider` + `RangeWritableProvider` 的断点续传，因为对象存储通常没有稳定 range write，目标侧提交语义还没有统一。

### 3.5 metrics、checkpoint 和写后扫描

table Transfer 当前以 `records_read` / `records_written` 为主；raw copy 更自然的指标是字节数。第一版建议：

- `records_read=1`、`records_written=1` 表示一个 content item 完成复制，保持现有执行表最小兼容。
- 同时在 execution metadata / checkpoint state 中记录 `bytes_read`、`bytes_written`；如果 source stat 能拿到 size，可计算 byte progress。
- 无法获取总 size 时，运行中进度仍按“活跃但未知总量”处理，成功后置 100。

写后 Meta 扫描沿用已有规则：

| 目标类型 | 写出目标 | Meta 扫描目标 |
|---|---|---|
| NFS file | `backup/a.pdf` | `backup` |
| MinIO / S3 object | `bucket/backup/a.pdf` | `bucket/backup` |

扫描后由 Meta / format descriptor 重新识别目标 item 的 `data_type`、`format`、`layout` 和 attributes。Transfer 不直接写目标 Meta attributes。

### 3.6 明确不做

第一版显式不做：

- document text extraction / OCR / 全文索引。
- image thumbnail / media metadata 解析。
- 音视频转码、抽帧、字幕、语音转写。
- document / media 格式转换，例如 DOCX -> PDF、PNG -> JPEG。
- container child copy，例如 ZIP entry、Excel sheet、SQLite table。
- multi refs raw copy，例如 Shapefile 相关文件组；Shapefile 已作为 table multi writer 进入 table 主链路，不走本节 raw copy。
- whole scope raw copy，例如复制整个目录 / prefix。
- checkpoint byte-range resumable。
- 新增 `binary` data type 或 `binary` format；未知二进制仍使用 `data_type=unknown` / `format=unknown` 的既有规范。

### 3.7 需要先确认的问题

实现前需要确认以下决策：

1. `unknown` 是否和 `document` / `media` 一起进入 raw copy 第一版。建议纳入，但只限 `layout=single` 的原始内容复制，不引入 binary data type。
2. target `data_type` / `format` 是否允许省略并继承 source。建议允许省略继承；显式声明时必须一致。
3. target path 是否必须是完整 file / object 路径。建议第一版必须完整，目录 / prefix 自动拼 basename 留给 UI 或后续方案。
4. overwrite 在 target engine 没有 `DeleteResource` 时如何处理。建议第一版拒绝，除非该 engine 的 `ContentWritableProvider` 明确声明 create 即覆盖。
5. 是否现在给 execution metadata 增加 `bytes_read` / `bytes_written`。建议增加，否则 raw copy 的进度和日志会被迫伪装成 row metrics。

## 四、后续优先级

### 近期

1. 确认 document / media raw copy 的 5 个待决策点。
2. 确认后实现 raw copy planner / executor / metrics / tests。
3. 设计下一批 transform 类型：过滤、派生字段、简单表达式、空间坐标转换。

### 中期

1. container child table transfer：Excel sheet、SQLite table、GeoPackage layer 等按 child table 转出。
2. checkpoint resumable：只有 source / target 能力声明和 executor 验收同时补齐后，才讨论 `resume_mode=checkpoint`。
3. 数据库写侧增强：PostgreSQL 主键 / 索引 / nullable 收紧 / 复杂类型演进，Doris Stream Load，ClickHouse 排序键 / 分区键 / 原生批量接口。

### 长期

1. Kafka / stream：新增 stream event、partition / offset checkpoint。
2. CDC：新增 change event 抽象，支持 snapshot + incremental。
3. graph：明确 graph native export / query result table 化 / 子图导出三类路径。
4. 当第二个非 table data type 或 CDC 真正进入 executor 后，再讨论是否新增 `common/transferio` 或更通用的 RecordBatch。
