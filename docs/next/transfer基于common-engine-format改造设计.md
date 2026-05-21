# Transfer 基于 common engine / format 的改造进展

更新时间：2026-05-21

本文记录 Transfer 模块基于 `common/engine`、`common/format`、`common/contentio` 重构后的当前状态和后续路线。当前口径采用 clean break：不兼容旧任务 JSON，不保留 Transfer 私有 reader / writer 插件体系，不为历史 pipeline 做兼容分支。

稳定后的通用规则应继续沉淀到 `docs/spec/`；本文只保留正在推进中的设计、进展和下一步。

## 一、当前结论

Transfer 的长期定位已经收敛为：

```text
Transfer 负责任务与编排
common 负责可复用读写与格式能力
```

具体来说：

- engine-native 读写能力归 `common/engine`，例如 PostgreSQL cursor / COPY、NFS / S3 / MinIO content read-write、DeleteResource。
- format / data type 读写能力归 `common/format`，例如 CSV / JSON / Parquet / Shapefile 对 table data type 的 reader / writer。
- content 定位、stream open/create、scope list、range read 归 `common/contentio`；multi ref 的组织规则和读写语义归 `common/format` / `common/dataitem` / Transfer 编排层；engine 到 contentio 的适配归 `common/engine/contentadapter`。
- Transfer 只保留任务配置、planner、policy、transform 编排、worker、checkpoint、日志、指标、重试和写后 Meta 扫描触发。

旧 Transfer `plugins/readers`、`plugins/writers`、`pkg/plugin_loader`、旧 `pkg/pipeline` 和旧 transform API 不作为新主路径保留。已经删除的旧入口不再恢复。

## 二、统一链路原则

### 2.1 按 data type 统一处理，不按引擎组合分叉

Transfer 不应为 `pg -> pg`、`pg -> nfs`、`nfs -> minio`、`minio -> minio` 分别做独立链路。只要 source / target 的 `data_type` 相同，应该走统一的数据类型链条。

例如 table 的统一链路：

```text
source endpoint
  -> table reader
  -> table batch / table rows
  -> table transform
  -> table writer
  -> target endpoint
```

endpoint 只决定 reader / writer 来自哪里：

| endpoint 表示 | Reader / Writer 来源 |
|---|---|
| native table | `common/engine` table reader / writer |
| encoded file/object | `common/engine` content reader / writer + `common/format` table reader / writer |
| multi ref file/object | `common/contentio.Writer` + `[]format.RelatedRef` + `common/format` multi table writer |

因此：

- PostgreSQL table -> PostgreSQL table 不应有 native-to-native 专用 executor。
- PostgreSQL table -> NFS Shapefile 不应有 PostGIS-to-Shapefile 专用 executor。
- NFS Shapefile -> MinIO Shapefile 不应有 file-to-object 专用 executor。
- MinIO CSV -> MinIO Parquet 也应是 table reader + table writer 的同一条链。

确实需要分叉时，只能按 data type / representation / layout 分叉，而不是按具体引擎组合分叉。

### 2.2 不新增无价值 adapter

adapter 只有在跨层语义确实不同、且无法通过 common 抽象表达时才允许出现。不得为了兼容旧 pipeline、旧 JSON 或局部妥协增加套壳代码。

当前应优先补 common 能力，而不是在 Transfer 中新增一套“临时 reader / writer”。

### 2.3 format 能力必须面向 data type 命名

`common/format` 的实现目录按格式组织，但能力接口面向 data type：

- table reader / writer：`TableReaderProvider`、`TableWriterProvider`
- multi table writer：`MultiTableWriterProvider`
- multi table reader：`MultiTableReaderProvider`
- table sample / info：`TableSampleReader`、`TableInfoProvider`

不要再引入 `FormatRecordReader`、`TableFormatWriter` 这类混淆命名。

多 ref table 能力的边界如下：

| 接口 | 语义 | 主要使用方 |
|---|---|---|
| `MultiTableInfoProvider` | 多 ref table 的 info 能力，面向元数据和 schema 探查。 | Meta、Manager、Transfer 探查 |
| `MultiTableSampleReader` | 多 ref table 的 sample 能力，面向预览、探查和少量样本读取。 | Manager、Transfer 探查兜底 |
| `MultiTableReaderProvider` | 多 ref table 的连续读取会话，面向全量批处理。 | Transfer 主链路 |
| `MultiTableWriterProvider` | 多 ref table 的连续写出会话。 | Transfer 写侧 |

