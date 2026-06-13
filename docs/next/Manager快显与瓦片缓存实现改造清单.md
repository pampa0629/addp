# Manager 快显与瓦片缓存实现改造清单

> 状态：next 实施清单。本文承接 [Manager 快显与瓦片缓存概念原则](Manager快显与瓦片缓存概念原则.md)、[Manager 瓦片缓存结果状态设计](Manager瓦片缓存结果状态设计.md)、[Manager 瓦片缓存生成任务设计](Manager瓦片缓存生成任务设计.md) 和 [Manager 快显现状调研和问题记录](Manager快显现状调研和问题记录.md)，用于指导后续代码改造。本文不引入兼容双轨，旧路径应按 clean break 收敛。

## 一、改造目标

实现层最终应收敛为三层模型：

| 对象 | 目标落点 | 说明 |
| --- | --- | --- |
| 快显状态 | `manager.quick_view` | 当前 item 是否可快显、推荐结果、不可用原因和 UI 偏好 |
| 瓦片缓存结果 | `manager.tile_cache` | 瓦片缓存是否可用、存储引用、格式、范围、层级、配置指纹和最近执行 |
| 瓦片缓存任务 | `manager.tile_cache_tasks` | 可执行、可调度、可编排的瓦片缓存生成任务定义 |

目标任务类型：

```text
tile_cache_generation
```

MVT 只是瓦片格式：

```text
config.tile.format=mvt
```

## 二、实施原则

1. 不保留旧 MVT-only 任务类型 / 旧任务表长期双轨。
2. 不把 Quick View 称为任务。
3. 不把 `quick_view` 继续作为瓦片缓存结果状态表。
4. 不在普通预览、普通瓦片请求中隐式执行准备动作。
5. 不默认几何列名为 `geom`。
6. 不将 MinIO bucket、prefix、MVT 路径规则作为上层事实源。
7. 从空间预览页发起生成时，也必须先创建 `manager.tile_cache_tasks`，再执行。
8. 第一阶段可以只支持 PostGIS + MVT + 默认存储，但命名和结构不能锁死。

## 三、阶段 0：现状保护和入口盘点

目标：在改代码前明确当前要删除和要迁移的入口。

清单：

- [x] 盘点 `manager/backend/internal/api/router.go` 中 `tile_cache_tasks` 与统一 `quick-view/*` 路由。
- [x] 盘点 `manager/backend/internal/api/task_provider_handler.go` 中 `tile_cache_generation` 分支。
- [x] 盘点旧任务模型和 `manager/backend/internal/models/quick_view.go`。
- [x] 盘点 `QuickViewService`、`UnifiedMVTService`、`TileCacheTaskService`、执行运行时的职责。
- [x] 盘点旧瓦片任务页面和 `manager/frontend/src/api/quickView.js`。
- [x] 盘点 Swagger 产物中 `/tile_cache_tasks`、`tile_cache_generation`、`tasks/tile_cache_generation/{id}/execute` 的引用。

验收：

```bash
legacy_pattern="$(printf '%s' \
  'mvt_' \
  '.*generation' \
  '|mvt_' '.*tasks' \
  '|quick-view/(' 'pre-cache|prepare|tasks' ')')"
rg -n "$legacy_pattern" manager/backend manager/frontend
```

本阶段只确认范围，不改行为。

## 四、阶段 1：数据库模型 clean break

目标：建立目标三表模型，移除旧任务表语义。

清单：

- [x] 新增或替换 `manager.tile_cache_tasks`。
- [x] 新增 `manager.tile_cache`。
- [x] 收敛 `manager.quick_view` 为快显状态表。
- [x] 删除旧 MVT-only 任务表。
- [x] 将旧 `quick_view` 中产物字段迁移到 `tile_cache` 或废弃。
- [x] 将旧 `quick_view` 中任务和执行字段迁移到 `tile_cache_tasks` / `common.task_executions` 或废弃。
- [x] 统一任务状态字段：任务最近执行摘要使用 unified execution status，产物状态使用瓦片缓存结果状态。

