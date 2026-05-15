# Meta 扫描深度、刷新与覆盖机制设计

更新时间：2026-05-15

本文是讨论稿，用于在动工前统一 Meta 扫描的核心语义。当前目标是保持机制足够简单，不引入不必要的状态、策略和判断逻辑。

## 背景

现有规范已经明确：Meta 负责从引擎资源树中识别 data item，生成 `meta_node`、`meta_item` 和标准 attributes。`basic` 扫描应快速发现资源树和 data item；`deep` 扫描补充类型信息和横切事实。

当前实现中存在几个不一致：

1. `POST /scan/engine` 解析了 `scan_depth`，但实际调用固定 deep 扫描；`common/client.MetaClient.TriggerScanEngine()` 发送 `basic` 也不会生效。
2. `meta` 定时任务、手动任务、同步扫描入口、Manager 刷新入口的默认深度不一致。
3. Manager 刷新节点时，表 / schema 会转为 namespace 扫描，但对象存储、文件系统、prefix、file、object 通常退化为全 engine 扫描。
4. 对象和文件 basic 扫描仍可能读取文件内容提取字段或容器 children，这和“basic 不读取 file/object 内部数据”的目标冲突。
5. 是否重复扫描、是否覆盖已有 deep 元数据，目前主要由局部 `ShouldUpdate*` 判断和 basic 保留旧 attributes 的实现隐式决定，没有统一的用户可见策略。
6. 前后端仍有 `full`、`shallow`、旧 `/api/meta` 代理预期等遗留概念，后续不保留兼容。

## 核心结论

扫描机制只保留四个简单维度：

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

## 术语

本文统一使用：

- `scan_depth`：请求参数，表示本次扫描目标深度。
- `scanned_depth`：落库状态，表示当前元数据已经达到的扫描深度。

不再使用 `scan_level`、`deep state`、`refresh_policy`、`if_stale` 等额外术语。

## Basic / Deep 边界

### Basic 扫描

`basic` 的目标是低成本建立资源目录和 data item 身份。原则上不打开 file/object 内容流。

允许写入：

- `meta_node` 树。
- `meta_item` 身份字段：`item_type`、`name`、`full_name`、`fingerprint`、`node_id`。
- `attributes.schema_version`。
- `attributes.storage` 中由 catalog 直接返回的事实，例如 bucket、path、name、size、etag、last_modified_at、content_type。
- `attributes.item` 中无需读取内容即可判断的事实，例如 `organization`、`data_type`、`format`、`component_files`、`file_count`、`scope_exclusive`。
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

`content_index` 第一阶段纳入 deep 的默认目标。具体格式是否生成、是否因为文件过大而跳过，由对应 provider 决定；跳过不应阻断 deep 扫描完成。

## Scanned Depth

建议在 `meta_node` 和 `meta_item` 增加 `scanned_depth` 字段。

| 字段 | 位置 | 说明 |
|---|---|---|
| `scanned_depth` | `meta_item` | 当前 item 已达到的扫描深度。 |
| `scanned_depth` | `meta_node` | 当前 node 范围已达到的扫描深度，可由扫描过程直接写入，也可由子项汇总。 |

`scan_status` 继续表达过程状态，例如 `pending`、`running`、`completed`、`failed`。不要把 `basic` / `deep` 混入 `scan_status`，否则“正在扫描”和“已扫深度”两个维度会互相覆盖。

虽然 `meta_node` 当前已有扫描状态字段，但不建议直接扩展为“已基础扫描 / 已深度扫描”。原因是 `scan_status` 描述的是任务过程结果，`scanned_depth` 描述的是元数据完整度。一个 item 可以同时处于“最近一次任务失败”和“历史上已经 deep 完成”的状态，如果混成一个枚举，会丢失信息。

`scanned_at` 继续表示最近一次扫描完成时间。第一阶段不新增 `basic_scanned_at` / `deep_scanned_at`，避免状态膨胀。

`scanned_depth` 更新规则：

| 本次 scan_depth | 成功后 item.scanned_depth |
|---|---|
| `basic` | `basic`，但如果原来已是 `deep`，保持 `deep` |
| `deep` | `deep` |

这样可以保证 basic 重新发现资源时不会把已有 deep 状态降级。

## 是否需要扫描

只保留一个请求字段：

```json
{
  "force": false
}
```

语义：

- `force=true`：不管已有元数据和时间戳，重新扫描并覆盖本次深度对应的元数据。
- `force=false`：默认策略。已有且未过时就跳过；未达到目标深度或已过时就扫描。

默认判断顺序：

1. 目标不存在：扫描。
2. `force=true`：扫描。
3. 请求 `scan_depth=deep`，但目标 `scanned_depth != deep`：扫描。
4. 请求 `scan_depth=basic`，但目标 `scanned_depth=none` 或为空：扫描。
5. 源数据时间晚于 `scanned_at`：扫描。
6. 源大小变化：扫描。
7. 其他情况跳过。

对象和文件：

- 优先使用 catalog 返回的 `last_modified_at` 与 `scanned_at` 对比。
- 没有 `last_modified_at` 时，用 size 变化兜底。
- etag 可作为增强判断，但第一阶段不强依赖。

数据库：

- 如果插件能提供 table `last_modified`，用它和 `scanned_at` 对比。
- 如果不能提供 `last_modified`，用 row_count / size 等 catalog 可得事实做低成本辅助判断。
- 如果这些事实都不可得，不强制时默认跳过已有目标；用户需要重建时使用 `force=true`。
- 不引入 provider version。解析器升级或规范调整后的重建由用户或运维通过强制扫描解决。