因此 `MultiTableReaderProvider` 不是重复定义 sample reader，而是把“按样本窗口读取”和“打开一次、连续按批读取”拆开。Shapefile 这类格式在 Transfer 中优先使用连续 reader；sample reader 保留给预览和兜底。

### 2.4 原生字段类型不得进入执行决策

`common/format` 已定义 ADDP 统一字段类型。各 format / engine plugin 必须在自身边界内完成原生字段类型与 ADDP 标准字段语义的转换：

```text
format / engine native field type
  -> ADDP FieldType / Size / Precision / SpatialInfo
```

因此：

- Shapefile 的 DBF `N/C/F/D/L`、CSV 的采样推断原始字符串、Parquet / Excel / SQLite 的原生字段类型，都只能作为对应 format plugin 的内部事实。
- PostgreSQL 的 `int4`、`varchar(32)`、`geometry(MultiPolygon,4326)` 等也只能作为 PostgreSQL engine plugin 的内部事实。
- Transfer、transform、writer selection、native table prepare / write 不得读取或判断任何 format / engine 原生字段类型。
- 跨模块执行链路只使用 ADDP 标准字段类型和标准补充事实，例如 `SpatialInfo.GeometryColumn`、`SpatialInfo.GeometryType`、`SpatialInfo.SRID`。
- 如果 Manager / Meta 需要展示“原始字段类型”，也只能通过只读的抽象 attributes 暴露，供查看和诊断使用；不得反向参与 Transfer / engine / format 的写入决策。

`OriginalType` / `NativeType` 不作为公共 `FieldInfo` 字段存在。原生字段类型如需展示，统一由对应 plugin 写入只读 attributes 的 `native_type`；已有元数据不做兼容迁移，删除后重新扫描生成。

## 三、新任务 JSON 口径

新任务配置以 source / target endpoint 为核心：

```json
{
  "mode": "batch",
  "source": {
    "engine": {"scope": "system", "id": 1},
    "resource": {
      "kind": "native_table",
      "path": {"schema": "public", "table": "roads"}
    },
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "engine": {"scope": "system", "id": 2},
    "resource": {
      "kind": "file",
      "path": {"path": "exports/roads.csv"}
    },
    "data_type": "table",
    "representation": "encoded",
    "format": "csv",
    "options": {"header": true},
    "policy": {"write_mode": "overwrite"}
  },
  "transforms": [
    {
      "type": "field_mapping",
      "version": "v1",
      "mode": "project",
      "fields": [
        {"source": "name", "target": "road_name", "target_type": "string"},
        {"source": "geom", "target": "geometry", "target_type": "geometry", "nullable": false},
        {"target": "created_by", "target_type": "string", "default": "transfer"}
      ]
    }
  ],
  "batch_size": 10000
}
```

关键规则：

1. `engine.id` 指向 System engine。
2. `resource.kind` 表示资源形态，例如 `native_table`、`file`、`object`。
3. `resource` 是 Transfer endpoint 的业务描述字段，不是 `common/contentio` 抽象；planner 将其转换为 engine `CatalogPath`，executor 再通过 `contentadapter` 构造 `contentio.Reader` / `contentio.Writer`。
4. `data_type` 表示平台数据类型，例如 `table`、`document`、`media`、`container`、`graph`。
5. `representation` 表示 endpoint 表示方式：`native` 或 `encoded`。
6. `format` 只用于 encoded endpoint，例如 `csv`、`jsonl`、`parquet`、`shapefile`。
7. native endpoint 不写 `format=table`。
8. GeoJSON 输出按 `format=json + spatial.target_encoding=geojson` 表达。
9. Shapefile 输出按 `format=shapefile` 表达，并通过 multi ref writer 写出 `.shp/.shx/.dbf/.cpg/.prj` 等相关内容。
10. encoded target 的默认写出后缀归 `common/format` 定义。Transfer planner 只消费 `common/format` 的默认写出后缀来规范化目标 file/object 路径：缺失后缀时自动补齐，已有冲突后缀时拒绝创建计划，不在 Transfer 内硬编码具体格式后缀。
11. `policy.write_mode` 目前只保留 `overwrite` 和 `append`。是否先删、删什么，是 Transfer 策略；common engine 只提供删除指定资源的原子能力。
12. `transforms` 描述 source 和 target 之间的 table batch 转换，不属于 source / target endpoint。
13. endpoint 的 `engine.type` 不是必填事实。生产链路应以 `engine.id` 为身份，Transfer 通过 System engine resolver 获取 engine type 和 connection info；`engine.type` 只作为测试或诊断时的可选一致性校验，不应由上游模块硬编码。

