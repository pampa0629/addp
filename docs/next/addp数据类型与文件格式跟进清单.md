# ADDP 数据类型与文件格式跟进清单

更新时间：2026-05-06

本文作为 next 阶段数据类型与文件格式体系的后续工作清单。当前要求不保留旧数据兼容；旧数据删除后重新 meta 扫描，旧代码路径应尽早暴露并清理。

## 文档维护要求

- [x] 每次推进本清单相关开发、修复、验证或规范确认后，必须同步更新本文。
- [x] 同步内容至少包括：完成项勾选、未完成项进展说明、阻塞点、验证命令和验证结果。
- [x] 未完整落地或仍有已知残留的事项不得直接勾选完成，应在条目下记录“已完成部分 / 剩余部分”。

## 一、文档整理

- [x] 将数据类型与格式概念文档移动到 `docs/next`。
- [x] 将数据格式扩展、detector、attributes、内置格式规范移动到 `docs/next`。
- [x] 将待规范事项按新概念重写。
- [x] 新增第三方插件扩展声明构想文档。
- [x] 新增 Manager 内容预览插件能力构想文档。
- [x] 新增 Registry 与能力发现层构想文档。
- [ ] 检查全仓文档链接，清理指向旧 `docs/concepts` / `docs/spec` 路径的引用。

## 二、规范确认

- [x] 确认 whole scope 独占语义：`organization=whole` 覆盖范围内其他资源不得再落 item；`item.scope_exclusive=true`、`item.claim_policy=whole_scope` 写入 attributes。
- [x] 确认对象存储跨层规则：默认禁止跨 bucket、跨目录、跨 sibling prefix 认领；遇到真实格式需求再讨论。
- [x] 确认 `entry_path` 口径：不作为标准 attributes 字段；data item 定位事实源统一为 `meta_item.full_name`。
- [x] 确认引擎原生 item 的 `format` 口径：无格式私有信息时不写 `format` 和 `format_info`。
- [x] 确认 Scanner* 口径：`ScannerTableInfo / ScannerFieldInfo` 是旧适配层，不再扩展，后续删除。
- [x] 确认 `capabilities.spatial` 最小字段集：`geometry_columns`、`primary_geometry_column`、`extent`、`has_spatial_index`；Geometry 类型只写声明或格式可确定类型；`srid` 与 `crs` 二选一。
- [ ] 确认第三方插件扩展声明 manifest。
- [ ] 确认 Manager preview 插件 manifest。
- [ ] 确认 Registry 能力发现视图。

以上未确认的插件化和能力发现事项不阻塞 meta / attributes / detector 主线重构。

## 三、实现跟进清单

### 新会话优先实现顺序

1. 清理旧字段读取和写入，先让旧路径暴露错误。
2. 更新 attributes normalizer，只生成 `storage/item/type_info/format_info/capabilities`。
3. 更新 detector 输出语义，改为 `organization=single|multi|whole`，删除旧枚举。
4. 去掉标准 attributes 中的 `entry_path`，统一用 `meta_item.full_name` 定位主资源或 whole scope 根范围。
5. 落地 `whole` 独占：`scope_exclusive=true`、`claim_policy=whole_scope`，覆盖范围内其他资源不再落 item。
6. 落地 `multi`：主文件写入 `meta_item.full_name`，组件写入 `item.component_files`。
7. 收口 `type_info.table`、`format_info`、`capabilities.spatial`，再处理 Manager / Transfer 消费。

### 具体任务

- [ ] 清理旧 attributes 读取：`schema`、`extensions`、平铺字段、`composition_type`、`data_family`。
  - 已完成：`common/attributes` 去掉平铺 fallback；`meta/backend/internal/service`、`manager/backend/internal/service` 主要读取路径已切到 `type_info` / `format_info` / `capabilities`。
  - 剩余：`manager/frontend/src/components/explorer/PreviewPanel.vue` 仍读取 `extensions.*`；全仓还需继续排查其他前端和非核心模块消费。
