# Manager 快显现状调研和问题记录

> 状态：备查记录。最高概念原则以 [Manager 快显与瓦片缓存概念原则](Manager快显与瓦片缓存概念原则.md) 为准。本文只记录当前 MVT / PostGIS 快显实现现状、历史问题和后续清理线索，不作为概念或规范来源。

## 一、现状摘要

当前 Manager 中的空间快显实现主要围绕 PostGIS + MVT：

1. 空间表可以通过 GeoJSON 或 MVT 进入地图预览。
2. 瓦片请求入口会先查内存、Redis、MinIO，未命中时再从 PostGIS 实时生成。
3. Quick View 只提供状态查询和 preferred mode 更新；瓦片缓存生成通过 `tile_cache_generation` 任务执行。
4. 当前 PostGIS + MVT 阶段由 Manager Backend 内部执行 `tile_cache_generation`，准备检查作为任务内部步骤进入 execution metadata；不保留独立 Manager Worker 空运行时。
5. `manager.tile_cache_tasks` 当前作为可编排任务定义，执行时写入 `common.task_executions`。

这些是当前实现事实，不代表概念边界。当前已按瓦片缓存生成语义收敛，MVT 只作为第一阶段瓦片格式。

## 二、当前主要问题

### 1. 快显、瓦片缓存和任务混杂

历史实现中 Quick View 有时被称为任务，有时又承担产物状态职责。

当前已收敛为：

1. 快显是空间预览中的显示模式。
2. 瓦片缓存是支撑快显的产物。
3. 瓦片缓存生成是任务能力。
4. 执行记录只表达某一次生成过程。

### 2. MVT-only 命名过重

历史代码、路由、任务表和文档大量使用 MVT 作为核心命名。

问题：

1. MVT 只是当前 PostGIS 路径下最方便的格式。
2. 未来可能存在栅格瓦片、预渲染瓦片或其他瓦片格式。
3. 任务体系不应为每种瓦片格式新增一套平行概念。

当前实现：`tile_cache_generation` / `manager.tile_cache_tasks` 表达瓦片缓存生成任务语义，MVT 进入 `config.tile.format`。

### 3. 存储位置假设过窄

第一阶段实现仍把瓦片缓存写入 ADDP 对象存储，但上层事实源已经改为 `storage_ref`。

问题：

1. 瓦片缓存体积可能较大。
2. 用户可能希望将缓存放在指定存储中复用。
3. 快显状态必须能记录 item 与缓存产物的关联，不能假设固定 bucket 或固定路径。

当前实现：产物状态记录 `storage_ref`、格式、版本、范围和层级；目标存储选择的多 provider 形态后续进入独立专题，不作为旧 MinIO 路径兼容分支保留。

### 4. 数据源加工边界需要显式化

当前准备阶段可能创建物化视图、空间索引并执行 `ANALYZE`。

这类动作不应简单理解为破坏性修改业务数据源，而应理解为显式的数据源加工或派生准备产物。

当前约束：

1. 不修改原始业务记录。
2. 不改写原始几何列。
3. 不删除原始业务数据。
4. 不在普通预览或普通瓦片请求中隐式执行。
5. 必须由用户操作或任务定义显式授权。

### 5. 产物状态与执行状态未完全分层

历史实现中 `quick_view.status`、旧队列状态、`common.task_executions.status` 和瓦片缓存结果状态有混用风险。

当前已明确：

1. Quick View State 表达当前快显是否可用。
2. Tile Cache Artifact State 表达瓦片缓存结果是否可用、存在哪里、由什么配置生成。
3. `common.task_executions` 表达某一次生成执行。
4. Asynq Job 只是内部队列作业，不作为长期事实源。

瓦片缓存结果状态已经独立建模，见 [Manager 瓦片缓存结果状态设计](Manager瓦片缓存结果状态设计.md)。

## 三、当前实现观察

### 1. 路由和 API

当前已有能力包括：

1. `tasks/tile_cache_generation/{id}/execute`
2. `quick-view/capability`
3. `quick-view/preferred-mode`
4. `quick-view/tiles/{z}/{x}/{y}.mvt`
5. `tile_cache_tasks`
6. `tile_cache`

当前边界：

1. `quick-view/capability` 和 `quick-view/preferred-mode` 属于快显能力和显示偏好。
2. `tasks/tile_cache_generation/{id}/execute` 和 `tile_cache_tasks` 属于瓦片缓存生成任务。
3. `tile_cache` 属于瓦片缓存结果状态。
4. MVT 瓦片读取统一收敛到 `quick-view/tiles/{z}/{x}/{y}.mvt?locator=...`，不表达任务语义。

### 2. `quick_view` 表

历史 `manager.quick_view` 同时承担了快显状态、MVT 缓存摘要、准备状态、显示偏好等职责。

当前表职责：

