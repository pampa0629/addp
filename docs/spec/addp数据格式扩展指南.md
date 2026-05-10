# ADDP 数据格式扩展指南

本文是新增或修改数据格式时的最小工作流。概念边界见 [ADDP 数据类型与格式体系图](../concepts/addp数据类型与格式体系图.md)，跨模块职责见 [ADDP 数据类型与格式模块边界规范](addp数据类型与格式模块边界规范.md)。

## 适用范围

当需要让 ADDP 支持一种新的文件格式、容器格式、多组件格式、whole scope 数据集或引擎原生数据表示时，按本文执行。

新增格式不等于新增数据类型。绝大多数格式应落到既有 `data_type`：

- `table`
- `document`
- `media`
- `container`
- `graph`
- `unknown`

只有既有数据类型无法表达用户理解方式、预览方式、处理方式和治理方式时，才修订概念文档并新增 `data_type`。

## 最小流程

### 1. 判断组织方式

先回答“哪些资源组成一个 data item”：

| 组织方式 | 适用场景 | 例子 |
|---|---|---|
| `single` | 一个引擎资源对应一个 item | CSV、PDF、图片、SQLite 文件、数据库表 |
| `multi` | 多个明确组件资源共同构成一个 item | Shapefile、主文件 + 索引文件 |
| `whole` | 整个目录、prefix、schema 或扫描范围构成一个 item | Iceberg 表目录、OSGB 场景目录 |

容器不是组织方式。Excel、SQLite、GeoPackage、ZIP 等外层通常仍是 `organization=single`，内部对象先进入 `type_info.container.children`。

组织方式、主资源、组件、claims / exclusive 和 `meta_item.full_name` 的规则写入 [ADDP 数据项 detector 规范](addp数据项detector规范.md)。

### 2. 判断数据类型和格式

再回答“用户如何理解这个 item”和“它用什么格式编码”：

| 判断项 | 写入位置 | 说明 |
|---|---|---|
| 数据类型 | `attributes.item.data_type` | `table`、`document`、`media`、`container`、`graph`、`unknown` |
| 文件格式 | `attributes.item.format` | `csv`、`json`、`parquet`、`shapefile`、`pdf` 等 |
| 类型信息 | `attributes.type_info.<data_type>` | 字段、行数、页数、宽高、children、图结构等 |
| 格式信息 | `attributes.format_info.<format>` | 版本、编码、组件摘要、manifest、格式私有字段 |
| 横切能力 | `attributes.capabilities.<capability>` | spatial、temporal、statistics、extraction、partitioning 等 |

空间、时间、分区、索引、提取、语义等能力优先作为横切能力，不新增数据类型。

### 3. 实现格式能力

根据格式形态实现最小能力：

| 格式形态 | 必需工作 |
|---|---|
| `single + table` | 格式识别、`FormatRule`、parser、`TableProvider` |
| `multi + table` | `FormatRule`、组件规则、`ComponentTableProvider` |
| `whole + table` | whole scope 规则、scope 读取、`ScopeTableProvider` |
| `document` | 格式识别、metadata / text extractor，后续需要时实现 `DocumentProvider` |
| `media` | 格式识别、metadata extractor，后续需要时实现 `MediaProvider` |
| `container` | 外层 item 规则、children 枚举、后续需要时实现 `ContainerProvider` |
| `graph` | 图结构描述、样本或查询归一，后续需要时实现 `GraphProvider` |

provider 和 capability 规则见 [ADDP 文件格式能力与 Data Type Provider 规范](addp文件格式能力与DataTypeProvider规范.md)，读取抽象见 [ADDP 资源读取抽象规范](addp资源读取抽象规范.md)。

### 4. 定义 attributes 写入

按 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md) 明确字段归属：

- 平台核心字段由 Meta normalizer 裁决。
- 格式私有字段进入 `format_info.<format>`。
- 跨格式能力进入 `capabilities.<capability>`。
- 第三方私有扩展必须进入合规命名空间。
- 同一事实只保留一个规范写入点。

### 5. 补充文档和验证

已经定稿的内置格式补入 [ADDP 内置数据格式规范](addp内置数据格式规范.md)。尚未形成共识的事项只记录到 [ADDP 数据类型与文件格式待规范事项](../next/addp数据类型与文件格式待规范事项.md)。

最低验证项：

1. Meta 是否生成正确数量的 item。
2. `meta_item.name/full_name/item_type/node_id` 是否符合 detector 规范。
3. `attributes.item/type_info/format_info/capabilities` 是否无重复事实源。
4. multi / whole 场景是否避免重复落库。
5. Manager 是否只消费已入库 item。
6. Transfer 是否不重复推断字段类型和组织方式。

## 快速判断示例

| 新格式 | 组织方式 | 数据类型 | 需要实现 |
|---|---|---|---|
| 新单文件表格格式 | `single` | `table` | `FormatRule`、parser、`TableProvider`、attributes 映射 |
| 新多文件空间表格式 | `multi` | `table` | 组件规则、`ComponentTableProvider`、`capabilities.spatial` |
| 新目录型表格式 | `whole` | `table` | whole scope detector、scope reader、`ScopeTableProvider`、partitioning 能力 |
| 新压缩包格式 | `single` | `container` | 外层容器 item、children 枚举、必要的解包读取能力 |
| 新文档格式 | `single` | `document` | metadata / text extractor，必要时补 `DocumentProvider` |
