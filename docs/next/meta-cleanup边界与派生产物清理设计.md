# Meta cleanup 边界与派生产物清理设计

> 状态：讨论稿。本文记录当前 cleanup 实现的边界问题和推荐方向；本轮先不改代码，后续作为 cleanup / preprocessing 专题的输入。

## 背景

Meta 扫描主线已经逐步收敛为：

- Meta 负责发现和刷新数据项事实，写入 `meta_node`、`meta_item` 和标准 `attributes`。
- Manager preview、Quick View、MVT 等消费方读取 Meta 已有事实，不应在预览链路偷偷写回 Meta。
- MVT / Quick View 属于空间展示相关派生产物，不属于源 metadata facts。

在继续整理 Meta 后端目录和服务职责时，发现 cleanup 链路存在跨模块 owner 边界问题，需要单独讨论，不适合在普通代码整理中顺手修改。

## 当前实现事实

### 1. Meta CleanupService 承担多类职责

当前 `meta/backend/internal/service/cleanup_service.go` 同时承担：

- 定时物理删除软删除的 `meta_item` / `meta_node`。
- 订阅 `cleanup.request` Redis Stream。
- 扫描和执行 Meta DB 垃圾清理。
- 清理 Meta 的 Meilisearch 索引。
- 扫描和删除 MinIO 对象。
- 在引擎删除事件后，软删除 Meta 数据并尝试删除 MVT 瓦片。

这些职责中，Meta DB 和 Meta Search 属于 Meta 自身 owner 范围；MinIO 中的 MVT tiles 则属于 Manager Quick View 派生产物。

### 2. Meta 直接读取 Manager 表

`meta/backend/internal/metacleanup/database.go` 中存在直接查询 `manager.quick_view` 的逻辑：

- 按无效 engine 查询 Quick View fingerprint。
- 按单个 engine 查询 Quick View fingerprint。

这使 Meta 知道了 Manager 的内部表结构、fingerprint 用法和 Quick View 派生产物关系。

### 3. Meta 直接删除 Manager bucket 中的 MVT tiles

`meta/backend/internal/metacleanup/minio.go` 中存在直接扫描和删除以下对象的逻辑：

```text
bucket: manager
prefix: mvt-tiles/<fingerprint>/
```

这使 Meta 知道了 Manager 的 MinIO bucket、MVT tiles key 结构，以及 Quick View 派生产物的存储策略。

### 4. cleanup 事件协议已有雏形，但 Manager 未接入

`common/events/cleanup_events.go` 已经定义：

- `cleanup.request`
- `CleanupRequestEvent.ExpectedModules`
- `CleanupResultData`
- `ModuleMeta`
- `ModuleManager`

这说明 cleanup 本来可以是跨模块协同协议。但当前实际消费方主要是 Meta，Manager 暂未实现 cleanup consumer，导致 Meta 代替 Manager 处理了 Manager 资源。

## 核心问题

### 1. Meta 越过模块 owner 边界

Meta 可以知道 System，因为 Meta 扫描需要读取 engine 注册信息和 engine 生命周期事件。

但 Meta 不应知道 Manager 的 Quick View 表结构、MVT bucket 结构和派生产物生命周期。否则 Manager 内部实现一旦调整，Meta cleanup 会被迫同步修改，形成隐式耦合。

### 2. cleanup 把源事实和派生产物混在一起

`meta_item.attributes` 是源 metadata facts。

Quick View / MVT tiles 是基于 metadata facts 和展示策略生成的派生产物。它们可能有自己的状态、任务、重试、缓存和过期策略，不应被 Meta DB cleanup 逻辑直接支配。

### 3. 当前逻辑难以扩展到更多 preprocessing 产物

如果后续出现：

- 图片缩略图
- 文档向量化索引
- embedding index
- 大文件 access index
- 其他物化缓存

都不能让 Meta cleanup 逐个知道每个模块的表、bucket、key 和删除规则。否则 cleanup 会变成跨模块硬编码中心。

### 4. System 不应知道 Meta，cleanup 也不能反向制造耦合

System 只应发布中性的生命周期事件，例如 engine deleted / disabled。

具体模块如何处理自己的资源，应由各模块订阅并执行。System 不需要知道 Meta，也不需要知道 Manager 的 MVT 或其他派生产物。

## 推荐原则

### 1. 谁拥有产物，谁负责 cleanup

