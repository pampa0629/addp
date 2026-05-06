# ADDP 数据类型与文件格式跟进清单

更新时间：2026-05-06

本文作为 next 阶段数据类型与文件格式体系的后续工作清单。当前要求不保留旧数据兼容；旧数据删除后重新 meta 扫描，旧代码路径应尽早暴露并清理。

## 一、文档整理

- [x] 将数据类型与格式概念文档移动到 `docs/next`。
- [x] 将数据格式扩展、detector、attributes、内置格式规范移动到 `docs/next`。
- [x] 将待规范事项按新概念重写。
- [x] 新增第三方插件扩展声明构想文档。
- [x] 新增 Manager 内容预览插件能力构想文档。
- [x] 新增 Registry 与能力发现层构想文档。
- [ ] 检查全仓文档链接，清理指向旧 `docs/concepts` / `docs/spec` 路径的引用。

## 二、规范确认

- [ ] 确认 whole scope 的 `explain/confidence` 是否入库及字段位置。
- [ ] 确认对象存储跨层组件 whole scope 的 claimed resources 表达。
- [ ] 确认第三方插件扩展声明 manifest。
- [ ] 确认 Manager preview 插件 manifest。
- [ ] 确认 Registry 能力发现视图。
- [ ] 确认引擎原生 item 的 `format` 和 `entry_path` 口径。

## 三、实现跟进清单

- [ ] 清理旧 attributes 读取：`schema`、`extensions`、平铺字段、`composition_type`、`data_family`。
- [ ] 清理旧枚举写入：`single_file`、`multi_file`、`container_file`、`directory_tree`、`mixed_collection`。
- [ ] 更新 meta normalizer，只生成 `storage/item/type_info/format_info/capabilities`。
- [ ] 更新 detector 输出模型，统一 `organization=single|multi|whole`。
- [ ] 容器类只生成外层 item，内部对象写入 `type_info.container.children`。
- [ ] Shapefile 按 `multi` 验证 claims、entry_path、component_files。
- [ ] Iceberg 等整体数据集按 `whole` 验证 Exclusive 和 claims。
- [ ] 引擎原生 item 按 `single` 验证，不引入 `engine_native`。
- [ ] TableInfo / ObjectInfo / Scanner* 模型收口：以新 `type_info` 语义重新确认 canonical model，删除不再需要的 Scanner 适配层。
- [ ] ObjectInfo 拆分：存储侧对象信息进入 `storage`，媒体和文档信息进入 `type_info.media` / `type_info.document`。
- [ ] 文档集合采样结构确认进入 `type_info.table`、`type_info.document` 或后续单独规范。
- [ ] 图 label / relationship 结构确认进入 `type_info.graph`。
- [ ] `capabilities.spatial` 最小字段集确认：geometry_column、geometry_type、geometry_types、srid、extent、dimension、has_spatial_index。
- [ ] 多几何字段表达方式确认。
- [ ] GeoTIFF / 栅格影像空间语义确认：继续使用 `spatial`，还是新增 raster 能力。
- [ ] PostGIS、Shapefile、GeoJSON、GeoPackage、GeoTIFF 的 spatial 字段映射对齐。
- [ ] Manager 空间预览依赖字段和缺失降级策略确认。

## 四、验证清单

- [ ] 旧数据删除后重新扫描，确认生成新 attributes。
- [ ] 旧字段数据触发错误时信息清晰可定位。
- [ ] Manager 不按扩展名或 MIME 重新猜测组织方式。
- [ ] Transfer 不重复推断字段类型和空间能力。
- [ ] Search / Asset 消费新 attributes 分区。
- [ ] 新规范下 CSV、GeoJSON、Shapefile、Excel、SQLite、GeoPackage、图片、PDF 端到端验证。
