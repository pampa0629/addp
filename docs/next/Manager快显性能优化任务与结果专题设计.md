# Manager 快显性能优化任务与结果专题设计

> 状态：专题归档。核心约束已收敛到 Manager 模块文档和任务体系规范；本文保留设计讨论、边界说明和阶段记录。

## 一、背景

Manager 快显有两类高性能渲染路径：

1. 没有瓦片缓存时，通过动态 MVT 直接浏览空间数据。
2. 生成 MVT 瓦片缓存后，通过缓存瓦片稳定浏览空间数据。

对于 PG 空间表，MVT 使用 WebMercator / EPSG:3857 瓦片矩阵。非 3857 源表如果在每次瓦片 SQL 中执行 `ST_Transform(source_geom, 3857)`，会显著增加单瓦片成本，并可能绕过可用空间索引。大数据量空间表在动态 MVT 和瓦片缓存生成中都会受到影响。

因此需要引入独立的快显性能优化能力，为 PG 空间表准备可复用的 3857 渲染目标。

## 二、核心结论

1. 快显性能优化是独立任务，不是瓦片缓存生成任务的内部步骤。
2. 任务名称使用“快显性能优化任务”。
3. 结果名称使用“快显性能优化结果”。
4. 快显性能优化任务应纳入 ADDP 统一任务体系，由 Manager TaskProvider 声明独立 `task_type`。
5. 快显性能优化结果应有独立结果表，表达当前优化目标是否存在、在哪里、是否可用、由哪次 execution 生成或刷新。
6. 快显性能优化结果服务动态 MVT 和 MVT 瓦片缓存生成，不归属于某一次瓦片缓存任务。
7. 删除快显性能优化结果，只删除 Manager 创建并登记归属的对象和缓存，不删除源表、不删除源表原有索引、不删除已生成的瓦片缓存。

## 三、建议任务类型

建议 Manager 新增任务类型：

```text
quick_view_optimization
```

该任务类型遵守 [ADDP 任务体系规范](../spec/addp任务体系规范.md)：

1. 任务定义归 Manager 私有表。
2. 每次执行写入 `common.task_executions`。
3. 执行状态使用统一 execution status：`pending` / `running` / `success` / `failed` / `timeout` / `cancelled`。
4. 任务类型通过 Manager TaskProvider capabilities 声明。
5. Orchestrator 引用时使用 `provider=manager + task_type=quick_view_optimization + task_id`。
6. Monitor 通过 `common.task_executions` 观察执行，通过 Manager 只读 API 回查任务定义和结果状态。

第一阶段 TaskProvider capabilities 建议：

| 字段 | 建议值 | 说明 |
| --- | --- | --- |
| `supports_schedule` | `false` | 不支持任务自身 Cron 定时刷新。 |
| `supports_cancel` | `false` | 暂不开放标准取消能力，避免物化视图构建和索引构建取消后的清理语义不完整。 |
| `supports_inline_execution` | `false` | 遵守 `task.capabilities/v1`，只允许引用已保存任务定义。 |

`supports_schedule=false` 只表示快显性能优化任务自身不按 Cron 自动刷新，不影响 Orchestrator 把该任务作为编排步骤触发。若某个编排本身支持定时运行，快显性能优化任务可以被该编排间接触发；此时 execution 的 `source=orchestrator`，`trigger_type` 仍按任务体系规范写入 `manual` 或 `scheduled`。

建议 TaskProvider 中 Manager 任务类型扩展为：

| provider | task_type | 任务定义表 | 结果表 |
| --- | --- | --- | --- |
| `manager` | `tile_cache_generation` | `manager.tile_cache_tasks` | `manager.tile_cache` |
| `manager` | `quick_view_optimization` | `manager.quick_view_optimization_tasks` | `manager.quick_view_optimization` |
| `manager` | `embedding` | `manager.embedding_tasks` | embedding 相关结果表 |

## 四、任务定义表

建议新增：

```text
manager.quick_view_optimization_tasks
```

该表回答：

> 以后按什么配置为哪个 PG 空间表准备或刷新快显性能优化目标。

核心字段遵守任务体系公共语义：

