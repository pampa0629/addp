# ADDP 数据类型与文件格式改进清单

更新时间：2026-05-05

本文是 ADDP 在“数据类型、文件格式、组合形态、扩展语义、attributes 治理”方向上的状态板和接力清单。

相关文档分工：

- `addp数据类型与文件格式概念规范.md`：概念边界。
- `addp数据类型与文件格式落地指南.md`：实现原则。
- `transfer数据类型与文件格式后续事项.md`：Transfer 模块后续工作，当前阶段单独跟踪。
- 本文：当前状态、主要差距、下一步。

## 一、核心共识

1. 存储引擎只回答“数据在哪里”。
2. 数据家族回答“数据长什么样”。
3. 组合形态回答“一套资源如何组成一个 meta item”。
4. 文件格式回答“单个文件如何编码”。
5. 扩展信息回答“附加语义”，空间不是独立数据家族。
6. `FieldInfo` 只属于结构化数据语义，应挂在 `TableInfo` 下。
7. `meta` 是 item 识别和入库的权威来源。
8. `manager` 只消费 `meta` 已扫描入库的 item，不预览未扫描资源。
9. `meta_item.attributes` 采用“受控核心 + 开放扩展”。

目标链路：

```text
资源树扫描
  -> 组合形态推断
  -> meta item 归并
  -> 数据家族判断
  -> 文件格式识别
  -> 元数据与扩展信息提取
  -> attributes 归一化
  -> manager / transfer / asset / search 消费标准 item
```

目标 attributes 结构：

```json
{
  "schema_version": 1,
  "storage": {},
  "item": {},
  "schema": {},
  "extensions": {}
}
```

## 二、当前实现快照

### `common/dataitem`

已成为组合形态推断主入口，已具备：

- `CompositionType`、`DataFamily`、`DetectedItem`
- `CompositeItemDetector` 和 registry
- `ResolveDirectory`
- `InferSingleFileItem` / `InferSingleFile`
- `InferFormat` / `InferDataFamily`
- `BuildAttributes`

已支持：

- Shapefile 多文件 detector。
- 单文件 SQLite / GeoPackage 识别为 `container_file`。
- 湖表分区目录树识别为 `directory_tree`。
- 对象存储祖先前缀候选，支持部分跨层组件归并。

仍需增强：

- 更多目录树 detector。
- 更多跨层辅助文件规则。
- 混合集合单 item。
- 嵌套组合形态。

### `common/format`

仍是格式识别和解析共享核心，已具备：

- `TableInfo`、`ObjectInfo`、`FieldInfo`、`ExtensionInfo`
- `FileTableParser`、`DBTableParser`、`ObjectInfoParser`、`DocCollectionParser`
- `DetectFormat`
- MIME 转换和 TypeMapper

原则：

- 不承载组合形态主语义。
- parser / extractor 输出解析结果和扩展信息，不直接决定 attributes 最终结构。

### `meta`

已开始按资源树和组合规则工作：

- 文件系统目录项进入 `common/dataitem.ResolveDirectory`。
- 文件系统目录树候选可使用递归文件视图，匹配后整目录落一个 item。
- 文件系统普通文件、对象存储单对象已通过 `common/dataitem` 补齐标准 item attributes。
- 对象存储同前缀和祖先前缀候选已进入组合检测。
- 关系表、NoSQL collection、图 label / relationship 已源头写入基础 `attributes.item` / `attributes.schema`。
- 查询接口已优先读 `attributes.schema` 和 `attributes.extensions.spatial`。
- `ScanRepository.UpsertItemSelective` 已接入 attributes normalizer，并补充核心字段冲突保护。
- `attributes.extensions` 已增加标准命名空间和私有命名空间约束，非法扩展统一收纳到 `extensions.unqualified`。
- 对象提取元数据已开始写入 `extensions.media`、`extensions.document`、`extensions.statistics`。
- 搜索索引构建已优先消费 `extensions.document` 中的文档标题、作者、页数、词数、关键词和日期等字段。
- 文件系统和对象存储扫描资源节点、Parquet 目录树 detector、共享前端 catalog 适配、Manager 图片元信息展示已优先消费标准 attributes 分区。

