# Transfer 基于 common engine / format 的改造进展

更新时间：2026-05-18

本文记录 Transfer 模块基于 `common/engine`、`common/format`、`common/resource` 重构后的当前状态和后续路线。当前口径采用 clean break：不兼容旧任务 JSON，不保留 Transfer 私有 reader / writer 插件体系，不为历史 pipeline 做兼容分支。

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
- 资源定位、多组件、range、component 读写归 `common/resource`。
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
| multi component file/object | `common/resource` component writer + `common/format` component table writer |

因此：

- PostgreSQL table -> PostgreSQL table 不应有 native-to-native 专用 executor。
- PostgreSQL table -> NFS Shapefile 不应有 PostGIS-to-Shapefile 专用 executor。
- NFS Shapefile -> MinIO Shapefile 不应有 file-to-object 专用 executor。
- MinIO CSV -> MinIO Parquet 也应是 table reader + table writer 的同一条链。

确实需要分叉时，只能按 data type / representation / organization 分叉，而不是按具体引擎组合分叉。

### 2.2 不新增无价值 adapter

adapter 只有在跨层语义确实不同、且无法通过 common 抽象表达时才允许出现。不得为了兼容旧 pipeline、旧 JSON 或局部妥协增加套壳代码。

当前应优先补 common 能力，而不是在 Transfer 中新增一套“临时 reader / writer”。

### 2.3 format 能力必须面向 data type 命名

`common/format` 的实现目录按格式组织，但能力接口面向 data type：

- table reader / writer：`TableReaderProvider`、`TableWriterProvider`
- table component writer：`ComponentTableWriterProvider`
- table sample / info：`TableSampleReader`、`TableInfoProvider`

不要再引入 `FormatRecordReader`、`TableFormatWriter` 这类混淆命名。

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
3. `data_type` 表示平台数据类型，例如 `table`、`document`、`media`、`container`、`graph`。
4. `representation` 表示 endpoint 表示方式：`native` 或 `encoded`。
5. `format` 只用于 encoded endpoint，例如 `csv`、`jsonl`、`parquet`、`shapefile`。
6. native endpoint 不写 `format=table`。
7. GeoJSON 输出按 `format=json + spatial.target_encoding=geojson` 表达。
8. Shapefile 输出按 `format=shapefile` 表达，并通过 multi component writer 写出 `.shp/.shx/.dbf/.cpg/.prj` 等组件。
9. `policy.write_mode` 目前只保留 `overwrite` 和 `append`。是否先删、删什么，是 Transfer 策略；common engine 只提供删除指定资源的原子能力。
10. `transforms` 描述 source 和 target 之间的 table batch 转换，不属于 source / target endpoint。

旧字段 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等不再兼容，出现即拒绝。

### 3.1 field_mapping transform

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
| Parquet | 最小 `TableReaderProvider` 已有 | 最小 `TableWriterProvider` 已有 | 已能跑通基础 table transfer；row group / 分区数据集仍待增强。 |
| Shapefile | component table info / sample 读侧已接入 Transfer 主链路 | `ComponentTableWriterProvider` 已有 | multi component 读写已可用于 Transfer；后续只保留连续 stateful component reader / 大文件性能增强。 |

### 4.3 Transfer

| 能力 | 状态 |
|---|---|
| 新 endpoint JSON | 创建、编辑、详情、执行已切换到新结构。 |
| planner | 已能规划 native table、encoded table file/object、multi component export、native table import。 |
| executor | 已统一走 common engine / format / resource 能力。 |
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
| PostgreSQL spatial table -> NFS Shapefile | 已真实验收通过 | NFS root / Meta 扫描闭环已验证，item node、字段、行数和空间能力可被 Manager 看到。 |
| PostgreSQL spatial table -> MinIO Shapefile | 已真实验收通过 | 覆盖 geometry type、SRID、字段类型、组件文件、Meta scan 和 Manager preview。 |
| NFS Shapefile -> PostgreSQL table | 已真实验收通过 | Shapefile multi component 读侧可进入统一 table reader / writer 链路，PostgreSQL 目标优先 COPY session。 |
| MinIO Shapefile -> PostgreSQL table | 已真实验收通过 | 对象存储 multi component 读侧可导入 native table。 |
| NFS Shapefile -> MinIO Shapefile | 已手动验证通过 | multi component 正确生成；Meta deep scan 后可得到字段、行数、format info、spatial capabilities。 |

## 六、写后 Meta 扫描规则

Transfer 成功写出目标后，如果 `auto_scan_metadata=true`，必须立即触发 Meta deep scan。

扫描目标必须使用目标资源所在容器，而不是总是扫引擎根，也不是总是扫单文件：

| 目标类型 | 写出目标 | Meta 扫描目标 |
|---|---|---|
| PostgreSQL native table | `public.roads` | `public` |
| NFS 顶层文件 | `roads.csv` | `/` |
| NFS 子目录文件 | `exports/roads.csv` | `exports` |
| NFS Shapefile | `shp/a3.shp` + components | `shp` |
| MinIO object | `addp/exports/roads.csv` | `addp/exports` |
| MinIO Shapefile | `addp/gis/a3.shp` + components | `addp/gis` |

