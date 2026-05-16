# ADDP 元数据扫描机制规范

本文定义 Meta 扫描的深度、目标、覆盖、跳过判断和跨模块触发规则。术语以 [ADDP 术语表](../concepts/addp术语表.md) 和 [ADDP 元数据体系图](../concepts/addp元数据体系图.md) 为准。

## 本文边界

本文负责：

| 本文负责 | 不在本文定义 |
|---|---|
| `scan_depth`、`scanned_depth`、`scan_status` 的语义 | data item 识别、claims、exclusive 规则 |
| `force=true/false` 的覆盖策略 | attributes 的完整 JSON schema |
| engine / node / item 扫描目标 | FormatPlugin、info provider、content reader 接口形态 |
| 手动 / 定时触发的默认组合 | Manager 前端 DTO 和预览 UI |
| 不强制扫描时的低成本跳过判断 | 具体格式 parser 的字段细节 |

data item 识别规则见 [ADDP 数据项探测器规范](addp数据项探测器规范.md)，attributes 写入规则见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)，格式与数据类型能力边界见 [ADDP 数据类型与格式能力规范](addp数据类型与格式能力规范.md)。

## 核心维度

Meta 扫描只保留四个核心维度：

| 维度 | 字段 | 取值 | 说明 |
|---|---|---|---|
| 触发方式 | `trigger_type` | `manual` / `scheduled` | 只区分手动和定时。System 立即扫描、Manager 刷新、Meta 前端手动扫描都属于 `manual`。 |
| 请求深度 | `scan_depth` | `basic` / `deep` | 本次扫描要达到的深度。 |
| 已扫深度 | `scanned_depth` | `none` / `basic` / `deep` | 当前 node / item 已经达到的扫描深度。 |
| 是否强制 | `force` | `true` / `false` | 是否不管已有元数据和时间戳都重新扫描。 |

扫描目标只抽象为三类：

1. engine
2. node
3. item

如果使用 locator，locator 本身已经能表达目标对象，不需要额外 `target_type`。

## 扫描编排依据

Meta 的扫描编排必须分开回答三类问题：

| 问题 | 事实源 |
|---|---|
| 目录层级如何组织、各层叫什么、哪一层是 item | `CatalogModelSpec` |
| 能否列目录、描述 item、采样字段、读取内容 | 已实现的 provider 组合 |
| Meta 需要怎样执行和落库 | Meta 自己的 scan strategy |

`engine_family` 只保留粗分类意义，不能单独决定扫描流程。Meta 可以因为执行语义不同而保留多种 strategy，例如：

- namespace/item 型：表格、文档、图等先扫描 namespace，再扫描 item。
- object catalog 型：对象存储按 bucket / prefix / object 模型扫描，可做复合对象聚合。
- file catalog 型：文件系统按 root / directory / file 模型扫描，可做复合文件聚合。

这些 strategy 的差异来自 catalog model 和 provider 语义，不等于为每个具体引擎重建一套上层抽象。新增引擎时，优先复用已有 strategy；只有当 `CatalogModelSpec` 与 provider 组合都无法表达真实差异时，才新增 strategy。

## Basic / Deep 边界

### Basic 扫描

`basic` 的目标是低成本建立资源目录和 data item 身份。原则上不打开 file/object 内容流。

允许写入：

- `meta_node` 树。
- `meta_item` 身份字段：`item_type`、`name`、`full_name`、`fingerprint`、`node_id`。
- `attributes.schema_version`。
- `attributes.storage` 中由 catalog 直接返回的事实，例如 bucket、path、name、size、etag、last_modified_at、content_type。
- `attributes.item` 中无需读取内容即可判断的事实，例如 `organization`、`data_type`、`format`。如果 `component_files`、`file_count`、`scope_exclusive` 可由 catalog、manifest 或路径规则直接判断，也可以写入；需要打开 file/object 内容才能判断的，一律归 deep。
- 轻量格式判断：扩展名、MIME、catalog 声明。若确实需要读取极小 header，必须有读取上限和明确理由。

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
- 大文件 deep 扫描应由 provider 自己控制成本和阈值，不能无边界全量读取。

`content_index` 纳入 deep 的默认目标。具体格式是否生成、是否因为文件过大而跳过，由对应 provider 决定；跳过不应阻断 deep 扫描完成。

## Scanned Depth

`meta_node` 和 `meta_item` 使用 `scanned_depth` 字段记录已完成扫描深度。

| 字段 | 位置 | 说明 |
|---|---|---|
| `scanned_depth` | `meta_item` | 当前 item 已达到的扫描深度。 |
| `scanned_depth` | `meta_node` | 当前 node 范围已达到的扫描深度。 |

`scan_status` 继续表达过程状态，例如 `pending`、`running`、`completed`、`failed`。不要把 `basic` / `deep` 混入 `scan_status`，否则“正在扫描”和“已扫深度”两个维度会互相覆盖。

`scanned_at` 表示最近一次扫描完成时间。第一阶段不新增 `basic_scanned_at` / `deep_scanned_at`，避免状态膨胀。

`scanned_depth` 更新规则：

| 本次 scan_depth | 成功后 item.scanned_depth |
|---|---|
| `basic` | `basic`，但如果原来已是 `deep`，保持 `deep` |
| `deep` | `deep` |

Basic 重新发现资源时不能把已有 deep 状态降级。

## 覆盖策略

只保留一个请求字段：

```json
{
  "force": false
}
```

语义：

- `force=true`：不管已有元数据和时间戳，重新扫描并覆盖本次深度对应的元数据。
- `force=false`：默认策略。已有且未过时就跳过；未达到目标深度或已过时就扫描。