- [x] 清理旧枚举写入：`single_file`、`multi_file`、`container_file`、`directory_tree`、`mixed_collection`。
- [x] 更新 meta normalizer，只生成 `storage/item/type_info/format_info/capabilities`。
- [x] 更新 detector 输出模型，统一 `organization=single|multi|whole`。
- [x] 删除标准 attributes 中的 `entry_path` 写入和读取；主资源、whole scope 根范围统一使用 `meta_item.full_name`。
- [x] `organization=whole` 写入 `item.scope_exclusive=true`、`item.claim_policy=whole_scope`，并确保覆盖范围内其他资源不再落 item。
- [ ] 禁止对象存储跨 bucket、跨目录、跨 sibling prefix 认领；manifest 外部引用先诊断，不生成跨范围 item。
- [ ] 容器类只生成外层 item，内部对象写入 `type_info.container.children`。
- [x] Shapefile 按 `multi` 验证 claims、`meta_item.full_name` 主文件、`component_files`。
- [ ] Iceberg 等整体数据集按 `whole` 验证 Exclusive 和 claims。
- [x] 引擎原生 item 按 `single` 验证，不引入 `engine_native`。
- [ ] 引擎原生 item 无格式私有信息时不写 `attributes.item.format` 和 `format_info`。
- [ ] TableInfo / ObjectInfo / Scanner* 模型收口：以新 `type_info` 语义重新确认 canonical model；`ScannerTableInfo / ScannerFieldInfo` 不再扩展并最终删除。
  - 已完成：数据库表字段、NoSQL 字段 / 索引、文件解析字段等主要写入路径已改为 `type_info.table`。
  - 剩余：`ScannerTableInfo / ScannerFieldInfo` 旧适配层尚未删除；canonical model 还需单独收口。
- [ ] ObjectInfo 拆分：存储侧对象信息进入 `storage`，媒体和文档信息进入 `type_info.media` / `type_info.document`。
- [ ] 文档集合采样结构确认进入 `type_info.table`、`type_info.document` 或后续单独规范。
- [ ] 图 label / relationship 结构确认进入 `type_info.graph`。
- [ ] `capabilities.spatial` 按最小字段集落地：`geometry_columns`、`primary_geometry_column`、`extent`、`has_spatial_index`。
  - 已完成：Shapefile 写入 `capabilities.spatial.geometry_columns` / `primary_geometry_column` / `extent` / `has_spatial_index`；Meta 查询与 Manager 后端空间读取已切到 `capabilities.spatial`。
  - 剩余：PostGIS / GeoJSON / GeoPackage / GeoTIFF 映射仍需逐项对齐；部分数据库空间扫描仍需从旧 `spatial_metadata` 结构收口为最小结构。
- [ ] Geometry 字段类型只写声明或格式可确定类型；PostGIS 声明为 `geometry` 时就写 `geometry`，不扫描全表推断实际类型。
- [ ] `srid` 与 `crs` 二选一：能确定 EPSG/SRID 编号写 `srid`；不能确定编号但有 CRS 描述写 `crs`。
- [ ] GeoTIFF / 栅格影像空间语义先可写 `capabilities.spatial` 的范围和坐标参考；是否新增 raster 能力后续再讨论。
- [ ] PostGIS、Shapefile、GeoJSON、GeoPackage、GeoTIFF 的 spatial 字段映射对齐。
- [ ] Manager 空间预览依赖字段和缺失降级策略确认。

## 四、验证清单

- [ ] 旧数据删除后重新扫描，确认生成新 attributes。
- [x] 旧字段数据触发错误时信息清晰可定位。
  - 已验证：`common/attributes` 不再 fallback 到 attributes 平铺字段；旧平铺字段不会被 normalizer 迁移到新分区。
- [ ] Manager 不按扩展名或 MIME 重新猜测组织方式。
- [ ] Manager 使用 `meta_item.full_name` 定位主资源，使用 `item.component_files` 读取 multi 组件。
  - 已完成：Manager 后端预览路由和文件 provider 已停止读取 `entry_path`；后端属性读取切到 `item.component_files`。
  - 剩余：前端展示和完整 Shapefile 端到端仍需验证。
- [ ] Transfer 不重复推断字段类型和空间能力。
- [ ] Search / Asset 消费新 attributes 分区。
  - 已完成：Manager search 后端文档字段读取切到 `type_info.document`。
  - 剩余：Asset 及其他搜索索引消费路径仍需排查。
- [ ] 新规范下 CSV、GeoJSON、Shapefile、Excel、SQLite、GeoPackage、图片、PDF 端到端验证。

### 最近验证记录

- 2026-05-06：通过 `go test ./common/dataitem/... ./common/format/parquet ./meta/backend/internal/service`。
- 2026-05-06：通过 `go test ./common/attributes ./common/resource ./meta/backend/internal/service ./manager/backend/internal/service`。
- 2026-05-06：尝试 `go test ./common/... ./meta/backend/internal/service ./manager/backend/internal/service`，失败项与本次改动无关：`common/format/csv` 测试引用不存在的 `ParseSchema/ReadRecords/CountRecords`；`common/scheduler` 的 `empty_cron_expression` 期望失败但当前无错误。