这个判断不追求数据库场景下的绝对精准，优先保持机制简单、成本可控。

## 默认组合

扫描调度和扫描深度是两个维度，可以自由组合。默认建议如下：

| 场景 | trigger_type | scan_depth | force |
|---|---|---|---|
| System 新建引擎后立即扫描 | `manual` | `basic` | `false` |
| System 定时扫描 | `scheduled` | `deep` | `false` |
| Meta 前端手动扫描 | `manual` | 用户选择 | 用户选择，默认 `false` |
| Meta 前端定时扫描 | `scheduled` | 用户选择 | 默认 `false` |
| Manager 刷新按钮 | `manual` | `deep` | `true` |
| Manager 预览前 deep 补齐 | `manual` | `deep` | `false` |
| Transfer 完成后触发 | `manual` | `basic` 或按导入结果决定 | `false` |

`manual` 可以在执行配置中额外记录来源，例如 `source=system_immediate`、`source=manager_refresh`、`source=meta_frontend`，用于审计和排查。但来源不进入 trigger type 枚举。

## Manager 即时 Deep 补齐

Manager 不应判断各类格式内部需要哪些 deep 字段，否则会和 Meta、format provider 过耦合。

建议最简单流程：

1. Manager 预览 item 前读取 item。
2. 如果 `item.scanned_depth != deep`，调用 Meta Client 对该 item 发起 `scan_depth=deep, force=false`。
3. Meta 内部判断是否需要真正扫描。
4. 扫描完成后 Manager 重新读取 item 并继续预览。

如果 `item.scanned_depth=deep`，Manager 直接预览，不检查 Shapefile、CSV、Excel 等具体 attributes。

Manager 刷新按钮则固定：

```json
{
  "scan_depth": "deep",
  "force": true
}
```

这个流程不需要新增专门的 scan-state API。只需现有 `GET /items/:item_id` 返回 `scanned_depth`，Manager 后端即可自动处理。

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

Manager 刷新映射：

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

## API 调整建议

### 扫描请求

建议统一请求模型：

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
- `trigger_type` 只允许 `manual` / `scheduled`。
- `engine_id`、`node_id`、`item_id`、`targets` 至少提供一种。
- 不保留 `full` / `shallow`。
- 不保留旧的路径和响应兼容语义。

### Meta Client

`common/client.MetaClient` 建议收敛为：

```go
type ScanOptions struct {
    EngineID uint
    NodeID   uint
    ItemID   uint
    Targets  []string

    ScanDepth   string
    Force       bool
    TriggerType string
}

TriggerScan(ctx context.Context, opts ScanOptions) (*TaskExecution, error)
EnsureItemDeepScanned(ctx context.Context, itemID uint) (*TaskExecution, error)
ForceRefreshItem(ctx context.Context, itemID uint) (*TaskExecution, error)
ForceRefreshNode(ctx context.Context, nodeID uint) (*TaskExecution, error)
```

`EnsureItemDeepScanned` 等价于：

```text
item_id=<id>, scan_depth=deep, force=false, trigger_type=manual
```

`ForceRefreshItem` 等价于：

```text
item_id=<id>, scan_depth=deep, force=true, trigger_type=manual
```

### 前端

Meta 前端：

- 手动扫描提供 `scan_depth` 选择。
- 手动扫描提供“强制重新扫描”开关，对应 `force=true`。
- 默认 `force=false`。
- engine 级 `force=true` 需要二次确认。
- 定时任务允许设置 `scan_depth`，默认 `deep`，`force` 默认 `false`。

Manager 前端：

- 保持一个刷新按钮。
- 固定 `scan_depth=deep`。
- 固定 `force=true`。
- 刷新目标必须是当前选中的 engine / node / item，不能默认全 engine。

System 前端：

- 新建引擎默认：立即扫描开启，`scan_depth=basic`。
- 定时扫描默认：开启时 `scan_depth=deep`。
- 用户可以分别修改立即扫描深度和定时扫描深度。

## 实施顺序建议

1. 修正文档和 DTO：`scan_depth` 只允许 `basic` / `deep`，删除 `full` UI 选项和旧 `shallow` 描述。
2. 在 `meta_node`、`meta_item` 增加 `scanned_depth` 字段。
3. 扫描请求增加 `force`，执行配置持久化 `scan_depth`、`force`、`trigger_type`。
4. 修复 `/scan/engine` 忽略 `scan_depth` 的问题；如果保留该入口，也必须支持 `force`。
5. 扩展 `MetaClient`，支持 engine / node / item / locator 目标、`scan_depth` 和 `force`。
6. 修 Manager `RefreshNode`：按 locator 映射到 engine / node / item，默认 `deep + force`。
7. 实现 Manager 预览前 deep 补齐：只判断 `item.scanned_depth != deep`，不判断格式内部 attributes。
8. 收敛 basic：禁止 file/object basic 打开内容流；字段、children、content_index 全部移到 deep。
9. 实现默认跳过逻辑：不强制时按 `scanned_depth`、`scanned_at`、源时间和 size 判断是否需要扫描。
10. 优化 Meta tree 查询，避免 Manager 每次全量拉树。

## 已确认取舍

1. Manager 刷新 item 时，目标就是 item；multi / whole 所需上下文由 Meta 内部解决。
2. `content_index` 纳入 deep 默认行为，但 provider 可以因不支持或成本过高跳过。
3. 不引入 provider version。解析器升级或规范调整后的重建通过 `force=true` 解决。
4. 不引入单独 scan-state API。`GET /items/:item_id` 返回 `scanned_depth` 即可。
5. 不保留兼容。旧 `full`、`shallow`、旧路径和旧响应结构应直接清理。