文件型引擎的特别规则见 [存储引擎路径体系规范](../spec/addp存储引擎路径体系规范.md)：NFS root `name="/"`，`full_name=""`，`path` 是 Meta 内部节点链，`.` 不进入业务路径。

## 七、职责边界

| 层 | 负责 | 不负责 |
|---|---|---|
| `common/engine` | 引擎连接、能力声明、catalog、metadata、content read/write、range read、table batch/session read/write、DeleteResource、query、stream / CDC 扩展点 | Transfer 任务、写入模式、调度、执行历史、重试策略 |
| `common/resource` | ResourceRef、ResourceReader、RangeReader、ComponentReader、ComponentWriter、多组件提交边界 | 格式解析、任务策略 |
| `common/format` | 格式身份、descriptor、capability view、data type reader / writer、编码解码、schema / sample / component 能力 | engine 连接、worker、任务记录 |
| `transfer` | 任务 JSON、planner、policy、field mapping / transform、worker、checkpoint、logs、metrics、写后 Meta scan | 具体 engine reader / writer、具体 format reader / writer |

边界判断原则：

1. 其他模块也会复用的能力，进入 common。
2. 只和一次 Transfer 任务策略有关的逻辑，留在 Transfer。
3. 追加 / 覆盖是 Transfer policy，不进入 common engine。
4. 删除指定资源是 common engine 原子能力。
5. 格式 reader / writer 只能属于 common format，不能回到 Transfer 私有插件。

## 八、仍然不足

| 方向 | 当前不足 |
|---|---|
| checkpoint / progress | 只有基础 records metrics，缺少 batch-level checkpoint、恢复和更细日志；这是下一阶段最高优先级之一。 |
| schema evolution | PostgreSQL 写侧可 ensure/create table，但字段变化、类型演进和目标表差异处理仍未完善。 |
| 并行读取 | PostgreSQL cursor session 已有，但分区并行读取、稳定快照和多 worker 协调仍未补。 |
| 其他数据库写侧 | MySQL、Doris、ClickHouse 等 common writer 仍待按真实需求补。 |
| Parquet 高性能 | 当前是最小 reader / writer，row group reader、predicate / projection、分区数据集读取仍待补。 |
| Shapefile 读取 | Transfer 主链路已验收通过；连续 stateful component reader、按批保持打开句柄、超大文件性能仍待增强。 |
| non-table data type | document / media / container / graph 还未形成 Transfer 主链路。 |
| stream / CDC | common engine 尚无稳定 `StreamReadableProvider`、`CDCReadableProvider`、change event / offset 标准。 |
| transform 扩展 | `field_mapping` 已进入主链路；过滤、派生字段、表达式、空间坐标转换等更完整 ETL transform 尚未设计。 |

## 九、下一步建议

### 近期优先级

1. **完善 checkpoint / progress / execution logs**  
   至少记录 batch 序号、累计行数、当前 source offset / cursor 状态、目标提交状态。

2. **增强 Shapefile 大文件读侧性能**  
   当前主链路已通过，但仍可把 component sample path 演进为 stateful component reader，避免每批重新打开 / 定位组件文件。

3. **补 Transfer 验收用例沉淀**  
   将已通过的 PostgreSQL spatial table -> NFS / MinIO Shapefile、NFS / MinIO Shapefile -> PostgreSQL table 形成可重复的集成测试或操作清单。

4. **设计下一批 transform 类型**  
   在 `field_mapping` 稳定后，再讨论过滤、派生字段、简单表达式、空间坐标转换等 ETL 能力边界。

### 中期优先级

1. Parquet row group reader / writer 增强，支持 projection、row group 级读取和分区数据集。
2. PostgreSQL schema evolution：字段新增、类型映射、目标表差异处理。
3. MySQL / Doris / ClickHouse 写侧 common writer，按真实链路逐个补，不一次性铺开。
4. document / media raw copy：先走 content reader / writer，不做格式转换。
5. container child table transfer：Excel sheet、SQLite table、GeoPackage layer 等按 child table 转出。

### 长期方向 

1. Kafka / stream：新增 stream event、partition / offset checkpoint。
2. CDC：新增 change event 抽象，支持 snapshot + incremental。
3. graph：明确 graph native export / query result table 化 / 子图导出三类路径。
4. 当第二个非 table data type 或 CDC 真正进入 executor 后，再讨论是否新增 `common/transferio` 或更通用的 RecordBatch。

## 十、压缩后的历史结论

以下内容不再展开保留，只保留结论：

- 旧任务 JSON 不兼容。
- 旧 local engines 不作为 Transfer 新入口。
- 旧 pipeline / plugins 不迁移为兼容层。
- 不新增 format 泛化 record reader / writer。
- 不为 native->native、file->object 等组合建立专用执行通道。
- GeoJSON 不是顶层 format，而是 JSON 的空间编码策略。
- Shapefile 是 multi component table format，Transfer 通过 component writer 写出。
- NFS root 是结构上必须存在、语义路径上为空、展示上可透明的节点；具体规范已移入 `docs/spec/addp存储引擎路径体系规范.md`。
