# ADDP 数据类型与文件格式跟进清单

更新时间：2026-05-09

本文只保留当前接力所需信息，已完成内容尽量压缩为结论，不再保留长过程记录。

## 接力标记

- 最近更新时间：2026-05-09
- 当前状态：
  - 旧 attributes、旧枚举、旧过渡入口、旧路径查询已清理。
  - `common/attributes` 已迁移为 `common/jsonmap`。
  - Meta item 识别、claims、resolver、detector、single resource 推断、容器 children 枚举和 attributes 落库构造已收口到 `meta/backend/internal/metaitem`。
  - `meta/backend/internal/service` 的通用 helper 已拆到 `metaattr`、`repository`、`scanchange`、`extractor`、`metapath`、`metatext`、`objectstore`、`metacatalog`、`enginecap`、`scanstats`、`metacleanup`、`scantask`、`metaquery`。
  - 代码侧不保留旧数据兼容。
- 架构共识：
  - `common/jsonmap` 只做 decoded JSON map 读写。
  - `format` 保留在 common 层；`data_type`、`organization` 和 detector 规则结构收口到 Meta 内部。
  - Meta item 的识别、claims、exclusive、`component_files`、`meta_item.full_name` 和 attributes 落库构造属于 Meta。
  - 不保留 `common/dataitem`；当前 item 组织方式枚举、规则结构和格式 / data_type 推断均属于 Meta 内部实现。
- 已完成结论：
  - 数据项概念总纲已收口到：`docs/concepts/addp数据项体系图.md`。
  - 数据类型与格式概念总纲已收口到：`docs/concepts/addp数据类型和格式体系图.md`。
  - 数据类型与格式文档体系已按唯一事实源收口：概念边界、探测器、attributes、format / provider / reader、resource、内置规则、扩展指南分别维护。
  - 数据项相关模块职责边界已迁移到 `docs/concepts/addp数据项体系图.md`，不再保留独立边界规范。
  - 概念文档已补充数据类型、典型格式和当前 ADDP 支持状态目录。
  - 数据类型与格式能力规范已整理：`docs/spec/addp数据类型与格式能力规范.md`。
  - 数据类型与格式能力规范已补充当前 FormatCapability 与 provider / reader / extractor 实现矩阵。
  - 资源读取抽象与 Format Provider 调用链已合并进：`docs/spec/addp资源读取抽象规范.md`。
  - 内置格式规范已按统一模板整理：识别与组织、attributes 写入、消费要求、格式约束。
  - 数据类型与文件格式扩展指南已简化为五步最小流程：判断组织方式、判断 data type / format、实现格式能力、定义 attributes、补充文档和验证。
  - Manager 预览、`common/format`、text / markdown / binary 兜底和第三方格式插件化推进已新增：`docs/next/addp格式预览与插件化扩展推进.md`。
  - Provider 消费者调研已新增：`docs/plan/addp格式与数据类型Provider消费者调研.md`。
  - Transfer 相关内容已整合为三份 plan 文档：`docs/plan/transfer现状与Provider化改造调研.md`、`docs/plan/transfer与FormatProvider整合方案.md`、`docs/plan/transferProvider化改造步骤与清理清单.md`。
  - Service / Develop 旧 `lake_table` 链路清理说明已新增：`docs/plan/service-develop旧lake_table链路清理说明.md`。
  - `common/format/scanner.go` 已拆分并归并到 `metadata.go`；`common/format/plugins/parquet/lake_table.go` 已改为 `table_file.go`，去掉 common 层 lake table 命名。
  - 具体内置格式实现已统一移动到 `common/format/plugins/*`，避免根目录堆放具体格式目录，也避免 `common/format/formats` 这种重复命名。
  - `common/format/README.md` 与 `common/format/mappers/README.md` 已按当前 Provider / capability / resource 边界重写，删除旧 scanner、旧 parser registry 和旧文档链接口径。
  - `format.ExtractInput` 已移除 `EngineID`，文件增强元数据提取器不再携带 engine 上下文。
  - `common/format/builtin` 已纳入 SpatiaLite type mapper 默认注册。
  - Meta 内部 `dataitem_lake_table_detector.go` 已改名为 `dataitem_table_file_detector.go`，内部符号统一为 table file / scope table 语义；扫描结果仍是 `item_type=table + format=parquet/orc/avro + organization=single/whole`。
  - `common/dataitem` 已下沉为 `meta/backend/internal/dataitem`。
  - 旧 scanner / object info 相关实现已出清。
  - 对象存储基础信息进入 `storage`，图片/PDF 提取信息进入 `type_info.media` / `type_info.document`，提取状态进入 `capabilities.extraction`。
  - TableInfo canonical model 已统一到 `plugin.TableInfo` / `format.FieldInfo`。
  - Excel / SQLite / GeoPackage 容器内部枚举已接入 single 容器扫描路径。
  - NoSQL collection 写入 `type_info.table`，图 schema 写入 `type_info.graph`。
  - GeoTIFF/TIFF 的可确定空间信息已写入 `capabilities.spatial`。