| 字段 | 说明 |
| --- | --- |
| `id` | Manager 内部任务定义 ID。 |
| `tenant_id` | 租户 ID。 |
| `name` / `description` | 任务名称和描述。 |
| `enabled` / `schedule` / `next_run_at` / `last_run_at` | 调度字段。 |
| `last_execution_id` / `last_execution_status` | 最近执行摘要。 |
| `config` | 快显性能优化私有配置。 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | 生命周期字段。 |

`config` 建议结构：

```json
{
  "target": {
    "item_fingerprint": "string",
    "locator": "addp://engine/8/path/public/dltb?type=table&item_id=54",
    "source_engine_id": 8,
    "schema": "public",
    "table": "dltb",
    "item_id": 54
  },
  "geometry": {
    "geometry_column": "SmGeometry",
    "source_srid": 2360,
    "target_srid": 3857
  },
  "optimization": {
    "target_kind": "source_schema_materialized_view",
    "include_source_key": true,
    "attributes": [],
    "analyze_after_build": true
  },
  "storage": {
    "target_schema": "public"
  }
}
```

第一阶段任务配置只应描述稳定目标和策略，不保存每次执行统计。执行统计进入 `common.task_executions.metadata`。

## 五、结果表

建议新增：

```text
manager.quick_view_optimization
```

该表回答：

> 某个 item 当前是否有可复用的快显性能优化目标，该目标在哪里，是否可用，由什么任务和 execution 生成。

建议核心字段：

| 字段 | 说明 |
| --- | --- |
| `id` | 结果 ID。 |
| `tenant_id` | 租户 ID。 |
| `item_fingerprint` | 源 item 指纹。 |
| `item_id` | 当前 Meta item 行引用，仅用于回查。 |
| `locator` | Resource Locator。 |
| `task_id` | 产生或最近刷新该结果的优化任务 ID。 |
| `last_execution_id` | 最近一次优化 execution。 |
| `source_engine_id` | 源 PG 引擎 ID。 |
| `source_schema` / `source_table` / `source_geometry_column` | 源空间表和几何列。 |
| `source_srid` | 源 SRID。 |
| `target_srid` | 目标 SRID，第一阶段为 `3857`。 |
| `target_kind` | 目标形态，第一阶段默认 `source_schema_materialized_view`。 |
| `target_schema` / `target_table` / `target_geometry_column` | 优化目标位置。 |
| `status` | 结果状态。 |
| `render_extent` / `render_extent_srid` | 可渲染范围，第一阶段建议保存 WGS84 范围。 |
| `row_count_estimate` | 优化目标估算行数。 |
| `source_fingerprint_snapshot` | 源事实快照，用于失效判断。 |
| `metadata` | 属性列、索引名、诊断摘要、构建选项等扩展信息。 |
| `error_message` | 最近错误摘要。 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | 生命周期字段。 |

结果状态属于 artifact state，不是统一 execution status：

| 状态 | 含义 |
| --- | --- |
| `building` | 优化结果正在生成或刷新。 |
| `ready` | 优化结果可用。 |
| `stale` | 源事实变化，结果需要刷新。 |
| `failed` | 最近生成失败，当前结果不可用或不完整。 |
| `deleted` | 结果已清理，仅保留审计或摘要。 |

## 六、优化目标存储位置

快显性能优化目标不应存储到 ADDP infra PG 的 `manager` schema 中。infra PG 只保存 Manager 自身的任务定义、结果记录和执行摘要，不承载用户源空间数据的派生副本。

第一阶段推荐：

> 在源空间表所在的 PG 引擎、数据库和源 schema 内，创建由 ADDP 命名和登记的 3857 物化视图。

这样做的原因：

1. 派生目标与源表在同一个 PG 数据库内，可以直接从源表构建，也可以保留源主键和必要属性列。
2. 动态 MVT 和瓦片缓存生成仍然连接同一个源 PG 引擎，不需要跨库读取或跨库 join。
3. 物化视图对用户可见，符合“独立任务生成独立结果”的产品理解。
4. 不要求用户或管理员提前配置派生 schema，第一阶段操作路径更短。
5. 删除快显性能优化结果时，只删除 ADDP 创建并登记的物化视图及其索引，不删除源表和源表原有索引。

不推荐：

