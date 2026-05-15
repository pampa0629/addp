# Meta 扫描深度、刷新与覆盖机制设计

更新时间：2026-05-15

本文是讨论稿，用于在动工前统一 Meta 扫描的几个核心维度：

1. `basic` / `deep` 的职责边界。
2. 手动、定时、刷新等触发方式与扫描深度的关系。
3. engine / node / item 级扫描目标表达。
4. 已有元数据的跳过、过期判断与强制覆盖策略。

## 背景

现有规范已经明确：Meta 负责从引擎资源树中识别 data item，生成 `meta_node`、`meta_item` 和标准 attributes。`basic` 扫描应快速发现资源树和 data item；`deep` 扫描补充类型信息和横切事实。

当前实现中存在几个不一致：

1. `POST /scan/engine` 解析了 `scan_depth`，但实际调用固定 deep 扫描；`common/client.MetaClient.TriggerScanEngine()` 发送 `basic` 也不会生效。
2. `meta` 定时任务、手动任务、同步扫描入口、Manager 刷新入口的默认深度不一致。
3. Manager 刷新节点时，表 / schema 会转为 namespace 扫描，但对象存储、文件系统、prefix、file、object 通常退化为全 engine 扫描。
4. 对象和文件 basic 扫描仍可能读取文件内容提取字段或容器 children，这和“basic 不读取 file/object 内部数据”的目标冲突。
5. 是否重复扫描、是否覆盖已有 deep 元数据，目前主要由局部 `ShouldUpdate*` 判断和 basic 保留旧 attributes 的实现隐式决定，没有统一的用户可见策略。

## 概念拆分

扫描请求应拆成四个独立维度，不再互相暗含：

| 维度 | 示例 | 说明 |
|---|---|---|
| 触发方式 `trigger_type` | `manual`、`scheduled`、`auto`、`refresh` | 谁触发、何时触发，用于审计、任务队列、默认策略。 |
| 扫描深度 `scan_depth` | `basic`、`deep` | 本轮要采集多少事实。 |
| 扫描目标 `targets` | engine、node、item、namespace、object_path | 扫描范围，不由触发方式推断。 |
| 覆盖策略 `refresh_policy` | `skip_existing`、`if_stale`、`force` | 遇到已有元数据时如何处理。 |

这四个维度可以组合使用。例如：

- System 注册后立即扫描：`auto + basic + engine + if_stale`
- System 定时扫描：`scheduled + deep + configured targets + if_stale`
- Meta 前端批量扫描：`manual + user selected depth + selected targets + user selected policy`
- Manager 刷新按钮：`refresh/manual + deep + selected node/item + force`

## Basic / Deep 边界

### Basic 扫描

`basic` 的目标是低成本建立资源目录和 data item 身份。原则上不打开 file/object 内容流。

允许写入：

- `meta_node` 树。
- `meta_item` 身份字段：`item_type`、`name`、`full_name`、`fingerprint`、`node_id`。
- `attributes.schema_version`。
- `attributes.storage` 中由 catalog 直接返回的事实，例如 bucket、path、name、size、etag、last_modified_at、content_type。
- `attributes.item` 中无需读取内容即可判断的事实，例如 `organization`、`data_type`、`format`、`component_files`、`file_count`、`scope_exclusive`。
- 轻量格式判断：扩展名、MIME、catalog 声明、必要时极小 header，但必须有读取上限和明确理由。

不应写入：

- 表字段、行数、主键、索引。
- Shapefile / CSV / Parquet 等文件内部 schema。
- 容器 children，例如 Excel sheet、SQLite table、GeoPackage layer、ZIP entry。
- `content_index`。
- 文档正文、媒体宽高、图片 EXIF、全文摘要、向量索引引用。
- 为了获取 extent、真实 geometry 类型分布而扫描全量数据。

### Deep 扫描

`deep` 的目标是补充 Manager、Transfer、Asset、Search 等模块需要稳定消费的元数据事实。

允许写入：

- `type_info.table.fields`、`row_count`、`primary_key`、`indexes`。
- `type_info.container.children`。
- `type_info.document`、`type_info.media`、`type_info.graph`。
- `format_info.<format>`。
- `capabilities.spatial`、`statistics`、`extraction`、`semantic`、`partitioning`、`indexing`。
- `content_index`，例如 CSV 稀疏行索引。