旧字段 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等不再兼容，出现即拒绝。

### 3.1 Manager 上传导入 Shapefile

Manager 的上传导入入口目前用于“用户上传一个 Shapefile ZIP，导入到目标 native table”。该入口已经切到新 endpoint config：

- Manager 只负责接收 ZIP、校验一套同 basename 的 `.shp/.dbf/.shx`、上传到中转对象存储。
- source endpoint 使用 `representation=encoded`、`format=shapefile`、`resource.kind=object`。
- target endpoint 使用 `representation=native`、`resource.kind=native_table`。
- DBF 编码通过 source endpoint `options.encoding` 传递给 Transfer planner，再进入 format `ParseOptions.Encoding`。
- source 对象存储 engine id 来自 `MANAGER_IMPORT_SOURCE_ENGINE_ID`，或由 Manager 按中转 MinIO endpoint / bucket / access key 在 System 对象存储引擎中自动匹配。
- Manager 不在 Transfer 任务 JSON 中声明 source engine type；Transfer 根据 System engine id 解析真实 engine type 和 connection info。

这一路径本质上仍是标准 Transfer 任务，不是 Manager 私有导入协议。

### 3.2 field_mapping transform

字段映射是第一类正式进入新主链路的 Transfer transform。它取代旧的任务外层 `mappings` / `field_mappings` 附属表语义，但用户界面仍可继续叫“字段映射”。

字段映射只负责 table batch 的字段级变换：

- 字段投影：只输出声明的目标字段。
- 字段重命名：`source` 读源字段，`target` 写目标字段。
- 默认值：`source` 为空，或源字段不存在 / 值为 nil 时使用 `default`。
- 类型声明：`target_type` 写入目标 schema，供 native table prepare、CSV / JSON / Parquet / Shapefile writer 使用。
- 空间字段同步：`target_type=geometry` 时，目标 schema 的 `SpatialInfo.GeometryColumn` 跟随 `target`。

第一版不支持表达式语言、过滤、条件分支、聚合或跨行计算。此类 ETL 能力后续作为新的 transform 类型补充，不塞进 `field_mapping`。

字段项定义：

| 字段 | 必填 | 说明 |
|---|---|---|
| `source` | 否 | 源字段名；为空表示常量 / 默认字段。 |
| `target` | 是 | 目标字段名。 |
| `target_type` | 否 | 目标字段类型，例如 `string`、`int`、`bigint`、`double`、`bool`、`date`、`timestamp`、`geometry`。 |
| `nullable` | 否 | 目标字段是否可空；默认 `true`。 |
| `default` | 否 | 源字段缺失或值为 nil 时使用的默认值。 |
| `format` | 否 | 日期、时间、数字等简单解析 / 格式化提示；第一版只保留配置，不引入表达式。 |

`mode` 第一版支持：

| mode | 语义 |
|---|---|
| `project` | 只输出 `fields` 声明的目标字段。默认模式。 |
| `passthrough` | 先保留源 row，再应用字段映射覆盖 / 新增目标字段。 |

旧任务外层 `mappings` 不再作为新执行主链路输入；新任务必须把字段映射写入 `config.transforms[]`。

## 四、已完成能力

### 4.1 common/engine

