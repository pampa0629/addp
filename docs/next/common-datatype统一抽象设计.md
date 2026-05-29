# Common Datatype 统一抽象接力记录

更新时间：2026-05-29

本文只作为本轮 `common/datatype` 统一抽象重构的接力记录，不再作为正式规范事实源。正式口径以 `docs/concepts/` 和 `docs/spec/` 下文档为准。

## 文档角色

- 概念层只回答“是什么”和“边界是什么”，不写 Go 接口、provider 方法、JSON 字段细节和迁移历史。
- 规范层回答“怎么落库、怎么接入、禁止什么、如何验收”。
- 本文只保留迁移状态、历史决策摘要、未完成队列和验证命令。

## 正式文档入口

- [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)
- [ADDP 元数据体系图](../concepts/addp元数据体系图.md)
- [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)
- [ADDP 元数据 attributes 规范](../spec/addp元数据attributes规范.md)
- [ADDP 引擎插件接口规范](../spec/addp引擎插件接口规范.md)
- [ADDP 存储引擎路径体系规范](../spec/addp存储引擎路径体系规范.md)
- [ADDP 数据类型与文件格式扩展指南](../spec/addp数据类型与文件格式扩展指南.md)
- [ADDP 数据引擎扩展指南](../spec/addp数据引擎扩展指南.md)
- [ADDP 内置数据类型与文件格式规范](../spec/addp内置数据类型与文件格式规范.md)
- [ADDP 元数据扫描机制规范](../spec/addp元数据扫描机制规范.md)

## 已完成状态

本轮已完成 `table`、`graph`、`document`、`media`、`container` 主事实收口，并删除 `file` 作为基础 data type 的路线。

| 事项 | 当前状态 | 正式归属 |
| --- | --- | --- |
| data type 基础集合 | `table/document/media/container/graph/unknown` 已成为当前基础集合；`file` 不再作为基础 data type | 数据类型和格式体系图；数据类型与格式能力规范 |
| `common/datatype` 事实源 | data type、type info、field type、field info、空间和访问索引等通用结构已统一到 `common/datatype` | 数据类型与格式能力规范 |
| table 主事实 | `format.TableInfo`、`format.FieldInfo`、`plugin.FieldInfo`、`plugin.ColumnInfo`、`common/models.FieldInfo` 等旧公共模型已删除或收敛；table item 主事实使用 `datatype.TableInfo` / `datatype.FieldInfo` | 数据类型与格式能力规范；引擎插件接口规范；元数据 attributes 规范 |
| graph 主事实 | graph item 使用 `datatype.GraphInfo`；Neo4j label、relationship type 和 endpoint pattern 是 graph 结构事实，不再作为独立 data item | 数据类型和格式体系图；元数据体系图；引擎插件接口规范；存储引擎路径体系规范 |
| graph 业务视图 | Neo4j Spatial 内部节点和关系不进入 GraphInfo、计数、样本、Graph Browser、Schema 推导、知识服务或 GDS 投影 | 引擎插件接口规范；数据类型与格式能力规范 |
| document 主事实 | `datatype.DocumentInfo` 只承载标题、页数、语言、编码、字数、大小等结构元信息；正文和提取状态不进入 `DocumentInfo` | 元数据体系图；数据类型与格式能力规范；内置数据类型与文件格式规范 |
| media 主事实 | `datatype.MediaInfo` 承载 kind、MIME、宽高、时长、编码、颜色空间和大小；音视频 codec、码率、采样率、轨道数等暂不进入主事实 | 数据类型和格式体系图；数据类型与格式能力规范；内置数据类型与文件格式规范 |
| container 主事实 | `datatype.ContainerInfo` 承载 child 数量、默认 child、child 轻量摘要和 refs；父容器不塞完整字段、样本行或子对象内容 | 数据类型和格式体系图；数据类型与格式能力规范；内置数据类型与文件格式规范 |
| MongoDB collection | 已按动态 schema 记录集合收尾：`item_type=collection`，`data_type=table`；字段画像走 `type_info.table.fields`；采样和索引事实进入 `capabilities.statistics` / `capabilities.indexing`；不写 `type_info.document`，不新增 `type_info.collection` 或 `document_collection` | 数据类型和格式体系图；元数据 attributes 规范；引擎插件接口规范；数据引擎扩展指南；存储引擎路径体系规范 |
| Manager / common client | 对象预览按需元数据触发已改为判断标准 attributes 是否已有主事实；提取结果只合并回 `ObjectPreview.attributes`；旧 extracted metadata 展示入口已删除 | 数据类型与格式能力规范；元数据体系图 |

## 已确认边界

- `type_info.*` 只保存对应 data type 的通用结构元信息。
- `format_info.<format>` 只保存格式私有事实。
- `capabilities.*` 保存 spatial、statistics、extraction、indexing 等横切能力事实。
- `access_index.<data_type>` 保存内容读取访问索引。
- 内容样本、原始内容、缩略图、正文片段、Manager DTO 和运行时查询结果不得写入 `type_info` 或 `format_info`。
- 文件、对象、目录、bucket、prefix、root 等只表达 catalog / storage 形态；内容无法识别时使用 `data_type=unknown`。
- `layout` 是 item 组织事实，不属于 `common/datatype`；最终 item layout 由 dataitem / Meta item 识别层确认。