1. `manager.quick_view` 保留为快显状态表，只表达快显能力、推荐结果和 UI 偏好。
2. 瓦片缓存结果状态拆出为独立的 `manager.tile_cache`。
3. `item_id` 和 `locator` 用于定位源对象。
4. 存储位置记录在瓦片缓存结果的 `storage_ref` 中。
5. 瓦片格式和产物版本记录在瓦片缓存结果状态中。

### 3. `manager.tile_cache_tasks`

当前 `manager.tile_cache_tasks` 是瓦片缓存生成任务定义表。

当前实现：

1. 任务语义为瓦片缓存生成。
2. 任务配置中包含瓦片格式。
3. 第一阶段目标存储通过生成得到的 `storage_ref` 表达。
4. 准备检查作为任务内部步骤进入 execution metadata。
5. 任务执行后更新瓦片缓存结果状态和快显状态。

目标任务类型为 `tile_cache_generation`，目标任务定义表为 `manager.tile_cache_tasks`。任务表命名必须带 `tasks`，见 [Manager 瓦片缓存生成任务设计](Manager瓦片缓存生成任务设计.md)。

### 4. MinIO 路径

历史实现中瓦片对象路径直接绑定 MVT 和 MinIO。

当前实现：

1. 路径是 `storage_ref` 内部描述的实现细节。
2. 产物状态记录存储引用，上层逻辑不直接依赖固定路径。
3. manifest 记录格式、层级、版本、覆盖范围和生成配置。

### 5. extent 与 SRID

extent 与 SRID 的专门边界见 [MVT 快显 extent 与 SRID 边界说明](MVT快显extent与SRID边界说明.md)。

当前边界：

1. Meta attributes 中的 spatial extent 是源事实。
2. 快显和瓦片缓存中的 extent 是派生事实。
3. 不应把 MVT / 快显渲染信息 的派生范围写回 `attributes.capabilities.spatial`。

## 四、清理完成记录

实现改造的阶段性清单见 [Manager 快显与瓦片缓存实现改造清单](Manager快显与瓦片缓存实现改造清单.md)。本文以下条目记录本轮收敛结果。

### P0 文档和概念

- [x] 按 [Manager 快显与瓦片缓存概念原则](Manager快显与瓦片缓存概念原则.md) 修订快显和瓦片缓存相关文档。
- [x] 删除或迁移文档中的 MVT-only 概念表述。
- [x] 删除或迁移文档中的 MinIO-only 概念表述。
- [x] 删除旧 md5 fingerprint、旧路径、旧状态和旧 API 描述。

### P1 任务体系

- [x] 将 `tile_cache_generation` 语义迁移为瓦片缓存生成语义。
- [x] 将目标任务类型确定为 `tile_cache_generation`。
- [x] 将目标任务定义表确定为 `manager.tile_cache_tasks`。
- [x] 调整 TaskProvider capabilities。
- [x] 明确空间预览页发起生成时必须跳转瓦片缓存页面的“任务”tab 并创建 `manager.tile_cache_tasks`。

### P2 状态和产物

- [x] 明确 Quick View State 状态机。
- [x] 按独立建模方式设计 `manager.tile_cache`。
- [x] 记录 item 与瓦片缓存结果的关联关系。
- [x] 将 `manager.tile_cache` 字段收敛为第一阶段必要核心字段，避免过早扩张。

### P3 UI

- [x] 空间预览页展示 `can_use_quick_view`。
- [x] 空间预览页展示 `can_generate_tile_cache`。
- [x] `can_use_quick_view=true` 时展示切换快显；`render_source=realtime_tile` 时可同时展示生成瓦片缓存按钮。
- [x] `can_use_quick_view=false` 且 `can_generate_tile_cache=true` 时只展示生成瓦片缓存按钮。
- [x] 具备生成条件时跳转到瓦片缓存页面的“任务”tab，并携带 item 信息。
- [x] 瓦片缓存页面包含“任务”和“结果”两个 tab。
- [x] “任务”tab 支持格式、层级、目标存储、准备动作判断等配置。
- [x] “结果”tab 支持查看和管理 `manager.tile_cache`。

### P4 当前实现收敛

- [x] 梳理 QuickViewService、UnifiedMVTService、SpatialPreviewService、worker 中的职责边界。
- [x] 清理 handler 中的默认 `geom`、默认 SRID 和调试输出。
- [x] 将准备动作从普通预览或瓦片请求中剥离。
- [x] 将固定 MinIO 路径依赖改为产物状态中的 storage ref。

## 五、后续独立专题

以下内容不作为旧路径兼容保留，后续如需增强应进入独立专题：

1. 目标存储选择的多 provider API 结构。
2. 瓦片缓存 manifest 的长期 JSON schema。
3. 物化视图命名规则是否需要从 PostGIS 专用实现上提为平台规范。
