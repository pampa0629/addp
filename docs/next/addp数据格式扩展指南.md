# ADDP 数据格式扩展指南

本文是 ADDP 数据类型、组织方式、文件格式和横切能力扩展的 next 阶段入口规范。概念边界以 [ADDP 数据类型与格式体系图](addp数据类型与格式体系图.md) 为准。

## 术语统一

spec 层统一采用以下术语，不保留旧术语兼容写入：

| 术语 | 字段 / 取值 | 说明 |
|---|---|---|
| 组织方式 | `attributes.item.organization` | 资源如何归并成 data item |
| single | `organization=single` | 一个引擎资源对应一个 data item，不等同于“单文件” |
| multi | `organization=multi` | 多个明确组件资源共同构成一个 data item |
| whole | `organization=whole` | 整个目录、prefix、schema 或扫描范围构成一个 data item |
| 数据类型 | `attributes.item.data_type` | 用户如何理解和处理 data item |
| 文件格式 | `attributes.item.format` | data item 的编码方式或格式族 |
| 类型信息 | `attributes.type_info` | 某个数据类型的通用元数据 |
| 格式信息 | `attributes.format_info` | 某个具体格式才有的描述 |
| 横切能力 | `attributes.capabilities` | spatial、temporal、statistics、extraction 等跨类型能力 |

`meta_item.item_type` 是表字段事实源，不等同于 `attributes.item.data_type`。平台语义应以 data item、organization、data_type、format、type_info、format_info、capabilities 的组合表达。

旧字段和旧枚举不做兼容读取或兼容写入。发现仍依赖旧字段的数据和代码，应直接暴露问题并通过重新 meta 扫描或代码修正解决。

## 相关规范

本主题拆分为以下文档：

| 文档 | 内容 |
|---|---|
| [ADDP 数据项 detector 规范](addp数据项detector规范.md) | `ResolveItems`、组织方式、claims、exclusive、`FormatRule` |
| [ADDP 元数据 attributes 规范](addp元数据attributes规范.md) | `attributes.storage/item/type_info/format_info/capabilities` 分区、唯一事实源、扩展命名空间 |
| [ADDP 内置数据格式规范](addp内置数据格式规范.md) | CSV/TSV、Excel、GeoJSON、Shapefile、Parquet/ORC/Avro、SQLite/GeoPackage、图片、PDF 的落地规则 |
| [ADDP 数据类型与文件格式待规范事项](addp数据类型与文件格式待规范事项.md) | 尚未定稿、需要讨论后才能开发的事项 |

## 基本原则

实现层必须从 `meta` 扫描入口出发，而不是从单个文件格式出发：

```text
资源树扫描
  -> 组织方式推断
  -> meta item 归并
  -> 数据类型判断
  -> 文件格式识别
  -> 类型信息、格式信息和横切能力提取
  -> attributes 归一化
  -> manager / transfer / asset / search 消费标准 data item
```

必须遵守：

1. 存储引擎只提供资源位置、catalog 和基础存储属性。
2. 组织方式优先于文件格式识别。
3. `meta` 是 data item 识别和落库的权威来源。
4. `manager`、`transfer` 等消费已入库 meta item，不重新推断资源归并关系。
5. `attributes` 采用“受控核心 + 开放能力”。
6. 新增格式、变更组织方式或新增横切能力时，先修订对应规范，再开发。

## 扫描主流程

1. `meta` 从存储引擎获取资源树候选集合。
2. 通过 Meta 内部 detector / resolver 推断组织方式。
3. 归并并落库稳定 `meta_item`。
4. 在 data item 层判断 `data_type` 和主 `format`。
5. 调用 parser / extractor 提取 `type_info`、`format_info` 和 `capabilities`。
6. 通过 normalizer 写入标准 attributes 分区。

## meta item 身份规则

组织方式确认后，`meta` 先生成稳定 data item，再提取类型信息、格式信息和横切能力。`meta_item` 表字段语义不得被 detector 任意改变。

| 场景 | `meta_item.name` 来源 | `meta_item.full_name` 来源 |
|---|---|---|
| `organization=single` | 入口资源名，文件资源保留扩展名 | 入口资源完整路径或引擎原生全名 |
| `organization=multi` | 入口资源名，文件资源保留扩展名 | 入口资源完整路径 |
| `organization=whole` | 根目录、prefix、schema 名，或规范定义的数据集名 | 整体范围的完整路径或引擎原生范围名 |
| 引擎原生 item | 引擎原生名称 | schema.table / database.collection 等原生全名 |

容器文件不产生独立的组织方式。SQLite、GeoPackage、Excel、ZIP 等外层 data item 通常是 `organization=single`、`data_type=container`；内部 table、sheet、layer、文件先在 `type_info.container.children` 或格式规范声明的位置表达。只有当需要独立授权、检索、血缘、传输或生命周期管理时，才讨论是否升格为独立 meta item。