仍需收口：

- 扫描中间结构尚未完全统一。
- 迁移期仍有平铺字段双写和手工兼容字段，但 normalizer 已保证分区字段优先。
- 标准扩展写入已起步，读取侧需继续补齐非核心展示和插件私有扩展场景。

### `manager`

已完成第一版确定性路由：

- `PreviewResolver` 要求存在 MetaItem / MetaNode。
- 取不到 MetaItem / MetaNode 返回未扫描错误。
- 优先按 `item_type`、`data_family`、`format` 路由 provider。
- 共享属性读取 helper 已优先读 `attributes.item` / `attributes.storage` / `attributes.schema` / `attributes.extensions.spatial`。
- 对象预览和文件表预览已优先使用 `entry_path`、`physical_path`、`content_type`、`size_bytes` / `total_size`。
- 搜索结果、对象预览和图谱 schema 展示已开始优先读取 `storage`、`schema`、`extensions.document` 分区。
- 图片尺寸、图片格式、颜色模式等对象展示字段已优先读取 `extensions.media`。

### `service` / `develop` / `common`

已推进：

- `service` 湖表模式判断优先读取 `attributes.item.mode`。
- `develop` DuckDB 湖表注册和 `common/duckdb` SQL 改写优先读取 `attributes.storage.physical_path`。
- `common/resource` 树节点对象计数优先读取 `attributes.storage.object_count`。
- `common/resource` 表空间元数据优先读取 `attributes.extensions.spatial`。

仍需收口：

- provider 内部仍有历史兜底格式推断。
- `PreviewRegistry.Resolve` 的 `Supports + priority` 机制仍作为兼容层存在。
- 还需要删除 provider 优先级抢语义路由。

## 三、主要差距

### 1. 组合形态覆盖

已覆盖第一批多文件、容器文件、湖表目录树和对象存储跨层候选。

待补：

- 更多多文件规则。
- 更多容器文件规则。
- 更多目录树 detector。
- 混合集合单 item。
- 嵌套组合形态。
- 对象存储更多真实跨层组件场景。

### 2. 扫描语义统一

仍存在：

- 个别路径先按扩展名或格式分支。
- 个别路径直接构造 attributes。
- 数据库表、文件表、对象、集合之间仍有不同中间模型。

### 3. attributes 治理

第一版已具备：

- `schema_version`
- `storage`
- `item`
- `schema`
- `extensions`
- 核心字段：`composition_type`、`data_family`、`format`、`entry_path`、`component_files`、`physical_path`、`fields`
- 分区字段优先于平铺字段的冲突保护。
- 第三方扩展命名空间约束：平台标准命名空间为 `spatial`、`media`、`document`、`statistics`；私有命名空间应使用反向域名或插件 ID 形式；不合规 key 进入 `extensions.unqualified`。
- 对象提取元数据到 `extensions.media`、`extensions.document`、`extensions.statistics` 的第一版映射。

待补：

- 增加扫描回归校验，禁止新增不在兼容清单内的平铺字段。
- 补齐更多 parser / extractor 的标准扩展映射和读取路径。
- 为第三方插件建立显式扩展声明机制。

### 3.1 平铺字段兼容层删除计划

迁移期保留以下平铺读取兼容字段，但新增平台逻辑必须优先读取分区：

- `storage` 兼容字段：`bucket`、`path`、`relative_path`、`name`、`physical_path`、`size_bytes`、`size`、`total_size`、`content_type`、`last_modified_at`、`modified_at`、`etag`、`file_type`、`object_count`、`schema_name`。
- `item` 兼容字段：`composition_type`、`data_family`、`format`、`entry_path`、`component_files`、`file_count`、`mode`。
- `schema` 兼容字段：`fields`、`table_metadata`、`table_type`、`table_comment`、`primary_key`、`indexes`、`row_count`、`document_count`、`count`。
- 标准扩展兼容字段：`spatial_metadata`。