Deep 扫描可以读取内容，但仍应遵守 provider / reader 边界：

- 元数据事实通过 info provider 写入 `type_info` / `format_info` / `capabilities`。
- 内容窗口、样本、原始文本、缩略图不直接塞进 `type_info`。
- 大文件 deep 扫描应按 format capability 设计分层能力，不能无边界全量读取。

## Deep 可用性

Manager 预览某些数据项时必须知道 deep 事实是否存在，例如：

- Shapefile 需要字段、geometry column、SRID 等信息。
- CSV / Parquet 需要字段和分页读取能力。
- 容器文件需要 children 列表。

建议 Meta 明确输出 item 的 deep 状态，而不是让 Manager 猜 attributes 是否完整。

建议新增扫描状态模型：

| 字段 | 建议位置 | 说明 |
|---|---|---|
| `basic_scanned_at` | `meta_item` | 最近一次 basic 身份扫描时间。 |
| `deep_scanned_at` | `meta_item` | 最近一次 deep 元数据扫描时间；为空表示 deep 未完成。 |
| `scan_level` | `meta_item` 或查询 DTO | 当前 item 已达到的最高扫描层级：`basic` / `deep`。 |
| `source_signature` | `meta_item` | 当前资源 catalog 事实签名。 |
| `deep_source_signature` | `meta_item` | deep 元数据基于哪个 source signature 生成。 |

如果短期不改表结构，可以先在 `MetaItemLite` DTO 中派生：

- `scan_level = deep`：存在 deep 必需分区，例如 `type_info.table.fields`、`type_info.container.children`、`capabilities.spatial` 等。
- `scan_level = basic`：只有 storage / item 基础事实。

但长期不建议只靠 attributes 反推，因为不同 data type 的 deep 必需事实不同，且难以表达“deep 已尝试但 provider 不支持”。

## Manager 即时 Deep 补齐

Manager 预览流程需要一个明确机制：

1. Manager 读取 item。
2. 判断 item 是否满足当前预览 provider 的 deep 依赖。
3. 如果不满足，调用 Meta Client 对该 item 或其所在最小范围发起 `deep + force/if_stale` 扫描。
4. 等待扫描完成或返回可轮询执行 ID。
5. 重新读取 item 后继续预览。

建议提供能力判断接口：

```text
GET /api/v1/meta/items/{item_id}/scan-state
```

或在 `GET /items/{item_id}` 中返回：

```json
{
  "scan_level": "basic",
  "deep_scanned_at": null,
  "deep_required_for": ["table_preview", "spatial_preview"],
  "stale": false,
  "stale_reason": ""
}
```

Manager 可按 preview provider 声明依赖，例如：

| 预览类型 | deep 依赖 |
|---|---|
| database table | `type_info.table.fields`，空间预览还需要 `capabilities.spatial` |
| CSV / TSV | `type_info.table.fields`，大文件分页需要 `content_index.table` |
| Shapefile | `type_info.table.fields` + `capabilities.spatial` + `item.component_files` |
| Excel / SQLite / GeoPackage | `type_info.container.children` |
| PDF / image / media | `type_info.document` 或 `type_info.media`，具体按 provider 能力 |

## 扫描目标

扫描目标必须显式表达，不再由入口猜测。

建议请求模型：

```json
{
  "engine_id": 1,
  "trigger_type": "manual",
  "scan_depth": "deep",
  "refresh_policy": "if_stale",
  "targets": [
    {
      "target_type": "node",
      "locator": "addp://engine/1/path/addp/shp?type=prefix&meta_id=12"
    }
  ]
}
```

兼容期可以保留现有字段：

- `namespaces`
- `object_paths`

但内部应尽快归一为 `targets`：

| target_type | 适用对象 | 转换规则 |
|---|---|---|
| `engine` | 整个引擎 | 全部可扫描根。 |
| `namespace` | schema / database | tabular、document、graph 引擎使用。 |
| `object_path` | bucket / prefix / object | MinIO / S3 使用。 |
| `file_path` | root / dir / file | NFS / 文件系统使用。 |
| `node` | 已入库 meta_node | 通过 node full_name 和 node_type 转换为 namespace/path。 |
| `item` | 已入库 meta_item | 通过 item full_name 和 item_type 转换为最小扫描范围。 |