1. 不推荐把 3857 派生目标搬到 infra PG。这样会复制大量用户数据到平台库，破坏源引擎边界，也会让属性同步、权限和生命周期复杂化。
2. 第一阶段不推荐让用户每次手动选择存储位置。快显性能优化是性能准备能力，不应把底层 schema 规划暴露成高频操作。
3. 第一阶段不默认创建同库独立 schema。独立 schema 有更清晰的隔离性，但会引入 schema 权限、命名配置和用户理解成本，后续确有需要再提供。

如果源 PG 引擎没有在源 schema 下创建物化视图或索引的权限，任务应在预检阶段失败，并给出明确诊断：需要授权 ADDP 创建快显性能优化结果，或改用已登记的外部 3857 目标。

未来如源 schema 污染、权限隔离或 DBA 管控成为主要问题，再提供“同库独立 schema”作为引擎级配置能力。普通用户创建任务时仍不需要选择底层位置。

## 七、优化动作包

快显性能优化面向用户是一件事，内部由一组动作组成。

### 1. 创建 3857 派生目标

价值：

1. 避免动态 MVT 或缓存生成时反复执行 `ST_Transform`。
2. 让瓦片查询可以直接对 3857 几何做空间过滤和裁剪。

产出：

1. Manager 创建并登记的 3857 物化视图。
2. 目标几何列，例如 `geom_3857`。

存储：

1. 第一阶段默认放在源表所在 schema 中。
2. 物化视图名称必须由 ADDP 生成稳定前缀和唯一后缀，避免与业务对象冲突。
3. 后续可扩展为同库独立 schema，但不作为第一阶段默认路径。

删除：

1. 删除快显性能优化结果时删除该物化视图。
2. 不删除源表。

### 2. 创建 `geom_3857` GiST 索引

价值：

1. 让动态 MVT 和瓦片缓存生成可以使用空间索引。
2. 让大数据量空间表进入可控查询路径。

产出：

1. Manager 在派生目标上创建的 GiST 索引。

存储：

1. 与派生目标在同一 PG 数据库中。
2. 索引名写入结果表 `metadata`，便于清理和诊断。

删除：

1. 随派生目标删除自动消失。
2. 如果单独清理索引，也只清理 Manager 登记创建的索引。

### 3. `ANALYZE` 派生目标

价值：

1. 让 PostgreSQL 查询优化器掌握派生目标统计信息。
2. 改善动态 MVT 和缓存生成 SQL 的执行计划。

产出：

1. PostgreSQL 内部统计信息。
2. execution metadata 中记录 analyze 是否执行和耗时。

存储：

1. PostgreSQL 系统统计信息。
2. Manager execution metadata 保存动作摘要。

删除：

1. 随派生目标删除自然失效。
2. 不需要单独删除统计信息。

### 4. 源表诊断快照

价值：

1. 判断优化结果是否仍匹配当前源表。
2. 支持后续提示 `ready`、`stale` 或需要重建。

产出：

1. 源 schema、table、几何列、SRID、几何类型、估算行数。
2. 源字段摘要、源表版本线索或扫描指纹。

存储：

1. `manager.quick_view_optimization.source_fingerprint_snapshot`。
2. execution metadata 中保存更详细诊断。

删除：

1. 删除结果记录时删除。
2. 不影响 Meta 源 facts。

### 5. 保留源主键或稳定行标识

价值：

1. 支持点击、高亮、回查和问题诊断。
2. 避免派生目标只剩几何后无法关联源记录。

产出：

1. 派生目标中的 `source_row_id`。
2. metadata 中记录该列的来源和可靠性。

存储：

1. 3857 派生目标。
2. `manager.quick_view_optimization.metadata`。

删除：

1. 随派生目标删除。

### 6. 保留最小 MVT 属性列

价值：

1. 支持样式、分类、tooltip 和基础识别。
2. 避免复制全表字段导致派生目标过宽、MVT 过大。

产出：

1. 派生目标中的最小属性列集合。

存储：

1. 3857 派生目标。
2. 属性列清单写入结果 metadata。

删除：

1. 随派生目标删除。

### 7. 计算和记录渲染范围

价值：

1. 支持 capability 返回地图初始视图。
2. 支持瓦片缓存任务默认范围和层级推荐。
3. 避免每次打开预览都扫描大表计算范围。

产出：

1. `render_extent`。
2. `render_extent_srid=4326`。
3. 范围来源和估算方式。

存储：