## 归拢规则

后续如继续从本文向正式文档归拢，必须先读目标文档原文，再决定补充、合并或替换，不得直接搬运本文段落。

| 内容类型 | 应放位置 | 规则 |
| --- | --- | --- |
| 稳定概念、分类边界、术语关系 | `docs/concepts/` | 只讲“是什么”，不放接口名、方法名、JSON 字段细节 |
| attributes 落点、字段名、禁止项、验收标准 | `docs/spec/` | 可执行约束必须放规范层 |
| provider 接口、能力声明、扫描和预览消费规则 | `docs/spec/` | 属于实现契约和模块协作规范 |
| 跨模块共享接口和公共模型 | `docs/spec/` 或共享模块文档 | 只有形成平台契约时才进入全局规范 |
| 单模块内部技术实现、helper、局部流程 | 对应模块目录下的 README / docs / CLAUDE 导航文档 | 不进入全局 concepts/spec；例如 Meta 内部 attributes helper 应记录在 Meta 模块文档或仅留接力记录 |
| 迁移历史、旧模型删除记录 | 本文或删除 | 不进入概念层；除非形成稳定规范，否则不进入全局 spec |

## 暂缓事项

| 问题 | 当前状态 | 暂缓原因 | 后续方向 |
| --- | --- | --- | --- |
| `datatype.AccessIndex` 的长期包归属 | 当前仍暂居 `common/datatype` | 它不是 data type 本体，但 format、Meta、Manager preview 都需要复用同一结构 | 等 engine range reader、format access index、Meta attributes 边界更稳定后，再决定是否迁到独立访问索引包 |
| `layout` 的长期包归属 | 当前公开入口仍在 `common/format` 顶层，`common/dataitem` 复用 | 现阶段 `common/dataitem` 仍需要消费 format descriptor / capability，贸然拆包会引入循环或过早抽象 | 后续如 engine 也出现 table/topic/label 组合 item，再评估是否抽独立 item layout 包 |
| `format.TableDescribeResult` / `format.MediaDescribeResult` | 保留在 `common/format` provider 边界 | provider 一次解析自然可能得到 type info、format info、spatial、access index 等组合事实 | 不迁入 `common/datatype`；如果 document、container、graph 出现同类场景，再按同级事实组合原则设计 |
| media 细粒度音视频事实 | 暂不进入 `datatype.MediaInfo` | codec、码率、采样率、轨道数等尚未形成稳定跨格式消费链路 | 需要时进入受控 `format_info.<format>` 或 `capabilities.extraction`，不要提前塞进主事实 |
| container 真实样例体验 | 主事实已稳定，体验仍需核实 | ZIP、Excel、SQLite、GeoPackage 等 child resolver 的真实样例和 Manager 体验还可继续增强 | 做样例验证和预览体验改进，不改变父容器只存轻量 child 摘要的原则 |

## 下一阶段候选

1. container 后续增强：核实 ZIP、Excel、SQLite、GeoPackage 的真实样例和 Manager child resolver 体验；不得把 child 样本或完整字段塞回父 `ContainerInfo`。
2. media 后续增强：在真实消费方出现前，音视频 codec、bitrate、sample rate 等继续留在 format/extraction 边界。
3. 文档全文检索：DOCX / PPTX / WPS 的全文不进入 `DocumentInfo`；Meta 深度扫描或 extraction 任务负责调用 `DocumentTextReader` / 外部 extractor 抽取正文并写入 Meilisearch，attributes 只记录 `type_info.document`、`capabilities.extraction` 状态、预览或外部索引引用。

## 已完成验证

MongoDB dynamic schema 收尾相关验证：

```bash
go test ./common/engine/plugin ./common/engine/plugins/mongodb ./meta/backend/internal/metaattr ./meta/backend/internal/service ./manager/backend/internal/preview
```

Manager 前端验证：

```bash
cd manager/frontend
npm run build
```

JSON / i18n key 校验已执行过一次，确认 `manager.explorer.dynamicSchemaNotice` 和 `manager.explorer.emptyPreview.*` 可被组件引用。

## 后续维护要求

- 新增 data type、type info 字段或横切能力前，必须先修订正式 concepts / spec。
- 不保留旧字段、旧 provider、旧 DTO 或兼容分支。
- 如果正式文档和本文冲突，以正式文档为准；本文应被更新为接力状态，而不是反向覆盖正式规范。
- 已进入正式文档的长篇推导不再留在本文中重复维护。

## 本轮收尾判断

MongoDB collection 的 data type 归属和 dynamic schema 采样链路已可以收尾。下一步不建议继续扩大 MongoDB 改动面，优先处理 container 真实样例体验或文档全文检索链路。