| 能力 | 状态 |
|---|---|
| PostgreSQL `TableReadSessionProvider` | 已补 cursor session，Transfer export 优先使用，避免大表 `LIMIT/OFFSET` 翻页退化。 |
| PostgreSQL `BatchWritableProvider` | 已补普通 batch insert、单批 COPY。 |
| PostgreSQL `TableWriteSessionProvider` | 已补跨批次 COPY session。 |
| PostgreSQL `TableWritePreparer` | 已收敛为 ensure / create table，不承载追加 / 覆盖策略。 |
| PostgreSQL 空间字段元数据 | 已从 catalog 读取字段类型，可保留 `geometry(MultiPolygon,4326)` 等类型。 |
| PostgreSQL 空间写入 | 已支持 geometry 字段写入 WKT 以及 WKB / EWKB `[]byte`；Transfer 对 encoded 空间表导入 PostgreSQL 时可请求 `ewkb`。 |
| Shapefile 空间写出 | 已根据 `SpatialInfo.GeometryType` / `SpatialInfo.Dimension` 选择二维或 Z shape type；writer 内部负责 `geom.T` 到 Shapefile native geometry 的转换。 |
| NFS `ContentWritableProvider` | 已支持单 writer 会话写入，不再每批次删除目标文件。 |
| S3 / MinIO `ContentWritableProvider` | 已升级为 streaming multipart，不再依赖 OS 临时文件。 |
| NFS / S3 / MinIO / PostgreSQL `DeleteResource` | 已补原子删除能力，Transfer overwrite 先调用删除再写入。 |
| 文件型 catalog 路径规范 | NFS root 已统一：`name="/"`，`full_name=""`，业务路径不出现 `.`；非根扫描会先确保 root -> dir 节点链。 |

### 4.2 common/format

| 格式 | Reader | Writer | 当前说明 |
|---|---|---|---|
| CSV / TSV | `TableReaderProvider` 已有 | `TableWriterProvider` 已有 | table transfer 主链路已使用。 |
| JSON / JSONL | `TableReaderProvider` 已有 | `TableWriterProvider` 已有 | 支持 JSON array、JSON Lines。 |
| GeoJSON encoding | 复用 JSON table reader / writer | JSON writer 支持 `spatial.target_encoding=geojson` | GeoJSON 不是顶层独立格式，而是 JSON 的空间编码策略。 |
| Parquet | `TableReaderProvider` 已有；`ScopeTableReaderProvider` 已支持 dataset / partitioned table 连续读取，并可从 Hive-style `key=value` 路径补充分区字段 | 最小 `TableWriterProvider` 已有 | 单文件和 whole scope table transfer 已有主链路；row group 性能、predicate / field selection 仍待增强。 |
| Shapefile | `MultiTableReaderProvider` 已有，Transfer 主链路优先使用；info / sample 保留给 Meta / Manager / 探查 | `MultiTableWriterProvider` 已有 | multi ref 读写已可用于 Transfer；range source 下按 `.shx` 读取索引窗口、`.dbf` 连续属性块和 `.shp` 记录窗口；非 range source materialize 到本地后也继续使用本地 `.shx` 索引，只有缺索引或不支持 shape 类型时才回退顺序读取。 |

### 4.3 Transfer

| 能力 | 状态 |
|---|---|
| 新 endpoint JSON | 创建、编辑、详情、执行已切换到新结构。 |
| planner | 已能规划 native table、encoded table file/object、multi ref export、native table import；source endpoint 可消费 Meta item 标准 attributes，还原 `layout/data_type/format/refs/storage/type_info/capabilities`，不再默认猜 refs、字段类型或空间字段。 |
| executor | 已统一走 common engine / format / contentio 能力；single content 使用固定 CatalogPath，multi source 优先使用 planner 提供的 `attributes.item.refs`，whole source 走 `ScopeTableReaderProvider`，multi writer 可按 format specs 生成目标 refs。 |
| worker | Asynq worker 保留，执行入口已切到 planner + executor。 |
| field mapping transform | 已进入 `config.transforms[type=field_mapping]`，executor 可执行投影、重命名、默认值、目标类型和 geometry schema 同步。 |
| metrics | 已回写基础 records_read / records_written。 |
| overwrite / append | 已收敛为 Transfer policy；common engine 不理解写入模式。 |
| 写后 Meta 扫描 | 已触发 deep scan；文件型目标扫描父目录，对象存储目标扫描 bucket/prefix 容器。 |
| UI | 目标引擎选择、目录选择、格式选择、任务编辑回显已切到新规范；目录选择器使用 catalog，NFS root 展示为 `/`，保存语义路径。 |

## 五、已完成或已验证链路