1. `manager.quick_view_optimization.render_extent`。
2. manifest 或 metadata 中保存诊断信息。

删除：

1. 删除结果记录时删除。

### 8. 源表空间索引检查

价值：

1. 判断源表是否具备基础空间查询能力。
2. 为准备过程和能力诊断提供提示。

产出：

1. 检查结论：是否存在源几何列 GiST 索引、索引是否有效、索引名称。

存储：

1. execution metadata。
2. 可选写入结果 metadata 作为诊断摘要。

删除：

1. 该动作只读检查，不产生可删除对象。

### 9. 源表空间索引创建

价值：

1. 对源 CRS 下的空间查询、部分准备前过滤和后续非 MVT 能力有帮助。

风险：

1. 会修改业务源表环境。
2. 可能影响源库写入、锁等待和 DBA 管理策略。
3. 删除时很难判断是否仍被其他业务使用。

建议：

1. 第一阶段不打包为默认动作。
2. 后续如支持，必须是独立显式授权动作。
3. 不建议在删除快显性能优化结果时自动删除源表索引。

产出：

1. 源业务表上的 GiST 索引。

存储：

1. 源业务 schema。

删除：

1. 默认不随快显性能优化结果删除。
2. 如未来提供删除入口，必须单独展示风险和归属。

### 10. 几何有效性处理

价值：

1. 降低少量无效几何导致派生目标构建或瓦片生成失败的概率。

风险：

1. `ST_MakeValid` 可能改变几何类型或几何结构。
2. 大表上成本较高。
3. 可能引入用户难以理解的数据语义变化。

建议：

1. 第一阶段只检测和记录，不默认修复。
2. 后续如果支持修复，应只修复派生目标中的几何，不修改源几何。

产出：

1. 默认产出为诊断统计。
2. 显式启用修复时，产出修复后的派生几何。

存储：

1. 诊断进入 execution metadata。
2. 修复结果进入派生目标。

删除：

1. 诊断随 execution 保留。
2. 修复几何随派生目标删除。

### 11. 低层级简化或聚合目标

价值：

1. 对超大面数据，低层级瓦片即使有 3857 索引也可能过大。
2. 简化或聚合目标可以降低低 zoom 的瓦片体积和浏览器压力。

建议：

1. 不进入第一阶段默认包。
2. 后续作为“多尺度快显优化”专题设计。
3. 不应与第一阶段 3857 派生目标混成一个不可拆分的大任务。

产出：

1. 简化表、聚合表或 zoom 到 target 的映射。

存储：

1. 第一阶段默认存储在源表所在 schema。
2. 快显性能优化结果 metadata 或后续独立结果表。

删除：

1. 如果由 Manager 创建并登记归属，应随对应优化结果删除。

## 八、已有 3857 数据的处理

### 1. 源表本身就是 3857 且已有有效 GiST 索引

这种情况已经满足动态 MVT 的关键性能条件。

建议：

1. capability 直接识别为可使用源表作为 3857 渲染目标。
2. UI 不推荐创建快显性能优化任务。
3. API 创建任务时应返回明确诊断：`already_optimized_by_source_3857`，不创建无价值任务定义。
4. 第一阶段不保留兼容分支；任务定义校验和执行校验都应拒绝 `source_srid=3857` 的快显性能优化任务。

原因：

1. 源表不是 Manager 创建的优化结果。
2. 删除“优化结果”不应让用户误以为会删除源表或源索引。
3. 避免为了展示一致性创建没有生命周期意义的假产物。

### 2. 已有 Manager 创建的快显性能优化结果

建议：

1. 不创建重复任务。
2. 允许刷新原任务或创建新的同目标任务但必须有唯一性约束，避免同一 item、几何列、目标 SRID 下出现多个当前结果。
3. capability 使用 `ready` 结果作为动态 MVT 和瓦片缓存生成的首选目标。

### 3. 已有非 Manager 创建的 3857 物化视图或派生表

该场景需要谨慎处理。

建议：