Manager 刷新映射建议：

| Locator 类型 | 扫描目标 |
|---|---|
| engine root | `target_type=engine` |
| schema / database | `target_type=namespace` |
| table / collection / label / relationship | 所属 namespace，未来可优化到 item |
| bucket | `target_type=object_path`，值为 bucket |
| prefix | `target_type=object_path`，值为 bucket/prefix |
| object | `target_type=item` 或 object 所在父 prefix |
| root / dir | `target_type=file_path` |
| file | `target_type=item` 或 file 所在父 dir |

## 覆盖策略

建议把“是否重复扫描”显式做成扫描请求的 `refresh_policy`，而不是用不同入口写死。

| 策略 | 含义 | 典型入口 |
|---|---|---|
| `skip_existing` | 已有对应层级元数据则跳过；只发现新增/删除。 | 大范围定时扫描、低成本巡检。 |
| `if_stale` | 仅当源资源签名变化或 deep 事实缺失时重扫。 | 默认策略。 |
| `force` | 无论是否变化，都重建本轮深度对应的 attributes 并覆盖。 | Manager 刷新按钮、用户显式强制刷新。 |

推荐默认值：

| 入口 | 默认 trigger | 默认 depth | 默认 policy |
|---|---|---|---|
| System 新建引擎立即扫描 | `auto` | `basic` | `if_stale` |
| System 定时扫描 | `scheduled` | `deep` | `if_stale` |
| Meta 前端手动扫描 | `manual` | 用户选择，默认 `basic` 或沿用表单 | `if_stale`，提供“强制重新扫描”开关 |
| Meta 前端定时任务 | `scheduled` | 用户选择 | `if_stale` |
| Manager 刷新按钮 | `refresh` / `manual` | `deep` | `force` |
| Transfer 完成后触发 | `auto` | `basic` 或按导入结果决定 | `if_stale` |

Meta 前端是否暴露强制覆盖：

- 建议默认不强制，使用 `if_stale`。
- 在手动扫描确认框或高级选项中提供“强制重新生成元数据”开关。
- 对定时任务不建议默认强制；强制定时深扫对大对象存储和大文件系统成本不可控。
- 对单个 node / item 可允许用户显式 force；对 engine 级 force 应有二次确认和成本提示。

## 过期判断

过期判断应区分“item 身份是否变化”和“deep 事实是否过期”。

### Source signature

`source_signature` 是源资源当前事实的稳定摘要，不是 item fingerprint。

- `fingerprint`：item 身份，通常基于 `engine_id + full_name`。
- `source_signature`：资源版本，基于 catalog / storage 可观察事实。

建议按引擎类型计算：

| 引擎 | source signature 输入 |
|---|---|
| MinIO / S3 object | bucket、path、size、etag、last_modified_at、content_type。etag 优先，mtime/size 兜底。 |
| NFS / file | full_name、size、mtime、inode/file_id（如可得）、content_type。mtime/size 兜底。 |
| PostgreSQL / MySQL / Doris / ClickHouse table | schema、table、catalog version（如可得）、column signature、row_count、size、last_modified（如可得）。 |
| MongoDB collection | database、collection、document_count、size、index signature、sample schema signature。 |
| Neo4j label / relationship | database、type、count、property schema signature。 |
| multi item | 主资源 signature + 组件列表 signature。 |
| whole scope item | scope 根 + manifest signature；无 manifest 时用成员列表摘要，但要避免全量昂贵计算。 |

### Stale 判定

建议判定顺序：

1. item 不存在：需要扫描。
2. `refresh_policy=force`：需要扫描。
3. 请求 `deep`，但 `deep_scanned_at` 为空：需要扫描。
4. 当前 `source_signature != deep_source_signature`：deep 事实过期，需要 deep。
5. 当前 `source_signature != source_signature` 的旧值：basic 身份或 storage 事实过期，需要 basic。
6. provider 版本变化：相关 deep 事实可能过期，需要 deep。
7. attributes schema_version 低于当前版本：需要重建。
8. 其他情况跳过。

### Provider version

只看源文件是否变化还不够。解析器升级、字段标准化规则调整、attributes schema 调整，也会让旧 deep 元数据过期。