除非经过规范确认并得到批准，不得改变 `meta_item.name`、`meta_item.full_name`、`meta_item.item_type` 等既有表字段语义。

## 模块职责

### meta

`meta` 负责资源树扫描、组织方式推断、data item 归并、数据类型判断、格式识别、元数据提取、fingerprint 维护和 attributes normalizer。

Meta 内部维护 data item detector registry 和扫描 resolver。新增或修改 detector 时，应接入 Meta 的扫描流程；不得在 common 包中通过 `init()` 自动注册 Meta item 识别逻辑。

### common 层数据类型概念

`data_type` 等跨模块稳定概念可以保留在 common 层，供 Meta、Manager、Transfer 等模块共享。它们只表达平台通用语义，不负责资源树扫描、detector registry、claims / exclusive 合并或 `meta_item.full_name` 决策。

### common/format

`common/format` 负责格式识别、类型信息、格式信息、字段类型映射、parser / extractor。它不直接决定 meta item 如何归并，也不绕过 normalizer 写最终 attributes。

### common/jsonmap

`common/jsonmap` 是 decoded JSON map 的通用读写 helper，用于读取嵌套 section、字符串、数字、时间等基础值。它不承载 attributes 规范语义，不能作为 `meta_item.attributes` 的业务模型或 normalizer。

### manager

`manager` 只消费 meta 已入库 item。预览路由基于 `item_type` 表字段、`meta_item.full_name`，以及 `attributes.item.organization`、`attributes.item.data_type`、`attributes.item.format`、`attributes.item.component_files`、`attributes.storage.physical_path` 等标准属性，不按后缀或 provider 优先级重新猜测 item。

### transfer

`transfer` 应消费标准化后的 meta item 和 attributes，不重复判断组织方式，不重复推断字段类型。Transfer 后续事项单独记录在 [Transfer 数据类型与文件格式后续事项](transfer数据类型与文件格式后续事项.md)。

## 新增格式步骤

### 新增 single 组织方式格式

1. 明确 `organization=single`、`data_type`、`format`。
2. 增加 `FormatRule` 或内置 single 规则。
3. 实现格式识别和 parser / extractor。
4. 定义 `type_info`、`format_info` 和可选 `capabilities`。
5. 接入 meta 扫描、attributes normalizer 和 manager 预览。

### 新增 multi 组织方式格式

1. 定义组件规则、入口资源、必需组件、可选组件和 claimed resources。
2. 实现 `ScopeItemDetector`，支持一个扫描范围产出多个 item。
3. 明确是否允许递归组件，默认不得跨目录或跨 prefix 猜测。
4. 实现 parser / extractor。
5. 接入 manager / transfer，使用 `meta_item.full_name` 定位主资源，使用 `component_files` 读取组件资源。

### 新增 container 数据类型格式

1. 定义容器文件自身 item 语义，通常为 `organization=single`、`data_type=container`。
2. 定义容器内部对象如何写入 `type_info.container.children`；未形成规范前不得展开为独立 meta item。
3. 实现容器识别和内部元数据枚举。
4. 定义容器级 `type_info`、`format_info` 和内部对象关系。

### 新增 whole 组织方式数据集

1. 定义整体范围强匹配规则、可忽略辅助文件和独占条件。
2. 由扫描入口提供递归观察资源。
3. detector 返回 claimed resources 和 `Exclusive=true`。
4. 将数据集格式私有信息写入 `format_info`，将分区、索引等横切能力写入 `capabilities`。

## 验证要求

新增或修改能力后，至少验证：

1. `meta` 是否正确归并 item。
2. `meta_item.name/full_name/node_id/item_type` 是否符合规范。
3. `attributes.storage/item/type_info/format_info/capabilities` 是否完整且无重复事实源。
4. claimed resources 是否避免重复落库。
5. 未认领资源是否继续被识别。
6. manager 树位置和预览入口是否正确。
7. fingerprint 是否稳定。
8. normalizer 是否保护平台核心字段。
9. 私有字段是否进入合规命名空间。

## 禁止事项

- 不要先按单个文件格式生成 item，再事后合并。
- 不要把目录、prefix、schema 当作天然 data item；只有明确 detector 或引擎原生边界声明时才可形成 item。
- 不要把容器当作组织方式。
- 不要把空间、时间、统计、提取等横切能力放进数据类型。
- 不要把 Parquet 直接称为湖表；Parquet 是 table 类型的一种文件格式，Iceberg 等表格式目录才可按规范形成 `whole` item。
- 不要默认空间字段名一定是 `geom`。
- 不要让 manager 重复实现 meta 的组织方式识别规则。
- 不要让 manager 预览未扫描、未入库的资源。
- 不要让 provider 通过优先级抢路由决定 item 类型或文件格式。
- 不要让 transfer 重复推断字段类型。
- 不要让 parser / extractor / 第三方插件直接覆盖平台核心 attributes。
- 不要把格式私有属性写入 attributes 顶层或长期写入未命名空间字段。