| 链路 | 状态 | 说明 |
|---|---|---|
| PostgreSQL table -> NFS CSV | 已真实跑通 | `public.yanshi` 导出 73090 行，CSV 含 header。 |
| PostgreSQL table -> NFS JSONL -> PostgreSQL table | 已通过 executor 真实链路验证 | JSON / JSONL reader / writer 与 PostgreSQL COPY 写侧已接入。 |
| PostgreSQL table -> NFS Parquet -> PostgreSQL table | 已真实跑通 | Parquet 最小 reader / writer 可用。 |
| CSV / TSV file/object -> PostgreSQL table | 已接入第一版 import | PostgreSQL 目标默认优先 COPY session。 |
| PostgreSQL table -> PostgreSQL table | 已收敛到统一 table reader / writer 链路 | 不保留 native-to-native 专用通道；空间字段类型已修复。 |
| PostgreSQL spatial table -> PostgreSQL spatial table | 已修复空间字段类型保真 | 旧目标表可直接删除后重建，不做兼容迁移。 |
| PostgreSQL spatial table -> NFS Shapefile | 已真实验收通过 | NFS root / Meta 扫描闭环已验证，item node、字段、行数和空间能力可被 Manager 看到；native source 的 `dimension=3` 可驱动 Shapefile writer 写出 Z shape 并读回三维 WKT。 |
| PostgreSQL spatial table -> MinIO Shapefile | 已真实验收通过 | 覆盖 geometry type、SRID、字段类型、相关内容、Meta scan 和 Manager preview；同一套 schema / row value 协议适用于对象存储目标。 |
| NFS Shapefile -> PostgreSQL table | 已真实验收通过 | Shapefile multi ref 读侧可进入统一 table reader / writer 链路，PostgreSQL 目标优先 COPY session；空间行值可走 EWKB；已补默认跳过的 PostGIS 集成测试，覆盖二维 Point 以及 PointZ / PolylineZ / PolygonZ / MultiPointZ。 |
| MinIO Shapefile -> PostgreSQL table | 已真实验收通过 | 对象存储 multi ref 读侧可导入 native table；空间行值可走 EWKB。 |
| NFS Shapefile -> MinIO Shapefile | 已手动验证通过，默认测试已覆盖核心 format 链路 | multi ref 正确生成；Meta deep scan 后可得到字段、行数、format info、spatial capabilities；executor 半集成测试已覆盖 Shapefile encoded source -> Shapefile encoded target，并校验 refs、`SpatialInfo.Dimension=3` 和 Z WKT。 |
| Parquet dataset / partitioned table -> CSV | 已通过 executor 单元链路验证 | `layout=whole` source 通过 `ScopeTableReaderProvider` 递归读取 scope 下 `.parquet` 文件，不使用 sample reader 冒充全量读取。 |

## 六、写后 Meta 扫描规则

Transfer 成功写出目标后，如果 `auto_scan_metadata=true`，必须立即触发 Meta deep scan。

扫描目标必须使用目标资源所在容器，而不是总是扫引擎根，也不是总是扫单文件：

| 目标类型 | 写出目标 | Meta 扫描目标 |
|---|---|---|
| PostgreSQL native table | `public.roads` | `public` |
| NFS 顶层文件 | `roads.csv` | `/` |
| NFS 子目录文件 | `exports/roads.csv` | `exports` |
| NFS Shapefile | `shp/a3.shp` + refs | `shp` |
| MinIO object | `addp/exports/roads.csv` | `addp/exports` |
| MinIO Shapefile | `addp/gis/a3.shp` + refs | `addp/gis` |

文件型引擎的特别规则见 [存储引擎路径体系规范](../spec/addp存储引擎路径体系规范.md)：NFS root `name="/"`，`full_name=""`，`path` 是 Meta 内部节点链，`.` 不进入业务路径。

## 七、执行进度、Checkpoint 和日志

Transfer 执行状态复用 `common.task_executions`：

- `records_read`、`records_written` 写入统一执行表的指标字段。
- `checkpoint_offset`、`checkpoint_state` 写入 `metadata`。
- 运行日志暂存于 `error_details.logs`；这是当前兼容实现，后续如日志量增大再拆到独立日志表或对象存储。

第一阶段目标不是恢复执行，而是先形成稳定观测点：每个成功写入的 batch 都回写一次执行进度、checkpoint 和简短日志。

batch checkpoint 最小结构：

```json
{
  "checkpoint_offset": 20000,
  "checkpoint_state": {
    "version": "v1",
    "batch_index": 2,
    "source_offset": 10000,
    "records_read": 20000,
    "records_written": 20000,
    "target_committed": true
  }
}
```

规则：