1. 对同源 PG 引擎、同源 schema 下能被轻量验证的 3857 物化视图，能力合成应识别为只读快显性能目标，并用于动态 MVT 和瓦片缓存生成。
2. 可用性验证必须同时满足：物化视图已 populated、目标几何列存在、目标几何实际 SRID 为 3857、目标几何列有有效 GiST 索引。
3. 自动识别的外部目标不写入 `manager.quick_view_optimization`，不出现在结果列表中，也不获得 Manager 生命周期所有权。
4. 删除或刷新仍只作用于 Manager 创建并登记的结果，不能删除、刷新或重建外部目标。
5. 如果无法轻量验证为可用目标，capability 不消费该对象，继续给出快显性能优化建议。

后续如果设计“登记外部优化目标”专题，应遵守以下边界：

1. 应先在专题中定义新的目标形态和生命周期语义，不复用第一阶段 Manager 自建目标或自动识别外部目标的 `target_kind`。
2. 删除结果只删除 Manager 登记记录，不删除外部表、外部物化视图或外部索引。
3. 刷新该目标不由 Manager 自动执行，除非用户明确把它迁移为 Manager 管理目标。

## 九、派生目标形态

快显性能优化结果不应强制只能是源 schema 下的物化视图。

建议统一概念为：

> 快显性能优化目标。

第一阶段推荐实现形态：

```text
源 PG 引擎内、源表所在 schema 下的 ADDP 命名物化视图
```

原因：

1. 它仍在源数据所在 PG 引擎和数据库内，不需要跨库复制用户数据。
2. 物化视图不修改源表原始记录，符合“独立任务生成独立结果”的边界。
3. 用户可见、可理解，能直接知道该结果服务于源表快显性能。
4. 可以稳定保存源主键、最小属性列和 `geom_3857`。
5. 不要求第一阶段额外配置独立派生 schema。

同库独立 schema 和派生表可以作为未来实现形态，但不作为第一阶段默认路线：

| 形态 | 优点 | 风险 |
| --- | --- | --- |
| 源 schema 物化视图 | 第一阶段最直观，用户可见，不需要额外 schema 配置 | 需要严格命名和登记，避免与业务对象混杂 |
| 源 PG 内 ADDP 管理派生 schema | 隔离性更好，删除边界更清晰 | 需要 schema 权限和引擎级配置，用户理解成本更高 |
| 源 PG 内 ADDP 管理派生表 | 便于 staging / swap / 删除 | 语义不如物化视图直观，刷新逻辑需要自管 |
| 外部已存在目标 | 可复用已有 DBA 成果 | 归属不清，删除和刷新不能自动处理 |

因此文档和 API 应避免把任务命名为“创建 3857 物化视图任务”。“快显性能优化任务”更稳定。

## 十、与瓦片缓存生成的关系

快显性能优化结果与瓦片缓存结果是两个独立产物：

| 对象 | 回答的问题 |
| --- | --- |
| 快显性能优化结果 | 当前是否有高性能 3857 渲染目标。 |
| 瓦片缓存结果 | 当前是否有可直接读取的 MVT 瓦片缓存。 |

关系：

1. 动态 MVT 优先使用 ready 的快显性能优化结果。
2. 瓦片缓存生成优先使用 ready 的快显性能优化结果。
3. 没有 ready 快显性能优化结果时，动态 MVT 仍可进行，但作为快显能力开放时必须标记慢路径，并受单瓦片响应时间预算、超时保护和体积限制约束；记录数只作为风险诊断和默认渲染源推荐依据。
4. 有可索引 3857 目标后，动态 MVT 标记为 ready 3857 目标路径；如果仍发生超时，应提示生成瓦片缓存，并按既有 TTL 允许后续重试。
5. 没有 ready 快显性能优化结果时，瓦片缓存生成仍允许执行且不做数据量限制，但非 3857 源表应给出更强的慢路径风险提示和“建议先执行快显性能优化”推荐。
6. 删除快显性能优化结果不删除已有瓦片缓存。
7. 删除瓦片缓存不删除快显性能优化结果。

是否把快显性能优化作为瓦片缓存生成子任务：

1. 概念上不作为子任务。
2. UI 上提供“先优化再生成瓦片缓存”的分步引导；空间预览中的动态 MVT 超时建议不只使用一次性消息，还会在当前预览区域保留可操作提示，按推荐动作跳转到“执行快显优化”或“生成瓦片缓存”。
3. 执行上应形成两个 execution，并通过 `parent_execution_id` 或 metadata 关联。
4. 第一阶段可以先跳转到快显性能优化任务；后续再做一键串联。

