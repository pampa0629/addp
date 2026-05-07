# ADDP 数据类型与文件格式跟进清单

更新时间：2026-05-07

本文作为 next 阶段数据类型与文件格式体系的后续工作清单。当前要求不保留旧数据兼容；旧数据删除后重新 meta 扫描，旧代码路径应尽早暴露并清理。

## 文档维护要求

- [x] 每次推进本清单相关开发、修复、验证或规范确认后，必须同步更新本文。
- [x] 同步内容至少包括：完成项勾选、未完成项进展说明、阻塞点、最近一次有效验证命令和结果。
- [x] 未完整落地或仍有已知残留的事项不得直接勾选完成，应在条目下记录“已完成部分 / 剩余部分”。
- [x] 每次更新必须维护“接力标记”小节，便于新会话直接接力；已完成内容可精简为结论，细节保留在 Git diff / 提交记录中。
- [x] 验证记录只保留最近一次有效验证；不要追加流水账。若有失败验证，只在失败仍影响接力时保留一条“当前失败 / 原因 / 下一步”。

## 接力标记

> 后续新会话优先阅读本节，再查看未完成复选框。

- 最近更新时间：2026-05-07
- 当前状态：旧 attributes 读取/写入、旧枚举、旧过渡入口、旧路径查询已清理；`common/attributes` 已迁移为 `common/jsonmap`；Meta attributes 落库构造、detector registry、扫描 resolver、Shapefile detector、Parquet whole-scope detector 已收口到 `meta/backend/internal/metaitem`。`meta/backend/internal/service` 已继续按职责拆分：attributes helper 进入 `metaattr`，扫描仓储进入 `repository`，对象元数据提取器进入 `extractor`，对象/文件路径规则进入 `metapath`，文本截断常量和 helper 进入 `metatext`；对象存储复合 item 检测/命名进入 `metaitem`，对象存储客户端缓存、配置解析和内容读取进入 `objectstore`；扫描任务参数读取、调度表达式构造和执行进度 reporter 进入 `scantask`。代码侧不保留旧数据兼容。
- 最近架构共识：
  - `common/jsonmap` 只是 decoded JSON map 读写工具，不承载 attributes 规范语义；`common/attributes` 包已删除。
  - `data_type` 是平台通用概念，仍应放 common 层；`format`、各类 type info / format info parser 和 analyzer 也属于 common/format。
  - 构成 meta item 的资源组织方式、识别、claims、exclusive、`component_files`、`meta_item.full_name` 决策和 attributes 落库构造属于 Meta 模块职责；跨模块需要时通过 Meta Client 消费结果，不把识别逻辑下沉到 common。
  - 当前 `common/dataitem` 仍保留 detector 接口 / 结果模型 / 规则类型；全局 registry、统一 resolver、attributes 落库构造和内置 detector 实现已迁回 Meta。
- 下一步优先级：
  1. 继续拆分 `common/dataitem` 中仍偏 Meta 的 `DetectedItem` / `DetectionResult` / detector 接口归属，决定迁入 `metaitem` 后 common 是否只保留 `DataType`、`Organization`、`FormatRule` 等纯概念。
  2. 继续梳理 `meta/backend/internal/service` 剩余大文件，优先按对象存储持久化、扫描任务状态机、索引读取 helper 等职责拆分，避免 service 目录再次堆积。
  3. 边界收口后，再接入 Excel / SQLite / GeoPackage 容器内部 `type_info.container.children` 真实枚举。
  4. 删除旧数据后重新触发 meta 扫描，验证新 attributes 端到端生成。
- 当前阻塞：第三方插件 manifest、Manager preview 插件 manifest、Registry 能力发现视图仍需规范确认；真实重扫和端到端验证需要运行环境与样例数据。
- 最近验证：`go test ./common/jsonmap ./common/dataitem/... ./common/format/parquet ./meta/backend/internal/extractor ./meta/backend/internal/metaattr ./meta/backend/internal/metaitem ./meta/backend/internal/metapath ./meta/backend/internal/metatext ./meta/backend/internal/objectstore ./meta/backend/internal/repository ./meta/backend/internal/scantask ./meta/backend/internal/service ./manager/backend/internal/service` 通过。

## 一、文档整理