1. checkpoint 只在目标 batch 写入成功后更新，避免记录未提交状态。
2. `checkpoint_offset` 第一版等于累计 `records_read`。
3. `source_offset` 使用当前 batch 的 `BatchData.Offset`。
4. `batch_index` 从 1 开始递增。
5. 当前版本只记录进度和故障定位信息，不承诺从 checkpoint 自动恢复。
6. 进度百分比在无法预知总行数时只表示执行活跃度：运行中从 0 递增但不超过 99，成功后统一置为 100。

## 八、职责边界

| 层 | 负责 | 不负责 |
|---|---|---|
| `common/engine` | 引擎连接、能力声明、catalog、metadata、content read/write、range read、table batch/session read/write、DeleteResource、query、stream / CDC 扩展点 | Transfer 任务、写入模式、调度、执行历史、重试策略 |
| `common/contentio` | Ref、Reader、Writer、Lister、RangeReader、Stat 等内容 I/O 原语 | 格式解析、multi item 组织规则、任务策略、engine 连接 |
| `common/engine/contentadapter` | engine content provider 到 contentio 的适配、CatalogPath 与 Ref 的映射 | 格式解析、任务策略 |
| `common/format` | 格式身份、descriptor、capability view、data type reader / writer、编码解码、schema / sample / multi 能力 | engine 连接、worker、任务记录 |
| `transfer` | 任务 JSON、planner、policy、field mapping / transform、worker、checkpoint、logs、metrics、写后 Meta scan | 具体 engine reader / writer、具体 format reader / writer |

边界判断原则：

1. 其他模块也会复用的能力，进入 common。
2. 只和一次 Transfer 任务策略有关的逻辑，留在 Transfer。
3. 追加 / 覆盖是 Transfer policy，不进入 common engine。
4. 删除指定资源是 common engine 原子能力。
5. 格式 reader / writer 只能属于 common format，不能回到 Transfer 私有插件。

## 九、仍然不足

| 方向 | 当前不足 |
|---|---|
| whole scope table 全量读取 | `ScopeTableReaderProvider` 和 Parquet dataset 链路已接入；真实 NFS / MinIO dataset 已验收；Hive-style 分区字段已能进入 schema 和 row。 |
| checkpoint / progress | 已有 batch-level metrics / checkpoint / logs 最小闭环；尚未实现从 checkpoint 恢复执行。 |
| schema evolution | PostgreSQL 写侧可 ensure/create table，但字段变化、类型演进和目标表差异处理仍未完善。 |
| 并行读取 | PostgreSQL cursor session 已有，但分区并行读取、稳定快照和多 worker 协调仍未补。 |
| 其他数据库写侧 | MySQL、Doris、ClickHouse 等 common writer 仍待按真实需求补。 |
| Parquet 高性能 | 当前是最小 reader / writer，已支持通用 `field_selection`；row group reader、predicate 仍待补。 |
| Shapefile 读取 | 连续 `MultiTableReaderProvider` 已接入 Transfer 主链路；indexed reader 支持 range source 和本地 materialized fallback，当前覆盖 Point / Polyline / Polygon / MultiPoint，Z 类型已完成主要链路验收。 |
| non-table data type | document / media / container / graph 还未形成 Transfer 主链路。 |
| stream / CDC | common engine 尚无稳定 `StreamReadableProvider`、`CDCReadableProvider`、change event / offset 标准。 |
| transform 扩展 | `field_mapping` 已进入主链路；过滤、派生字段、表达式、空间坐标转换等更完整 ETL transform 尚未设计。 |

## 十、下一步建议

### 近期优先级