`manager.tile_cache_tasks` 第一阶段字段：

| 字段 | 说明 |
| --- | --- |
| `id`、`tenant_id`、`name`、`description` | 任务定义基础字段 |
| `enabled`、`schedule`、`next_run_at`、`last_run_at` | 调度字段 |
| `last_execution_id`、`last_execution_status` | 最近执行摘要 |
| `config` | target / tile / storage / preparation / options |
| `created_by`、`created_at`、`updated_at`、`deleted_at` | 生命周期字段 |

`manager.tile_cache` 第一阶段字段：

| 字段 | 说明 |
| --- | --- |
| `id`、`tenant_id`、`item_id`、`locator` | 产物身份和来源 |
| `task_id`、`last_execution_id` | 任务和执行关联 |
| `tile_format`、`storage_ref` | 格式和存储引用 |
| `extent`、`extent_srid`、`min_zoom`、`max_zoom` | 产物覆盖范围和层级 |
| `config_hash` | 同一生成配置的幂等判断 |
| `status`、`error_message` | 产物状态 |
| `created_by`、`created_at`、`updated_at`、`deleted_at` | 生命周期字段 |

验收：

```bash
legacy_pattern="$(printf '%s' \
  'manager\\.mvt_' '.*tasks' \
  '|TaskTypeMvt' 'Generation' \
  '|mvt_' \
  '.*generation')"
rg -n "$legacy_pattern" manager/backend common
```

目标是没有旧任务类型和旧任务表引用。

## 五、阶段 2：后端 API 和 TaskProvider

目标：统一 Manager 任务能力为 `tile_cache_generation`。

清单：

- [x] TaskProvider capabilities 声明 `tile_cache_generation` 和 `embedding`。
- [x] 删除旧任务类型分支。
- [x] 删除或替换旧私有 CRUD。
- [x] 新增 `/tile_cache_tasks` 私有 CRUD。
- [x] 标准任务入口支持：

```text
GET  /api/v1/manager/tasks?task_type=tile_cache_generation
GET  /api/v1/manager/tasks/tile_cache_generation/{id}
POST /api/v1/manager/tasks/tile_cache_generation/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

当前 `tile_cache_generation.supports_cancel=false`，标准取消入口不作为本轮目标入口暴露；后续只有具备真实取消能力后才开放 `POST /api/v1/manager/executions/{execution_id}/cancel`。

- [x] 新增或收敛瓦片缓存结果查询和管理 API。
- [x] 新增或收敛快显状态查询 API，返回 `can_use_quick_view`、`can_generate_tile_cache`、`default_tile_cache_id` 和不可用原因。
- [x] 旧 quick-view 直接准备 / 直接生成入口不作为目标入口保留。
- [x] 执行请求不支持参数覆盖时，拒绝非空 `parameters`。

验收：

```bash
legacy_pattern="$(printf '%s' \
  'quick-view/(' 'pre-cache|prepare|tasks' ')' \
  '|tile cache task preparation' ' step')"