| 资源 | Owner | cleanup 责任 |
| --- | --- | --- |
| `meta.meta_node` | Meta | Meta |
| `meta.meta_item` | Meta | Meta |
| Meta search index | Meta | Meta |
| `manager.quick_view` | Manager | Manager |
| `manager` bucket 下的 MVT tiles | Manager | Manager |
| Manager embedding / preview cache | Manager | Manager |
| Transfer 临时导入产物 | Transfer | Transfer |

### 2. common 只定义协议，不承载业务 owner

`common/events` 可以定义 cleanup request / result 的协议模型，但不能把具体模块的表结构、bucket key、artifact 类型写进 common。

### 3. cleanup coordinator 与 cleanup executor 分离

推荐概念模型：

```mermaid
flowchart TD
    Console[Console / Admin UI] --> Request[cleanup.request]
    System[System lifecycle event] --> Request

    Request --> MetaConsumer[Meta cleanup consumer]
    Request --> ManagerConsumer[Manager cleanup consumer]
    Request --> TransferConsumer[Transfer cleanup consumer]

    MetaConsumer --> MetaOwned[Meta DB / Meta Search]
    ManagerConsumer --> ManagerOwned[Quick View / MVT / Preview Artifacts]
    TransferConsumer --> TransferOwned[Transfer Artifacts]

    MetaConsumer --> Result[cleanup:results:task_id]
    ManagerConsumer --> Result
    TransferConsumer --> Result
```

其中：

- coordinator 只负责发起请求、汇总结果和展示审计。
- executor 只清理本模块拥有的资源。
- `ExpectedModules` 用于声明本次请求等待哪些模块响应。

### 4. engine lifecycle event 应由各模块独立消费

引擎删除或禁用后：

- Meta 消费事件：软删除或标记相关 metadata，并清理 Meta search index。
- Manager 消费事件：清理 Quick View、MVT tiles、相关 preview/embedding 派生产物。
- 其他模块按自己的 owner 范围消费事件。

这条路径不要求 System 知道 Meta 或 Manager 的内部实现。

## 推荐改造方向

### 阶段 1：文档固化

- 本文先记录现状、问题和推荐方向。
- `meta-preprocessing语义与任务边界.md` 可引用本文，说明 artifact preprocessing 的 cleanup owner 归属。
- 暂不修改 Meta cleanup 代码，避免影响当前正在推进的 Meta 扫描主线。

### 阶段 2：Manager 接入 cleanup consumer

在 Manager 内新增 cleanup consumer：

- 订阅 `cleanup.request`。
- 仅处理 `ExpectedModules` 包含 `manager`，或请求未限制模块但 Manager cleanup 已启用的任务。
- scan 阶段统计 Manager 自己的垃圾数据：
  - 无效 engine 关联的 `quick_view`。
  - 孤立或过期的 Quick View 记录。
  - 可删除的 MVT tiles。
- execute 阶段删除 Manager 自己的资源：
  - `manager.quick_view`。
  - `manager` bucket 下对应 MVT tiles。
- 写入 `cleanup:results:<task_id>` hash，key 为 `manager`。

### 阶段 3：移除 Meta 对 Manager 产物的直接依赖

确认 Manager cleanup consumer 可用后，从 Meta 移除：

- `metacleanup.DatabaseCleaner.InvalidFingerprints`
- `metacleanup.DatabaseCleaner.FingerprintsByEngine`
- `metacleanup.MinIOCleaner` 中扫描 / 删除 `manager` bucket 的 MVT 逻辑
- `CleanupService.deleteMinIOMVTByEngine`
- `CleanupService.ScanGarbage` / `ExecuteCleanup` 中基于 Manager fingerprint 的 MinIO cleanup

Meta 保留：

- Meta DB cleanup。
- Meta search index cleanup。
- 对 System engine lifecycle event 的消费。

### 阶段 4：统一 cleanup 结果模型

后续可进一步讨论：

- cleanup task 是否进入 common execution。
- cleanup scan 和 cleanup execute 是否需要统一 execution 记录。
- `ExpectedModules` 的默认行为。
- 各模块 cleanup result 的标准字段。
- cleanup scan result 与 execute result 的关联和审计。

## 本轮暂不处理

本轮只记录边界，不修改代码：

- 不删除 Meta 中现有 MVT cleanup 逻辑。
- 不新增 Manager cleanup consumer。
- 不修改 `common/events` 协议。
- 不调整 MVT / Quick View 表结构和 MinIO key。
- 不改变当前 cleanup API 行为。

后续需要在 cleanup / preprocessing 专题中确认后，再进入代码迁移。