1. **增强 Parquet whole scope 读取能力**
   真实 NFS / MinIO Parquet dataset 的 `layout=whole` Transfer 链路已验收通过，Hive-style 分区字段已能进入 schema 和 row。下一步可以在现有 `ScopeTableReaderProvider` 基础上继续补 row group 和 predicate。

   `field_selection` 是 table data type 的通用读取选项，表达调用方希望输出哪些字段。它不是某个格式的私有能力，也不是 GIS projection / CRS 投影。当前接口已落到 `common/format.ParseOptions.FieldSelection`；Parquet single / scope reader 已作为第一个 provider 消费该通用选项；Transfer planner 已能从 `field_mapping` 的 `project` 模式推导 source `FieldSelectionOptions`，并分别传给 encoded source parse options 与 native source read options。

   当前接口口径：

   ```go
   type ParseOptions struct {
       FieldSelection *FieldSelectionOptions
   }

   type FieldSelectionOptions struct {
       // Include 为空表示不裁剪，输出全部字段。
       // 非空表示只输出这些字段，输出顺序按 Include 保持。
       Include []string

       // 默认 error，避免静默产出缺字段数据。
       MissingFieldPolicy MissingFieldPolicy
   }

   type MissingFieldPolicy string

   const (
       MissingFieldError  MissingFieldPolicy = "error"
       MissingFieldIgnore MissingFieldPolicy = "ignore"
   )
   ```

   边界规则：

   - `field_selection` 由 `common/format` 定义，`TableInfoProvider`、`TableSampleReader`、`TableReaderProvider`、`MultiTable*` 和 `ScopeTable*` 可按需消费。
   - format provider 能下推则下推；不能下推时也可以读全后裁剪输出，但必须保证返回的 schema 与 row 字段一致。
   - native table engine 应复用同一语义，并尽量下推到 SQL `SELECT` 字段列表。当前 PostgreSQL `TableReadSessionProvider` 已消费 `field_selection` 并生成字段级 SELECT。
   - Transfer planner 只生成通用 `FieldSelectionOptions`，不得根据 Parquet、CSV、PostgreSQL 等具体格式或引擎写分支。
   - Transfer planner 仅从 `field_mapping mode=project` 的显式 `source` 字段推导读取字段；默认值 / 常量目标字段不进入读取字段；`mode=passthrough` 必须保留源 row 全字段，因此不下推 `field_selection`。
   - 空间字段不由 reader 隐式保留。若调用方需要 geometry 字段，必须显式加入 `Include`；CRS / 坐标投影仍属于 spatial capability 或 transform，不得和 `field_selection` 混用。
   - 第一版只支持 `Include`，暂不提供 `Exclude`，避免 include / exclude 优先级和隐式保留字段规则复杂化。
   - Parquet scope reader 打开单个文件时不会把 scope 级分区字段传入单文件 reader；它会先合并文件字段和 Hive-style 分区字段，再统一应用 `field_selection`。

2. **补 Transfer 验收用例沉淀**
   将已通过的 PostgreSQL spatial table -> NFS / MinIO Shapefile、MinIO Shapefile -> PostgreSQL table、NFS Shapefile -> MinIO Shapefile 继续形成可重复的集成测试或操作清单。

3. **设计 checkpoint 恢复语义**
   明确哪些 source reader 支持 seek / cursor 恢复、哪些 target writer 可幂等续写，再决定恢复执行是否进入主链路。

4. **设计下一批 transform 类型**
   在 `field_mapping` 稳定后，再讨论过滤、派生字段、简单表达式、空间坐标转换等 ETL 能力边界。

### 中期优先级

1. Parquet row group reader / writer 增强，支持 field selection、predicate 和 row group 级读取。
2. PostgreSQL schema evolution：字段新增、类型映射、目标表差异处理。
3. MySQL / Doris / ClickHouse 写侧 common writer，按真实链路逐个补，不一次性铺开。
4. document / media raw copy：先走 content reader / writer，不做格式转换。
5. container child table transfer：Excel sheet、SQLite table、GeoPackage layer 等按 child table 转出。

### 长期方向

1. Kafka / stream：新增 stream event、partition / offset checkpoint。
2. CDC：新增 change event 抽象，支持 snapshot + incremental。
3. graph：明确 graph native export / query result table 化 / 子图导出三类路径。
4. 当第二个非 table data type 或 CDC 真正进入 executor 后，再讨论是否新增 `common/transferio` 或更通用的 RecordBatch。

## 十一、压缩后的历史结论

以下内容不再展开保留，只保留结论：

- 旧任务 JSON 不兼容。
- 旧 local engines 不作为 Transfer 新入口。
- 旧 pipeline / plugins 不迁移为兼容层。
- 不新增 format 泛化 record reader / writer。
- 不为 native->native、file->object 等组合建立专用执行通道。
- GeoJSON 不是顶层 format，而是 JSON 的空间编码策略。
- Shapefile 是 multi table format，Transfer 通过 multi writer 写出。
- NFS root 是结构上必须存在、语义路径上为空、展示上可透明的节点；具体规范已移入 `docs/spec/addp存储引擎路径体系规范.md`。