不引入 `refresh_policy`、`if_stale`、provider version 等额外策略。解析器升级或规范调整后的重建由用户或运维通过 `force=true` 解决。

## 是否需要扫描

默认判断顺序：

1. 目标不存在：扫描。
2. `force=true`：扫描。
3. 请求 `scan_depth=deep`，但目标 `scanned_depth != deep`：扫描。
4. 请求 `scan_depth=basic`，但目标 `scanned_depth=none` 或为空：扫描。
5. 源数据时间晚于 `scanned_at`：扫描。
6. 源大小变化：扫描。
7. 其他情况跳过。

这个判断不是新的复杂状态机。实现落地时以 provider / catalog 能低成本提供的事实为准，`scanned_at` 是最近扫描完成时间，可用于展示和兜底判断，不要求所有 provider 都精确维护源端更新时间。

对象和文件：

- 优先使用 catalog 返回的 `last_modified_at` / `data_updated_at` 与 `scanned_at` 对比。
- 没有 `last_modified_at` 时，用 size 变化兜底。
- etag 可作为增强判断，但第一阶段不强依赖。

数据库：

- 如果插件能提供 table `last_modified`，用它和 `scanned_at` 对比。
- 如果不能提供 `last_modified`，用 row_count / size 等 catalog 可得事实做低成本辅助判断。
- 如果这些事实都不可得，不强制时默认跳过已有目标；用户需要重建时使用 `force=true`。

这个判断不追求数据库场景下的绝对精准，优先保持机制简单、成本可控。

## 扫描目标

扫描目标只保留 engine、node、item 三种抽象。

请求可以使用 engine_id：

```json
{
  "engine_id": 1,
  "scan_depth": "basic",
  "force": false
}
```

也可以使用 locator：

```json
{
  "targets": [
    "addp://engine/1/path/addp/shp/farmland.shp?type=object&meta_id=100"
  ],
  "scan_depth": "deep",
  "force": false
}
```

也可以使用明确 ID：

```json
{
  "item_id": 100,
  "scan_depth": "deep",
  "force": true
}
```

接口层可以支持 `engine_id`、`node_id`、`item_id` 或 `targets`，但进入扫描服务内部后统一转换为 locator 或内部 target 对象。

不需要 `target_type`。locator 已经包含 engine、path、type、meta_id；ID 字段本身也能区分 node / item。

## 默认组合

扫描调度和扫描深度是两个维度，可以自由组合。默认组合如下：

| 场景 | trigger_type | scan_depth | force |
|---|---|---|---|
| System 新建引擎后立即扫描 | `manual` | `basic` | `false` |
| System 定时扫描 | `scheduled` | `deep` | `false` |
| Meta 前端手动扫描 | `manual` | 第一阶段固定 `deep`；API 支持用户选择 | 第一阶段固定 `false`；API 支持用户选择 |
| Meta 前端定时扫描 | `scheduled` | 用户选择，默认 `deep` | 默认 `false` |
| Manager 刷新按钮 | `manual` | `deep` | `true` |
| Manager 预览前 deep 补齐 | `manual` | `deep` | `false` |
| Transfer 完成后触发 | `manual` | `basic` 或按导入结果决定 | `false` |

`manual` 可以在执行配置中额外记录来源，例如 `source=system_immediate`、`source=manager_refresh`、`source=meta_frontend`，用于审计和排查。但来源不进入 trigger type 枚举。

## Manager 边界

Manager 不应判断各类格式内部需要哪些 deep 字段，否则会和 Meta、format provider 过耦合。

Manager 预览 item 前的 deep 补齐流程：

1. Manager 预览 item 前读取 item。
2. 如果 `item.scanned_depth != deep`，调用 Meta Client 对该 item 发起 `scan_depth=deep, force=false`。
3. Meta 内部判断是否需要真正扫描。
4. 扫描完成后 Manager 重新读取 item 并继续预览。

如果 `item.scanned_depth=deep`，Manager 直接预览，不检查 Shapefile、CSV、Excel 等具体 attributes。

Manager 刷新按钮固定：

```json
{
  "scan_depth": "deep",
  "force": true
}
```

Manager 刷新目标必须是当前选中的 engine / node / item，不能默认全 engine。

| 前端选中对象 | 扫描目标 |
|---|---|
| engine root | engine |
| schema / database / bucket / prefix / root / dir | node |
| table / collection / label / relationship / object / file | item |

对于 item 目标，Meta 内部负责确定最小扫描上下文：

- single item：扫描该 item。
- multi item：根据 `component_files` 或父 node 构造组件上下文。
- whole item：扫描 whole scope。

这个复杂度留在 Meta 内部，Manager 不关心。

## API 约束

统一请求模型：

```json
{
  "engine_id": 1,
  "node_id": 0,
  "item_id": 0,
  "targets": [],
  "scan_depth": "deep",
  "force": false,
  "trigger_type": "manual"
}
```

规则：

- `scan_depth` 只允许 `basic` / `deep`。
- `force` 默认为 `false`。
- 扫描请求语义只区分 `manual` / `scheduled`。公共 `task_executions.trigger_type` 现阶段仍沿用 common 的既有枚举和值，后续再统一收敛。
- `engine_id`、`node_id`、`item_id`、`targets` 至少提供一种。
- 不保留 `full` / `shallow`。
- 第一阶段继续使用现有 `POST /scan/engine` 入口，但不保留旧 depth 和旧响应兼容语义；后续可以再收敛为更准确的路径命名。