- [x] 将数据类型与格式概念文档移动到 `docs/next`。
- [x] 将数据格式扩展、detector、attributes、内置格式规范移动到 `docs/next`。
- [x] 将待规范事项按新概念重写。
- [x] 新增第三方插件扩展声明构想文档。
- [x] 新增 Manager 内容预览插件能力构想文档。
- [x] 新增 Registry 与能力发现层构想文档。
- [x] 检查全仓文档链接，清理指向旧 `docs/concepts` / `docs/spec` 路径的引用。
  - 已验证：`rg -n "docs/(concepts|spec)/addp(数据类型|数据格式|数据项|元数据attributes|内置数据格式|第三方插件|Manager内容预览|Registry)|addp数据格式扩展指南\\.md|addp数据项detector规范\\.md|addp元数据attributes规范\\.md|addp内置数据格式规范\\.md" docs -g '*.md'`，数据类型与格式相关入口均指向 `docs/next`。

## 二、规范确认

- [x] 确认并落文档：`common/attributes` 迁移为 `common/jsonmap`，`common/attributes` 不再作为 attributes 规范包占位。
  - 结论：`common/jsonmap` 是通用 decoded JSON map helper；`common/attributes` 包已删除，调用方已改为 `github.com/addp/common/jsonmap`。
  - 已更新文档：`docs/next/addp元数据attributes规范.md`、`docs/next/addp数据类型与格式体系图.md`、`docs/concepts/addp共享模块介绍.md`、`common/CLAUDE.md`、`common/README.md`。
- [x] 确认并落文档：`data_type`、`format`、type info / format info parser 和 analyzer 属于 common；Meta item 资源组织方式、识别逻辑、claims、exclusive 和 attributes 落库构造属于 Meta。
  - 已讨论共识：跨模块需要 item 信息时通过 Meta Client 获取，不把 Meta item 识别流程作为 common 能力暴露。
  - 已更新文档：`docs/next/addp数据类型与格式体系图.md`、`docs/next/addp元数据attributes规范.md`、`docs/next/addp数据项detector规范.md`、`docs/next/addp数据格式扩展指南.md`、`docs/concepts/addp共享模块介绍.md`、`common/CLAUDE.md`、`common/README.md`。
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

1. 继续拆分 `common/dataitem` 中仍偏 Meta 的 `DetectedItem` / `DetectionResult` / detector 接口归属。
2. 继续梳理 `meta/backend/internal/service` 剩余扫描和任务相关大文件，按 scanner / extractor / repository / metapath / metaattr / metatext / metaitem / objectstore / scantask / indexer 等职责拆分。
3. 边界收口后，再推进容器内部 children 枚举、Scanner* 删除、ObjectInfo 拆分和 spatial 映射对齐。

### 具体任务

- [x] 清理旧 attributes 读取：`schema`、`extensions`、平铺字段、`composition_type`、`data_family`。
  - 结论：Meta / Manager / Search / Copilot 相关消费已切到 `storage/item/type_info/format_info/capabilities`；不再读取旧平铺字段、`extensions.*`、旧 `relative_path` 或 attributes 顶层 `bucket/path/name/content_type`。
  - 说明：`spatial_metadata` 在引擎能力声明 schema 中仍作为 engine capability 字段名存在，不属于 meta item attributes。
- [x] 清理旧枚举写入：`single_file`、`multi_file`、`container_file`、`directory_tree`、`mixed_collection`。
- [x] 删除旧过渡入口和旧命名：`ResolveDirectory`、`InferSingleFile`、`SingleFileInput`、`BuiltinSingleFileRules`、`MatchBuiltinSingleFileRule`、`ExtractDirectoryTreeInfo`。
  - 结论：single 资源推断入口统一为 `InferSingleResource` / `SingleResourceInput`；Parquet whole scope 入口统一为 `ExtractWholeScopeInfo`；不保留旧别名。
- [x] 更新 meta normalizer，只生成 `storage/item/type_info/format_info/capabilities`。
- [x] 将 `common/attributes` 迁移为 `common/jsonmap`，并替换 Meta / Manager / Service / Develop / common 内调用方。
- [x] 将 attributes 落库构造从 `common/dataitem` 迁入 Meta metaitem 包。
  - 结论：`metaitem.BuildAttributes` 目前在 `meta/backend/internal/metaitem`，负责将 `DetectedItem` 合并为 `storage/item/...` 可落库结构；`common/dataitem` 不再暴露 `BuildAttributes`。