删除节奏：

1. 当前阶段：normalizer 继续双写上述兼容字段，读取侧优先消费分区字段。
2. 下一阶段：清理已完成初筛，非 Transfer 核心代码中未再发现清单内平铺字段的直接读取；后续新增读取必须走统一 helper 或标准分区。
3. 再下一阶段：增加扫描回归校验，禁止新增不在清单内的平铺字段。
4. 最终阶段：完成存量数据迁移后，停止 normalizer 双写平铺字段，仅保留离线迁移脚本或诊断工具中的旧数据读取能力。

### 4. 空间扩展

空间应统一作为 `attributes.extensions.spatial`。

待补：

- 统一 `SpatialInfo` 使用口径。
- 梳理 shapefile、geojson、postgresql、影像空间元数据。
- 明确哪些预览能力依赖空间扩展。
- 避免硬编码空间字段名或按格式判断空间能力。

### 5. 平行模型与注册体系

仍需梳理：

- `common/engine/plugin.TableInfo`
- `common/engine/plugin.ObjectInfo`
- `common/format.ScannerTableInfo`
- `common/format.ScannerFieldInfo`
- `common/format` registry
- `common/dataitem` detector registry
- `manager.ObjectContentRegistry`

## 四、阶段状态

| 阶段 | 状态 | 当前结论 |
|---|---|---|
| 阶段 0：文档与共识 | 已完成 | 三份文档定位已清楚，后续随实现同步维护。 |
| 阶段 1：`common/dataitem` 与组合形态 | 第一版已完成，继续增强 | 主入口已建立，继续补 detector 和真实场景。 |
| 阶段 2：数据家族与格式识别分离 | 第一版已完成，继续治理 | 空间不作为数据家族，格式不承载组合形态。 |
| 阶段 3：attributes 治理 | 第一版已完成，继续治理 | 分区结构、normalizer、冲突保护、命名空间约束和删除计划已落地，继续补读取迁移和扩展映射。 |
| 阶段 4：空间扩展收口 | 待推进 | 需要统一标准空间扩展口径。 |
| 阶段 5：manager 预览路由 | 第一版已完成，继续删除历史逻辑 | 确定性路由已开始，继续删 provider 兜底和优先级抢路由。 |
| 阶段 6：模型与注册收口 | 待推进 | 需要减少平行模型和重复 registry。 |

## 五、当前优先级

### P0

- 增加扫描回归校验，禁止新增不在兼容清单内的平铺字段。
- 继续清理 provider 历史兜底格式推断和优先级抢语义路由。
- 保持本文和概念规范、落地指南术语同步。
- Transfer 相关事项已拆到 `docs/next/transfer数据类型与文件格式后续事项.md`，本阶段不纳入当前实施范围。

### P1

- 增强对象存储更多目录树和混合集合归并能力。
- 统一空间扩展口径。
- 收敛旧扫描中间结构。
- 删除 `manager` 预览旧兜底逻辑。

### P2

- 收拢平行 registry。
- 建立第三方插件扩展声明机制。
- 建立更统一的能力发现层。

## 六、后续接力点

1. 增加扫描回归校验，禁止新增不在兼容清单内的平铺字段。
2. 补齐 `extensions.media`、`extensions.document`、`extensions.statistics` 更多 parser / extractor 写入和读取。
3. 建立第三方插件扩展声明机制，让私有命名空间可校验、可展示、可索引。
4. 删除 `manager` 预览旧兜底逻辑和 provider 优先级抢路由。
5. 为对象存储更多目录树、混合集合补 detector，并覆盖跨层组件真实扫描用例。
6. 继续迁移剩余 manager provider，删除 provider 内部格式猜测和优先级抢语义路由。
7. 梳理数据库表、文档集合与 `dataitem` 模型的边界，决定是否引入“引擎原生 item”组合形态。
8. 收口 `TableInfo` / `ObjectInfo` 与 `Scanner*` 模型。
9. 审核 `common/format`、`common/dataitem`、`manager` 的 registry 语义，形成统一接入路径。