动态 MVT 阈值验证作为本专题的后续前置工作：

1. 验证应同时覆盖源表 `ST_Transform` 路径、源表已是 3857 且有索引路径、Manager ready 快显性能优化目标路径和外部只读 3857 目标路径。
2. 验证样本应覆盖小、中、大记录规模和点、线、面几何，输出无可索引 3857 目标和有可索引 3857 目标两组响应时间预算与超时处理建议。
3. 输出指标至少包含单瓦片耗时、服务端总耗时、超时率、错误率、原始 MVT 大小、空瓦片率和 DB 资源占用。
4. 验证结论只用于动态 MVT capability 性能模式、提示等级、`QUICK_VIEW_REALTIME_TILE_TIMEOUT_MS` / `QUICK_VIEW_REALTIME_TILE_RETRY_AFTER_SEC` 配置建议和运行时保护；瓦片缓存生成不因动态 MVT 响应时间预算而被禁止。

## 十一、删除语义

删除快显性能优化结果时，允许删除：

1. Manager 创建并登记的 3857 物化视图；未来如支持派生表，也只删除 Manager 创建并登记的派生表。
2. Manager 创建在派生目标上的索引。
3. Manager 保存的优化结果记录。
4. 与该结果相关的动态 MVT 目标缓存。
5. 与 capability 相关的运行时缓存。

不得自动删除：

1. 源业务表。
2. 源表原有索引。
3. 用户或 DBA 创建的外部物化视图。
4. 已生成的 MVT 瓦片缓存。
5. Meta 源 facts。

如果删除失败：

1. 不删除 `manager.quick_view_optimization` 结果记录。
2. 结果状态进入 `failed`。
3. `error_message` 记录 cleanup failure 摘要。
4. `metadata.cleanup_error` 和 `metadata.cleanup_failed_at` 记录清理失败细节和时间。
5. 不得在未删除实际 PG 对象时把结果伪装为已清理。

## 十二、已确认决策和后续专题

### 1. 已确认决策

| 问题 | 决策 |
| --- | --- |
| 稳定 `task_type` | 使用 `quick_view_optimization`。 |
| 第一阶段结果形态 | 使用源 PG 引擎内、源表所在 schema 下的 ADDP 命名 3857 物化视图。 |
| 外部已有 3857 目标 | 第一阶段自动识别并消费同源 schema 下可轻量验证的只读 3857 目标；不写入结果表、不进入结果列表、不获得 Manager 生命周期所有权。后续单独设计“登记外部优化目标”。 |
| 当前 ready 结果数量 | 同一 `tenant_id + item_fingerprint + geometry_column + target_srid + target_kind` 只允许一个当前 ready 结果。 |
| 任务自身定时刷新 | 第一阶段不支持，TaskProvider 声明 `supports_schedule=false`。 |
| Orchestrator 编排触发 | 支持作为 Orchestrator Step 引用已保存任务定义。 |
| 标准取消能力 | 第一阶段不支持，TaskProvider 声明 `supports_cancel=false`。 |
| 低层级简化或聚合目标 | 不进入第一阶段，后续进入多尺度快显优化专题。 |

### 2. 实现前最小决策

#### 物化视图命名

第一阶段物化视图命名规则：

```text
addp_qvo_<hash24>
```

其中：

1. `hash24` 由 tenant、`item_fingerprint`、geometry column 和目标 SRID 计算得到，取 24 位稳定十六进制摘要。
2. 名称统一使用小写字母、数字和下划线。
3. 不直接拼接源表名或 geometry column 原名，避免超长、大小写、特殊字符和引用转义问题。
4. 完整来源信息写入 `manager.quick_view_optimization.metadata`。

示例：

```text
addp_qvo_a1b2c3d4e5f69f8e7d6c1a2b3
```

#### 物化视图字段

第一阶段物化视图字段固定为：

| 字段 | 说明 |
| --- | --- |
| `source_row_id` | 源记录标识，统一转为 text。 |
| `geom_3857` | 转换后的 EPSG:3857 几何。 |
| 用户选择的最小属性列 | 用于样式、tooltip、分类和基础识别。 |

`source_row_id` 生成规则：