- [x] 将 detector registry / 统一扫描 resolver 从 `common/dataitem` 迁入 Meta metaitem 包。
  - 结论：Meta 通过 `meta/backend/internal/metaitem` 显式组装 detector 并执行 `metaitem.ResolveItems`；`common/dataitem` 不再暴露 `Register`、`GetAll`、`ResolveItems`，`common/format/detector` 旧兼容包已删除。
- [x] 将 Shapefile detector 与 Parquet whole-scope detector 实现迁入 Meta metaitem 包。
  - 结论：`common/dataitem/shapefile` 已删除；`common/format/parquet` 保留 Parser、lake table 基础判断和 Manager 预览读取函数。Meta item 识别实现位于 `meta/backend/internal/metaitem`。
- [x] 将 Meta attributes helper、扫描仓储、对象元数据提取、路径规则和文本截断 helper 从 `service` 拆出。
  - 结论：`metaattr` 负责标准 attributes 分区和字段 attributes 构造；`repository.ScanRepository` 负责扫描仓储；`extractor.MetadataExtractor` 负责对象元数据提取、缓存和按需提取；`metapath` 负责对象路径、FS CatalogPath 和 full_name 拼接规则；`metatext` 负责文档内容/预览截断常量和 helper。`service` 保留扫描编排和业务流程。
- [x] 将对象存储复合 item 检测和对象存储客户端管理从 `service` 拆出。
  - 结论：对象存储目录候选分组、claims 过滤、复合 item 命名、单资源 item type 推断已进入 `metaitem/object_storage_items.go`；MinIO/S3 配置解析、客户端缓存、对象内容读取已进入 `objectstore.ClientManager`。`scan_object_storage_service.go` 保留扫描编排、对象元数据转换和持久化流程。
- [x] 将扫描任务 helper 从 `service` 拆出。
  - 结论：扫描任务 JSON 参数读取、存储类型规范化、Cron 表达式构造、执行进度 reporter 已进入 `scantask`。`scan_task_service.go` 保留任务生命周期、调度注册、执行状态流转和数据库编排。
- [x] 更新 detector 输出模型，统一 `organization=single|multi|whole`。
- [x] 删除标准 attributes 中的 `entry_path` 写入和读取；主资源、whole scope 根范围统一使用 `meta_item.full_name`。
- [x] `organization=whole` 写入 `item.scope_exclusive=true`、`item.claim_policy=whole_scope`，并确保覆盖范围内其他资源不再落 item。
- [x] 禁止对象存储跨 bucket、跨目录、跨 sibling prefix 认领；manifest 外部引用先诊断，不生成跨范围 item。
- [ ] 容器类只生成外层 item，内部对象写入 `type_info.container.children`。
  - 已完成：NFS / 对象存储 single 容器 item 不展开内部 meta item；外层 item 写入 `type_info.container.children=[]`、`child_count=0`、`resource_count=1` 作为未枚举摘要。
  - 剩余：Excel / SQLite / GeoPackage 的真实内部 sheet/table/layer 枚举尚未接入 meta 扫描阶段的 `type_info.container.children`。接入前先完成 common/jsonmap 与 Meta item 识别职责拆分，避免把容器 attributes 构造继续放错层。
- [x] Shapefile 按 `multi` 验证 claims、`meta_item.full_name` 主文件、`component_files`。
- [ ] Iceberg 等整体数据集按 `whole` 验证 Exclusive 和 claims。
- [x] 引擎原生 item 按 `single` 验证，不引入 `engine_native`。
- [x] 引擎原生 item 无格式私有信息时不写 `attributes.item.format` 和 `format_info`。
- [ ] TableInfo / ObjectInfo / Scanner* 模型收口：以新 `type_info` 语义重新确认 canonical model；`ScannerTableInfo / ScannerFieldInfo` 不再扩展并最终删除。
  - 已完成：数据库表字段、NoSQL 字段 / 索引、文件解析字段等主要写入路径已改为 `type_info.table`。
  - 剩余：`ScannerTableInfo / ScannerFieldInfo` 旧适配层尚未删除；canonical model 还需单独收口。收口时需遵守新边界：type info 模型在 common/format，Meta attributes 落库构造在 Meta。
