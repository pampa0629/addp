# ADDP 数据类型与格式模块边界规范

本文定义数据类型、组织方式、文件格式、provider、attributes 和资源读取抽象在各模块之间的职责边界。概念边界见 [ADDP 数据类型与格式体系图](../concepts/addp数据类型与格式体系图.md)。

## 核心结论

数据类型与格式体系跨越 Meta、Manager、Transfer、common/format、common/resource 和 engine plugin。为了避免重复推断和事实源分裂，模块职责按以下顺序分层：

```text
engine capability
  -> Meta detector 确定 data item
  -> Meta normalizer 写 attributes
  -> common/resource 提供读取抽象
  -> common/format 解码 / 提取 / 编码
  -> data type provider 归一平台语义
  -> Manager / Transfer / Asset / Search 消费
```

## 职责矩阵

| 模块 / 层级 | 负责 | 不负责 |
|---|---|---|
| engine plugin | 连接、catalog、元数据、内容读写、批次读写等 engine capability | 判断最终 `data_type`、归并 multi / whole item、写最终 attributes |
| `meta` | 资源树扫描、detector 调度、data item 识别、claims / exclusive 合并、attributes normalizer、落库；内部持有 item 组织方式枚举、轻量规则结构、格式 / data_type 推断 helper | Manager preview DTO、Transfer 执行计划、format provider 内部解析细节 |
| `common/format` | 格式枚举、FormatCapability、parser / extractor / writer、TableProvider 等 provider 实现 | 构造 engine reader、决定最终 item 边界、直接写 `meta_item.attributes` |
| `common/resource` | `ResourceRef`、`ResourceReader`、`ComponentReader`、`NativeCursor` 等读取抽象 | 连接凭据管理、格式解析、preview DTO |
| `manager` | 消费已入库 meta item 和标准 attributes，构造 preview DTO | 重新判断 organization、重新猜 format、重新枚举 sibling 组件 |
| `transfer` | 基于 meta item、engine capability、resource 抽象和 provider 规划读写 | 重复推断字段类型、重复识别组件、绕过 provider 直接硬编码格式 |
| `asset` / `search` | 消费标准 attributes 做资产治理、索引和检索 | 自行识别 data item 或重写格式解析规则 |

## 事实源归属

| 事实 | 事实源 |
|---|---|
| data item 边界、主资源、组件、whole scope、claims / exclusive | [ADDP 数据项 detector 规范](addp数据项detector规范.md) |
| attributes 分区和字段归属 | [ADDP 元数据 attributes 规范](addp元数据attributes规范.md) |
| format capability 与 provider 能力 | [ADDP 文件格式能力与 Data Type Provider 规范](addp文件格式能力与DataTypeProvider规范.md) |
| 资源定位和读取抽象 | [ADDP 资源读取抽象规范](addp资源读取抽象规范.md) |
| 内置格式落地规则 | [ADDP 内置数据格式规范](addp内置数据格式规范.md) |
| 新增格式步骤 | [ADDP 数据格式扩展指南](addp数据格式扩展指南.md) |

## 调用边界

### Meta 扫描

```text
engine catalog / metadata / content read
  -> Meta detector
  -> parser / extractor 提供候选事实
  -> Meta normalizer
  -> meta_item + attributes
```

Meta 可以调用格式解析能力，但最终 item 边界和 attributes 核心字段由 Meta 裁决。

### Manager 预览

```text
meta item + attributes
  -> Manager 根据 engine capability 构造 resource reader
  -> format provider / data type provider
  -> Manager preview DTO
```

Manager 不重新识别 item。multi 读取使用 `item.component_files`；whole 读取使用已入库 whole scope。

### Transfer

```text
source / target meta item
  -> TransferPlan(engine, resource, data_type, format, capabilities, policy)
  -> resource reader / writer
  -> format provider
  -> data type provider
  -> pipeline reader / writer
```

Transfer 可以根据目标能力选择编码格式，但不能绕过标准 schema、字段类型和空间能力事实源。

## 设计约束

1. 同一事实只在一个模块裁决，并写入一个规范位置。
2. `manager`、`transfer`、`asset`、`search` 只消费已入库 meta item，不复刻 Meta detector。
3. `common/format` 可以声明 layout 能力，但不直接决定最终 `organization` 和 claims。
4. `common/resource` 不进入格式语义，也不承载 preview DTO。
5. format provider 不接 `engine_id`，不反向构造 engine reader。
6. 私有格式字段必须进入命名空间，不能覆盖平台标准字段。