rg -n "$legacy_pattern" manager/backend/internal/api manager/backend/internal/service
bash scripts/swagger/gen-swagger.sh manager
bash scripts/swagger/check-route-coverage.sh manager
```

## 六、阶段 3：执行运行时和生成服务

目标：把当前 MVT 生成能力收敛为瓦片缓存生成任务的执行实现。

清单：

- [x] 将旧任务服务收敛为 `TileCacheTaskService`。
- [x] 将执行主链路改为读取 `tile_cache_tasks.config`。
- [x] 执行开始时创建或更新 `tile_cache.status=generating`。
- [x] 生成完成时写入 `storage_ref`、范围、层级、`config_hash`、`last_execution_id`。
- [x] 生成失败时同步更新 `tile_cache.status`。
- [x] 成功后只更新 `tile_cache`；快显能力由能力 API 动态合成，不写回 `quick_view`。
- [x] 将瓦片数量、耗时和停止原因写入 `common.task_executions.metadata`。
- [x] 将旧准备入口从普通预览或普通瓦片请求中剥离。
- [x] 保留 PostGIS + MVT 作为第一阶段 tile format 实现。
- [x] 当前 PostGIS + MVT 生成由 Manager Backend 内部执行；删除空 Manager Worker 旧运行时。后续若负载模型要求独立执行器，应切换为唯一 Manager Worker 或 GIS 执行引擎，不保留双轨。

验收：

```bash
rg -n "QuickViewService|UnifiedMVTService|TileCacheTaskService|tile_cache_generation|tile_cache_tasks" manager/backend/internal
go test ./manager/backend/internal/... ./common/...
```

如果当前仓库不支持直接按以上包路径测试，应按 Manager 现有 Go module 结构调整命令。

## 七、阶段 4：空间预览和快显状态

目标：预览页只展示必要入口，降低用户认知负担。

清单：

- [x] 空间预览页请求快显状态。
- [x] `can_use_quick_view=true` 时展示“切换快显”；`render_source=realtime_tile` 时可同时展示“生成瓦片缓存”。
- [x] `can_use_quick_view=false && can_generate_tile_cache=true` 时只展示“生成瓦片缓存”。
- [x] 点击“生成瓦片缓存”跳转瓦片缓存页面的“任务”tab，并携带 item 上下文。
- [x] `can_use_quick_view=false && can_generate_tile_cache=false` 时展示不可用原因。
- [x] 预览页不展示刷新、重建、生成另一份缓存等复杂操作。
- [x] 重新生成、刷新、删除产物从瓦片缓存页面发起。

验收：

```bash
legacy_pattern="$(printf '%s' \
  'pre-' 'cache' \
  '|tile cache task preparation' ' step' \
  '|TileCache' 'Tasks')"
rg -n "$legacy_pattern" manager/frontend/src
```

目标是前端不再从空间预览页直接调用旧 quick-view 直接生成入口。

## 八、阶段 5：瓦片缓存页面

目标：参考向量化页面，建立“任务”和“产物”两个 tab。

清单：

- [x] 新增或替换瓦片缓存页面。
- [x] “任务”tab 管理 `manager.tile_cache_tasks`。
- [x] “任务”tab 支持创建、编辑、执行、调度、删除和跳转 Monitor。
- [x] “任务”tab 支持从预览页带 item 上下文进入创建态。
- [x] “结果”tab 管理 `manager.tile_cache`。
- [x] “结果”tab 支持状态筛选、定位源 item、查看存储引用、删除或跳转 Monitor。
- [x] 页面文案使用“瓦片缓存”，不使用“MVT 任务”作为一级概念。

验收：

```bash
rg -n "TileCacheTasks|tile_cache_tasks|tile_cache_generation|瓦片缓存任务|MVT 任务" manager/frontend/src
```

## 九、阶段 6：存储引用和 manifest

目标：让上层逻辑依赖 `storage_ref`，不依赖固定 MinIO 路径。

清单：

- [x] 定义第一阶段 `storage_ref` 字符串或 JSON 结构。
- [x] 生成任务写入 `storage_ref`。
- [x] 当前 MinIO 路径规则下沉到 tile format / storage 实现内部。
- [x] 瓦片请求通过产物状态或 manifest 解析真实对象路径。
- [x] 第一阶段 manifest 至少记录格式、层级、范围、生成配置摘要和对象前缀。
- [x] 清理产物时通过 `storage_ref` / manifest 删除对象。

暂不扩展：

1. 多存储 provider 完整授权模型。
2. 复杂访问策略。
3. 复杂清理策略。

这些进入后续存储和清理专题。

## 十、阶段 7：清理旧命名和文档

目标：代码、Swagger、前端路由和文档全部使用新语义。

清单：

- [x] 删除旧 MVT-only 任务类型、任务表和代码模型。
- [x] 删除“Quick View 任务”等旧任务文案。
- [x] 删除旧 MVT-only 任务路由。
- [x] 删除旧 quick-view 直接准备 / 直接生成目标入口。
- [x] Swagger 不再暴露旧任务类型。
- [x] 前端不再使用 `TileCacheTasks.vue` 作为任务页面名称。
- [x] Manager 文档和平台文档通过旧命名关键词检查。

验收：

```bash
legacy_pattern="$(printf '%s' \
  'mvt_' \
  '.*generation' \
  '|manager\\.mvt_' '.*tasks' \
  '|Mvt' 'Task' \
  '|quick-view[/](' 'pre-cache|prepare|tasks' ')' \
  '|tile cache task preparation' ' step')"
