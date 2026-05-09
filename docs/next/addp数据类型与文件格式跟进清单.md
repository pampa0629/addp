# ADDP 数据类型与文件格式跟进清单

更新时间：2026-05-08

本文只保留当前接力所需信息，已完成内容尽量压缩为结论，不再保留长过程记录。

## 接力标记

- 最近更新时间：2026-05-08
- 当前状态：
  - 旧 attributes、旧枚举、旧过渡入口、旧路径查询已清理。
  - `common/attributes` 已迁移为 `common/jsonmap`。
  - Meta item 识别、claims、resolver、detector、single resource 推断、容器 children 枚举和 attributes 落库构造已收口到 `meta/backend/internal/metaitem`。
  - `meta/backend/internal/service` 的通用 helper 已拆到 `metaattr`、`repository`、`scanchange`、`extractor`、`metapath`、`metatext`、`objectstore`、`metacatalog`、`enginecap`、`scanstats`、`metacleanup`、`scantask`、`metaquery`。
  - 代码侧不保留旧数据兼容。
- 架构共识：
  - `common/jsonmap` 只做 decoded JSON map 读写。
  - `data_type` 与 `format` 保留在 common 层。
  - Meta item 的识别、claims、exclusive、`component_files`、`meta_item.full_name` 和 attributes 落库构造属于 Meta。
  - `common/dataitem` 仅保留跨模块纯概念和格式 / data_type 推断。
- 已完成结论：
  - 格式与数据类型总体模型已新增：`docs/next/addp格式与数据类型总体模型.md`。
  - Format Capability 与 Data Type Provider 接口草案已新增：`docs/next/addp格式Capability与DataTypeProvider接口草案.md`。
  - 文件格式能力接口规范已新增：`docs/next/addp文件格式能力接口规范.md`。
  - 资源读取抽象与 Format Provider 调用链草案已新增：`docs/next/addp资源读取抽象与FormatProvider调用链草案.md`。
  - 资源读取抽象规范已新增：`docs/next/addp资源读取抽象规范.md`。
  - Provider 消费者调研已新增：`docs/next/addp格式与数据类型Provider消费者调研.md`。
  - `common/dataitem` Meta 语义已出清。
  - `Scanner*`、`format.Scanner`、`ObjectInfoParser`、`format.ObjectInfo` 已出清。
  - 对象存储基础信息进入 `storage`，图片/PDF 提取信息进入 `type_info.media` / `type_info.document`，提取状态进入 `capabilities.extraction`。
  - TableInfo canonical model 已统一到 `plugin.TableInfo` / `format.FieldInfo`。
  - Excel / SQLite / GeoPackage 容器内部枚举已接入 single 容器扫描路径。
  - NoSQL collection 写入 `type_info.table`，图 schema 写入 `type_info.graph`。
  - GeoTIFF/TIFF 的可确定空间信息已写入 `capabilities.spatial`。
- 下一步优先级：
  1. 做真实环境旧数据清空与 meta 重扫，验证新 attributes 端到端生成。
  2. 确认 Transfer 任务配置与前端向导如何消费 Meta 标准 attributes。
  3. 继续拆 `meta/backend/internal/service` 剩余大文件中职责清晰的 helper。
- 当前阻塞：
  - 第三方插件 manifest、Manager preview 插件 manifest、Registry 能力发现视图仍需规范确认。
  - Transfer 配置迁移需要任务样例、前端配置口径和读写器接口确认。
  - 真实重扫和端到端验证需要运行环境与样例数据。
  - GeoTIFF 真实样例仍需端到端验证。
- 最近验证：
  - `go test ./common/jsonmap ./common/dataitem ./common/format ./common/format/image ./common/format/parquet ./common/spatial ./common/engine/plugin ./common/engine/plugins/postgresql ./common/engine/plugins/mysql ./common/engine/plugins/clickhouse ./common/engine/plugins/doris ./common/engine/plugins/spark_sql ./common/engine/plugins/minio ./common/engine/plugins/s3 ./common/engine/plugins/mongodb ./common/engine/plugins/neo4j ./common/resource ./meta/backend/internal/enginecap ./meta/backend/internal/extractor ./meta/backend/internal/metaattr ./meta/backend/internal/metacatalog ./meta/backend/internal/metacleanup ./meta/backend/internal/metaitem ./meta/backend/internal/metapath ./meta/backend/internal/metatext ./meta/backend/internal/metaquery ./meta/backend/internal/objectstore ./meta/backend/internal/repository ./meta/backend/internal/scanchange ./meta/backend/internal/scanstats ./meta/backend/internal/scantask ./meta/backend/internal/service ./manager/backend/internal/service ./transfer/backend/internal/api ./transfer/backend/internal/service` 通过。

## 仍待确认

- 第三方插件扩展声明 manifest
- Manager preview 插件 manifest
- Registry 能力发现视图
- Iceberg 等整体数据集按 `whole` 验证 Exclusive 和 claims
- Manager 空间预览依赖字段和缺失降级策略
- 旧数据删除后重新扫描
- Manager 使用 `meta_item.full_name` 和 `item.component_files`
- Transfer 不重复推断字段类型和空间能力
- ResourceReader / ComponentReader 是否在真实调用链稳定后沉淀到 `common/engine/plugin`
- ResourceReader / ComponentReader / NativeCursor 的最终 Go 接口形态
- CSV、JSON 空间结构、Shapefile、Excel、SQLite、GeoPackage、图片、PDF 端到端验证