- [ ] ObjectInfo 拆分：存储侧对象信息进入 `storage`，媒体和文档信息进入 `type_info.media` / `type_info.document`。
- [ ] 文档集合采样结构确认进入 `type_info.table`、`type_info.document` 或后续单独规范。
- [ ] 图 label / relationship 结构确认进入 `type_info.graph`。
- [ ] `capabilities.spatial` 按最小字段集落地：`geometry_columns`、`primary_geometry_column`、`extent`、`has_spatial_index`。
  - 已完成：Shapefile 与 PostGIS 写入 `capabilities.spatial.geometry_columns` / `primary_geometry_column` / `extent` / `has_spatial_index`；PostGIS 扫描已停止写旧 `spatial_metadata`；GeoJSON single item 写入默认 `geometry` / `srid=4326` / `has_spatial_index=false`；GeoTIFF/TIFF single item 先写入 spatial 能力壳（`extent=null`、`has_spatial_index=false`）；Meta 查询与 Manager 后端空间读取已切到 `capabilities.spatial`。
  - 剩余：GeoPackage 映射需结合 container 内部 layer 枚举；GeoTIFF 真实 extent / CRS 需接入栅格元数据读取。
- [x] Geometry 字段类型只写声明或格式可确定类型；PostGIS 声明为 `geometry` 时就写 `geometry`，不扫描全表推断实际类型。
- [ ] `srid` 与 `crs` 二选一：能确定 EPSG/SRID 编号写 `srid`；不能确定编号但有 CRS 描述写 `crs`。
- [x] GeoTIFF / 栅格影像空间语义先可写 `capabilities.spatial` 的范围和坐标参考；是否新增 raster 能力后续再讨论。
- [ ] PostGIS、Shapefile、GeoJSON、GeoPackage、GeoTIFF 的 spatial 字段映射对齐。
  - 已完成：PostGIS、Shapefile、GeoJSON、GeoTIFF/TIFF 最小映射。
  - 剩余：GeoPackage 需在 container/layer 枚举落地后补齐；GeoTIFF/TIFF 目前只有能力壳，真实范围和 CRS 仍待 extractor。
- [ ] Manager 空间预览依赖字段和缺失降级策略确认。

## 四、验证清单

- [ ] 旧数据删除后重新扫描，确认生成新 attributes。
  - 进展：代码侧不再兼容旧 attributes；旧数据必须删除后重新扫描。尚未执行真实环境删除和重扫验证。
- [x] 旧字段数据触发错误时信息清晰可定位。
  - 结论：旧平铺字段不会被 normalizer 迁移；Manager / Search / Meta item 查询不再读取平铺 fallback；旧 `shallow` scanDepth 不再自动转换为 `basic`。
- [x] Manager 不按扩展名或 MIME 重新猜测组织方式。
  - 已验证：预览路由读取 `attributes.item.data_type` / `item.format`；`FileTablePreviewProvider.resolveFormat` 只读标准 `item.format` / `storage.content_type`，不回退文件名。
- [ ] Manager 使用 `meta_item.full_name` 定位主资源，使用 `item.component_files` 读取 multi 组件。
  - 已完成：Manager 后端预览路由和文件 provider 已停止读取 `entry_path`；后端属性读取切到 `item.component_files`。
  - 剩余：前端展示和完整 Shapefile 端到端仍需验证。
- [ ] Transfer 不重复推断字段类型和空间能力。
- [x] Search / Asset 消费新 attributes 分区。
  - 结论：Manager Search、向量 metadata、Meta Meilisearch 索引均消费标准分区；Asset 自动发现接口当前不读取 meta item attributes。
- [ ] 新规范下 CSV、GeoJSON、Shapefile、Excel、SQLite、GeoPackage、图片、PDF 端到端验证。

### 最近验证记录

- 2026-05-07：通过 `go test ./common/jsonmap ./common/dataitem/... ./common/format/parquet ./meta/backend/internal/extractor ./meta/backend/internal/metaattr ./meta/backend/internal/metaitem ./meta/backend/internal/metapath ./meta/backend/internal/metatext ./meta/backend/internal/objectstore ./meta/backend/internal/repository ./meta/backend/internal/scantask ./meta/backend/internal/service ./manager/backend/internal/service`。