建议记录：

```json
{
  "metadata_version": {
    "attributes_schema_version": 1,
    "detector_version": "builtin-2026-05",
    "format_provider": {
      "format": "shapefile",
      "version": "builtin-2026-05"
    }
  }
}
```

这类运行状态不建议长期放进业务 attributes 顶层。可以作为 `meta_item` 操作列或内部元数据列；如果短期放 attributes，也应放在受控命名空间并在规范中补充。

## API 调整建议

### 统一扫描入口

建议保留两个入口：

1. 异步入口：`POST /api/v1/meta/scan/run/manual`
2. 任务入口：`POST /api/v1/meta/scan/tasks/:task_id/trigger`

`POST /scan/engine` 可作为兼容入口，但必须：

- 尊重 `scan_depth`。
- 支持 `refresh_policy`。
- 支持 `object_paths` / `file_paths`。
- 长期标记为兼容入口，鼓励前端使用异步入口。

### Meta Client

`common/client.MetaClient` 应提供更完整的方法：

```go
type ScanOptions struct {
    EngineID      uint
    ScanDepth     string
    RefreshPolicy string
    Namespaces    []string
    ObjectPaths   []string
    FilePaths     []string
    Targets       []ScanTarget
}

TriggerScan(ctx context.Context, opts ScanOptions) (*TaskExecution, error)
EnsureDeepScanned(ctx context.Context, itemID uint, policy string) (*TaskExecution, error)
GetItemScanState(ctx context.Context, itemID uint) (*ItemScanState, error)
```

旧 `TriggerScanEngine(engineID, namespaces)` 可以保留一段时间，但内部转为：

```text
scan_depth=basic
refresh_policy=if_stale
targets=namespace 或 engine
```

### 前端

Meta 前端：

- 手动扫描增加 `scan_depth` 选择。
- 增加“强制重新生成元数据”高级开关，对应 `refresh_policy=force`。
- 默认 `refresh_policy=if_stale`。
- 对 engine 级 force 给二次确认。

Manager 前端：

- 保持一个刷新按钮。
- 固定 `scan_depth=deep`。
- 固定 `refresh_policy=force`。
- 刷新目标必须是当前 locator 对应的最小范围，不能默认全 engine。

System 前端：

- 新建引擎默认：立即扫描开启，深度 `basic`。
- 定时扫描默认：开启时深度 `deep`。
- 用户可以分别修改立即扫描深度和定时扫描深度。

## 实施顺序建议

1. 修正文档和 DTO：把 `scan_depth` 从 `basic/deep/full` 收敛为 `basic/deep`，删除 `full` UI 选项和旧 `shallow` 入口描述。
2. 为扫描请求增加 `refresh_policy`，并在执行配置中持久化。
3. 修复 `/scan/engine` 忽略 `scan_depth` 的问题，或将其改为异步入口的兼容封装。
4. 扩展 `MetaClient`，支持 `scan_depth`、`refresh_policy`、`object_paths`、`targets`。
5. 修 Manager `RefreshNode` 的 locator 到目标转换，默认 `deep + force`。
6. 收敛 basic：禁止 file/object basic 打开内容流；将字段、children、content_index 全部移到 deep。
7. 增加 item deep 状态和 source signature。
8. 增加过期判断和 provider version 判断。
9. 优化 Meta tree 查询，避免 Manager 每次全量拉树。

## 待确认问题

1. `deep_scanned_at`、`source_signature`、`deep_source_signature` 放在 `meta_item` 独立列，还是先放在内部 attributes 命名空间过渡？
2. `Manager` 刷新 object / file 时，是扫描单 item，还是扫描父 prefix / dir？单 item 成本最低，但 multi 组件和 whole scope 需要 detector 上下文。
3. 对 `content_index` 是否纳入 deep 默认行为？大 CSV 默认建 index 可能成本较高，也可以作为 `deep + content_index` 的子能力。
4. 定时扫描默认 `if_stale` 时，是否仍需要全量列举目录以发现删除？对象存储和文件系统要区分“发现删除”的 basic 巡检与“重建 deep”的 deep 扫描。
5. Provider version 如何命名和递增，是否由 `common/format` descriptor 暴露？