rg -n "$legacy_pattern" docs manager common
```

允许保留的 MVT 表述只应是：

1. 当前第一阶段瓦片格式。
2. MVT 专题实现备查。
3. `config.tile.format=mvt`。

## 十一、建议实施顺序

推荐顺序：

1. 数据库模型和后端 task type clean break。
2. TaskProvider 和任务 CRUD。
3. 执行运行时接入 `tile_cache_tasks` 和 `tile_cache`。
4. 快显能力 API。
5. 前端瓦片缓存页面。
6. 空间预览页按钮逻辑。
7. 删除旧路由、旧页面、旧 Swagger。

不建议先做前端页面再改后端模型，否则容易为了旧 API 做兼容。

## 十二、最小验收命令

文档和关键词：

```bash
legacy_pattern="$(printf '%s' \
  'mvt_' \
  '.*generation' \
  '|manager\\.mvt_' '.*tasks' \
  '|Mvt' 'Task' \
  '|quick-view[/](' 'pre-cache|prepare|tasks' ')')"
rg -n "$legacy_pattern" docs manager common
rg -n "tile_cache_generation|manager\\.tile_cache_tasks|manager\\.tile_cache" docs manager common
```

后端：

```bash
go test ./manager/backend/internal/... ./common/...
bash scripts/swagger/gen-swagger.sh manager
bash scripts/swagger/check-route-coverage.sh manager
```

前端：

```bash
cd manager/frontend
npm run type-check
npm run build
```

## 十三、补充整改：快显能力状态机与渲染路径

本节承接 [Manager 快显能力状态机与渲染路径设计](Manager快显能力状态机与渲染路径设计.md)。前述阶段 4 虽已完成入口清理，但当前实现仍缺少“可快显能力合成”和“前端真实切换渲染源”的落地，应补充整改。

清单：

- [x] 后端新增统一的快显能力合成函数，不能命中 `quick_view` 记录后直接返回。
- [x] `can_use_quick_view=true` 支持 `cached_tile`、`direct_geojson`、`realtime_tile` 三类第一阶段渲染源。
- [x] 小数据量空间表直接返回可快显，第一阶段先按 `record_count <= direct_geojson_max_rows` 判定。
- [x] `tile_cache` 状态变化后统一重算 quick_view，不能由单个 generating / failed 结果覆盖旧 ready 能力。
- [x] 删除瓦片缓存结果后按“其他 ready 结果 -> direct_geojson -> realtime_tile -> 可生成缓存 -> 不可用”顺序重算。
- [x] `preferred_mode` 只表达用户偏好，不应被 `tile_cache` ready 自动覆盖；如需系统推荐，使用响应字段表达。
- [x] quick-view status API 返回渲染源和渲染参数，前端不猜测。
- [x] 前端空间预览维护 `activePreviewMode`，点击“切换快显”后实际切换到 quick_view 渲染路径。
- [x] `direct_geojson` 快显使用完整或分批 GeoJSON，不依赖表格当前页。
- [x] 补齐 100 条、127 条、已有 ready 瓦片缓存结果、新结果 generating、生成失败、删除默认结果等测试或手工验收。

如果本地项目脚本名称不同，应使用 Manager 现有 package scripts 的等价命令。