1. 源表有单列主键时，使用主键值并转为 text。
2. 源表有复合主键时，使用 JSON 字符串表达复合主键并转为 text。
3. 源表没有主键时，仍允许创建优化结果，但标记 `source_identity=reduced`，点击回查、高亮定位等能力降级。
4. 第一阶段不使用 `ctid` 作为可靠源记录标识。

属性列规则：

1. 默认不复制全字段。
2. 只复制任务配置中选择的最小属性列。
3. 第一阶段属性列只允许非几何业务属性；源几何列和目标几何列 `geom_3857` 不得作为属性列进入物化视图。
4. `source_row_id` 是系统保留列，不能作为属性列配置。
5. 属性列按不区分大小写的列名去重；重复列应在任务创建或更新阶段直接拒绝。
6. 超大文本、`bytea`、大 JSON 字段默认不进入物化视图。

#### 刷新策略

第一阶段采用轻量 staging / swap：

```text
create materialized view addp_qvo_<...>_build_<execution短码>
create gist index on build view
analyze build view
drop old view if exists
alter materialized view build rename to final
update manager.quick_view_optimization ready
```

约束：

1. 不采用直接 `DROP old -> CREATE new` 的破坏性刷新，避免失败后丢失旧 ready 结果。
2. 第一阶段不默认使用 `REFRESH MATERIALIZED VIEW CONCURRENTLY`，避免引入唯一索引要求和更复杂的刷新约束。
3. 构建新物化视图或索引失败时，旧 ready 结果继续保留。
4. swap 失败时不得把结果伪装为 ready，必须记录错误并保留可诊断状态。

#### API 边界

任务定义 CRUD：

```http
GET    /api/v1/manager/quick_view_optimization_tasks
POST   /api/v1/manager/quick_view_optimization_tasks
GET    /api/v1/manager/quick_view_optimization_tasks/{id}
PUT    /api/v1/manager/quick_view_optimization_tasks/{id}
DELETE /api/v1/manager/quick_view_optimization_tasks/{id}
```

TaskProvider 标准入口：

```http
GET  /api/v1/manager/tasks?task_type=quick_view_optimization
GET  /api/v1/manager/tasks/quick_view_optimization/{id}
POST /api/v1/manager/tasks/quick_view_optimization/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

第一阶段已实现后端 API 和任务体系接入。第二阶段已提供 Manager 前端创建/编辑 UI，TaskProvider capability 中的 `create_url=/manager/quick-view-optimization?tab=tasks` 与 `edit_url=/manager/quick-view-optimization?tab=tasks&task_id=:id` 指向当前承载页面。Orchestrator 应通过标准 API 调用任务，不依赖该 UI 路由。

结果管理：

```http
GET    /api/v1/manager/quick_view_optimization
GET    /api/v1/manager/quick_view_optimization/{id}
DELETE /api/v1/manager/quick_view_optimization/{id}
```

Capability 只暴露诊断和推荐，不触发创建动作：

```json
{
  "optimization": {
    "status": "missing | ready | stale | building | failed | not_required",
    "recommended": true,
    "reason": "large_non_3857_pg_table",
    "task_type": "quick_view_optimization",
    "result_id": null
  }
}
```

#### 已是 3857 的源表

源表本身是 3857 且几何列已有有效 GiST 索引时：

1. capability 返回 `optimization.status=not_required`。
2. 不推荐创建快显性能优化任务。
3. 用户强行通过 API 创建时返回业务错误：`already_optimized_by_source_3857`。
4. 不创建伪造的 Manager 优化结果，避免删除语义混乱。

### 3. 后续专题

后续需要单独设计：

1. 外部已有 3857 目标登记、校验、使用和删除边界。
2. 快显性能优化任务自身定时刷新，包括 stale 规则、刷新窗口、引擎级并发限制和失败保护。
3. 标准取消能力，包括 PG 语句取消、staging 对象清理和旧 ready 结果保留。
4. 多尺度快显优化，包括低层级简化、聚合目标和 zoom 到 target 的选择策略。

## 十三、验证命令

文档检查：

```bash
rg -n "quick_view_optimization|source_schema_materialized_view|external_3857_materialized_view|快显性能优化|快显性能优化目标|3857 派生" docs/next manager/docs docs/spec -g '*.md'
git diff --check -- docs/next/Manager快显性能优化任务与结果专题设计.md
```