- 下一步优先级：
  1. 做真实环境旧数据清空与 meta 重扫，验证新 attributes 端到端生成。
  2. 按 Transfer Provider 化文档先落 `TransferPlan`，把 source / target 拆成 engine、resource、data_type、format、spatial、policy。
  3. 继续拆 `meta/backend/internal/service` 剩余大文件中职责清晰的 helper。
- 当前阻塞：
  - 第三方插件 manifest、Manager preview 插件 manifest、Registry 能力发现视图仍需规范确认。
  - Transfer 配置迁移需要任务样例、前端配置口径和读写器接口确认。
  - 真实重扫和端到端验证需要运行环境与样例数据。
  - GeoTIFF 真实样例仍需端到端验证。
- 最近验证：
  - `go test ./common/format/... ./common/resource ./meta/backend/internal/metaitem ./meta/backend/internal/service ./meta/backend/internal/extractor ./meta/backend/internal/objectstore ./meta/backend/internal/scanchange ./meta/backend/internal/metapath ./manager/backend/internal/service` 通过。
  - `go test ./meta/backend/internal/extractor ./meta/backend/internal/service ./meta/backend/internal/metaitem ./meta/backend/internal/objectstore ./meta/backend/internal/scanchange ./meta/backend/internal/metapath` 通过。
  - `go test ./manager/backend/internal/service` 通过。
  - 历史全量验证记录：`go test ./common/jsonmap ./common/format ./common/spatial ./common/engine/plugin ./common/engine/plugins/postgresql ./common/engine/plugins/mysql ./common/engine/plugins/clickhouse ./common/engine/plugins/doris ./common/engine/plugins/spark_sql ./common/engine/plugins/minio ./common/engine/plugins/s3 ./common/engine/plugins/mongodb ./common/engine/plugins/neo4j ./common/resource ./meta/backend/internal/dataitem ./meta/backend/internal/enginecap ./meta/backend/internal/extractor ./meta/backend/internal/metaattr ./meta/backend/internal/metacatalog ./meta/backend/internal/metacleanup ./meta/backend/internal/metaitem ./meta/backend/internal/metapath ./meta/backend/internal/metatext ./meta/backend/internal/metaquery ./meta/backend/internal/objectstore ./meta/backend/internal/repository ./meta/backend/internal/scanchange ./meta/backend/internal/scanstats ./meta/backend/internal/scantask ./meta/backend/internal/service ./manager/backend/internal/service ./transfer/backend/internal/api ./transfer/backend/internal/service` 通过。

## 仍待确认

- 第三方插件扩展声明 manifest
- Manager preview 插件 manifest
- Registry 能力发现视图
- text / markdown 内置格式和 unknown / binary 兜底预览口径
- Manager content handler 与 format capability 的统一派生机制
- Iceberg 等整体数据集按 `whole` 验证 Exclusive 和 claims
- Manager 空间预览依赖字段和缺失降级策略
- 旧数据删除后重新扫描
- Manager 全链路验证使用 `meta_item.full_name` 和 `item.component_files`
- Service / Develop 删除旧 `lake_table` 查询链路，改为 `table + format + organization`
- Transfer 不重复推断字段类型和空间能力
- ResourceReader / ComponentReader / NativeCursor 的最终 Go 接口形态
- CSV、JSON 空间结构、Shapefile、Excel、SQLite、GeoPackage、图片、PDF 端到端验证
