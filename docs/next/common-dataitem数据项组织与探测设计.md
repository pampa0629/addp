# common/dataitem 数据项组织与探测设计

更新时间：2026-05-14

本文是下一步实现前的设计稿，放在 `docs/next/` 下用于讨论和执行。正式概念仍以以下文档为准：

- [ADDP 数据项体系图](../concepts/addp数据项体系图.md)
- [ADDP 数据项探测器规范](../spec/addp数据项探测器规范.md)
- [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)
- [ADDP 资源读取抽象规范](../spec/addp资源读取抽象规范.md)

## 背景

当前容器预览和格式能力已经向统一链路推进：Excel、SQLite、GeoPackage、ZIP 等外层都可以作为 `data_type=container` 的 data item，Manager 也已经开始使用通用 container preview DTO 和 child 切换机制。

但 ZIP 内部暴露出一类更基础的问题：

1. 容器内部 entry 目前由 ZIP provider 自己枚举和识别，不能复用 Meta 对目录 / prefix 下资源的 data item 组织规则。
2. Shapefile 这类多组件 item 在普通目录下可以被 Meta 识别为一个 `organization=multi` item，但放入 ZIP 后会被拆成 `.shp`、`.shx`、`.dbf` 等多个 child，进而导致 Manager 把单个组件当成 table 读取。
3. 普通 ZIP 中的 `__MACOSX/`、`.DS_Store` 等系统噪声会进入 children。
4. ZIP 内的 PDF、Markdown、图片、嵌套 ZIP 等 child 已能被识别出格式线索，但前端和后端 child preview 分发还需要沿用 data type / format capability，而不是写 ZIP 专用 handler。

这说明问题不属于 ZIP 或 Shapefile 单点，而属于：

**给定一个候选对象集合，如何按平台规则组织成 0..N 个可识别和可处理的数据项。**

这个能力应该同时服务：

- Meta 扫描目录、对象存储 prefix、数据库 schema 等外部范围。
- 容器内部探测，例如 ZIP / TAR / RAR / Excel / SQLite / GeoPackage 等 container children。
- 后续嵌套容器，例如 ZIP 里还有 ZIP，容器里还有多组件数据项。

## 目标

1. 新增 `common/dataitem`，承载跨 Meta 与 Manager 动态容器预览复用的“候选集合 -> 数据项组织结果”能力。
2. Meta 扫描和容器内部探测尽早使用同一套组织规则，避免多处重复识别 multi / whole / single。
3. `common/dataitem` 不硬编码具体 format 名、文件后缀或格式规则，只通过 `common/format` 查询 descriptor、capability、identification 和 component specs。
4. 目录下和容器内使用同一套输入输出模型，只在 candidate 的定位方式上有所不同。
5. Manager 不重新识别 item，只消费 Meta attributes 或容器 child 解析结果，并按 data type / provider / reader 能力预览。
6. 为后续新会话执行留下足够明确的迁移步骤、接口草案和验证用例。

## 非目标

`common/dataitem` 不负责以下事情：

| 不负责 | 归属 |
|---|---|
| 扫描调度、递归遍历、任务状态 | Meta 或具体 engine 扫描入口 |
| `meta_item` 落库、fingerprint、node 绑定 | Meta |
| attributes schema 的最终构造和 normalizer | Meta |
| 构造 engine reader、管理连接凭据 | Manager / Meta / Transfer + engine capability |
| 读取 table rows、document text、media thumbnail | `common/format` content reader |
| Manager 前端 DTO、预览插件选择 | Manager |
| 决定容器内部 child 是否升格为独立 `meta_item` | Meta 规范和扫描策略 |
| 持久化容器内部 child 的完整识别结果 | Manager 动态预览用完即弃 |

## 设计原则

1. 数据项边界先于内容解析。
2. 同一类组织规则只写一次。
3. format 事实来自 `common/format`，data item 组织执行在 `common/dataitem`。
4. container 是 data type，不是 organization。ZIP、SQLite、Excel 外层通常仍是 `organization=single`。
5. multi item 只认领明确组件，不独占目录、prefix 或容器。
6. whole item 才可能独占一个范围，并且必须由 format capability 明确声明。
7. 父容器不存储 child 的完整内容。父级只保存轻量 children 索引，child 的字段、样本、正文、缩略图按选中 child 后再读取。
8. 嵌套容器不是特殊分支，而是同一套“选中 child 后按 data type / format capability 继续解析”的递归应用。

## 现有规范需要调整的点

当前 [ADDP 数据项探测器规范](../spec/addp数据项探测器规范.md) 里有一句重要约束：

> `ResolveItems` 是 Meta 扫描流程内的统一入口，而不是面向所有业务模块的 common API。

这个约束在“Manager、Transfer、Asset、Search 不得绕过 Meta 重新识别已入库 item”这一层仍然正确。但现在容器内部探测暴露出新的复用场景：Manager 在动态预览容器 children 时也需要对内部候选 entry 做 data item 组织，否则 multi 识别会在 Meta 与容器内部重复实现。

因此建议后续规范化时调整为：

```text
Meta 仍拥有扫描调度、最终裁决、claims 合并、attributes normalizer 和落库。
common/dataitem 承载可复用的候选集合组织规则执行。
Manager、Transfer、Asset、Search 仍不得对已入库外部 item 重新做目录级识别。
Manager 可以在容器预览过程中调用 common/dataitem 组织内部 child，结果用完即弃，不自动升格为 meta_item，也不写回父容器 attributes。
```

## 命名

本设计避免使用 `resource` 作为核心命名，因为这个词已用于读取抽象，也容易泛化。本问题的本质是“候选对象如何组织成 data item”。

建议使用以下命名：

| 名称 | 含义 |
|---|---|
| `Candidate` | 一个待参与 data item 组织的候选对象，可以来自目录、prefix、schema 或容器内部 entry |
| `ResolveInput` | 一次组织解析的输入，包括 scope、candidates 和选项 |
| `ResolvedItem` | 已解析出的逻辑数据项结果，不等同于已落库 `meta_item` |
| `ResolveResult` | 一次解析的完整结果，包括 items、claims、exclusive、ignored |
| `Detector` | 可选扩展点，基于 format rule 或 engine-native rule 识别 item |
| `IgnorePolicy` | 系统噪声过滤策略，例如 macOS 隐藏文件、压缩包目录项等 |
| `ScopeKind` | 候选集合来源范围，例如 directory、object_prefix、container、schema |

`ResolvedItem` 与现有 Meta 的 `DetectedItem` 关系：

- `ResolvedItem` 更适合 `common/dataitem`，强调它是组织解析结果，还未落库。
- Meta 可以把 `ResolvedItem` 转为内部 `DetectedItem` 或直接逐步迁移命名。
- 文档和代码迁移期间可保留 adapter，但不保留双套规则。

## 输入模型

接口草案：

```go
package dataitem

type ScopeKind string

const (
    ScopeKindDirectory     ScopeKind = "directory"
    ScopeKindObjectPrefix  ScopeKind = "object_prefix"
    ScopeKindContainer     ScopeKind = "container"
    ScopeKindSchema        ScopeKind = "schema"
)

type Candidate struct {
    Path        string
    Name        string
    BaseName    string
    Extension   string
    ContentType string
    SizeBytes   *int64
    IsDirectory bool
    Properties  map[string]interface{}
}

type ResolveInput struct {
    ScopeKind  ScopeKind
    ScopePath  string
    Candidates []Candidate
    Options    ResolveOptions
}

type ResolveOptions struct {
    MaxItems        int
    IncludeIgnored  bool
    AllowWholeScope bool
    IgnorePolicy    IgnorePolicy
}
```

说明：

- `Path` 是在当前 scope 内可唯一定位 candidate 的路径。目录扫描中可以是完整路径，容器内部可以是 entry path。
- `Name` 是展示和身份生成可用的末段名称。
- `Extension` 可以由调用方预填，也可以由 `common/dataitem` 基于 `Name` 标准化得到。它只是候选事实，不是规则来源。
- `ContentType` 来自 engine 或容器 entry metadata，参与 format detection。
- `Properties` 只保存来源侧补充事实，例如 ZIP entry 压缩方法、对象存储 ETag、数据库原生类型等，不写 format 私有解析结果。

## 输出模型

接口草案：

```go
type Organization string

const (
    OrganizationSingle Organization = "single"
    OrganizationMulti  Organization = "multi"
    OrganizationWhole  Organization = "whole"
)

type ResolvedItem struct {
    Name         string
    FullName     string
    ItemType     string
    Organization Organization
    DataType     string
    Format       string

    EntryPath      string
    ComponentPaths map[string]string
    ComponentList  []ComponentRef

    SizeBytes *int64
    Children  []ResolvedItem

    DetectionReason string
    Properties      map[string]interface{}
}

type ComponentRef struct {
    Role     string
    Path     string
    Required bool
}

type ResolveResult struct {
    Items     []ResolvedItem
    Claims    []string
    Ignored   []Candidate
    Exclusive bool
}
```

说明：

- `FullName` 对目录 / prefix 外部扫描可直接映射到 `meta_item.full_name`；对容器内部 child，它是容器内逻辑路径，不直接落外部 `meta_item.full_name`。
- `EntryPath` 是 item 的主入口。single item 是自身路径，multi item 是主组件路径，whole item 是 scope 根路径。
- `ComponentPaths` / `ComponentList` 只表达 multi 或必要 whole 组件，不用于把 child 内容塞到父容器。
- `Children` 仅用于调用方明确要求递归组织时返回嵌套结果。默认不深挖，避免 Meta 扫描成本失控。

## 规则来源

`common/dataitem` 不写具体格式规则。它只向 `common/format` 查询以下事实：

| 事实 | 来源 |
|---|---|
| format ID、默认 data type | `format.FormatDescriptor` |
| 支持的 organization/layout | `FormatDescriptor.Layouts` / `FormatCapability.Layouts` |
| 扩展名、MIME、magic bytes、识别优先级 | `FormatDescriptor.Identification` / format detection API |
| 多组件角色、必需性、主组件 | `format.ComponentSpecProvider` 或后续组件规格 registry |
| provider / reader 能力 | `FormatDescriptor.Providers`、`ContentReaders`、`ProviderHints` |
| container 能力 | `ContainerInfoProvider`、container reader / resolver capability |

禁止在 `common/dataitem` 中出现以下逻辑：

```go
if ext == ".shp" || ext == ".dbf" { ... }
if format == "shapefile" { ... }
if name == "xl/workbook.xml" { ... }
```

允许出现的是：

```go
descriptors := format.ListFormatDescriptors()
for _, descriptor := range descriptors {
    if supportsLayout(descriptor, "multi") {
        specs := format.GetComponentSpecs(descriptor.Format)
        // 按 specs 执行组件匹配
    }
}
```

如果当前 `common/format` 还没有统一的 component spec registry，可以先用已有 `ComponentSpecProvider` 查询；后续再把组件规格提升为 descriptor / capability 的一部分。

## IgnorePolicy

系统噪声过滤不属于 format 识别规则，但属于数据项组织入口的通用前置策略。

默认策略建议过滤：

- 空名称。
- 容器目录项。
- macOS 资源叉和索引文件，例如 `__MACOSX/`、`.DS_Store`。
- 明确标记为 deleted / hidden / temporary 的候选对象，如果 engine metadata 能提供这些事实。

过滤策略要独立于 format rule，避免把“压缩包系统噪声”写入 ZIP provider 或某个格式 detector。

接口草案：

```go
type IgnorePolicy interface {
    Ignore(candidate Candidate) (bool, string)
}
```

默认策略可以在 `common/dataitem` 内提供，但调用方可以替换或关闭。

## Resolve 顺序

建议统一执行顺序：

1. 标准化 candidates：补齐 name、extension、content type、路径排序。
2. 应用 `IgnorePolicy`，得到有效候选集合。
3. 识别 single container resource：先把外层容器本身确定为 item，内部 child 不自动升格。
4. 识别 multi components：遍历 `common/format` 中声明 `multi` layout 的格式，按 component specs 归并候选。
5. 识别 whole scope：仅当调用方允许 `AllowWholeScope` 且 format 声明 whole layout 时尝试。
6. 识别 residual single：未被 claims 认领的候选按 format detection 结果生成 single item。
7. 输出 claims、ignored、items，并按稳定顺序排序。

这里的“single container resource”只适用于外层扫描范围中 candidate 自身是 container 文件或原生 container 对象。容器内部的 child 是否还是 container，由 residual single 识别出来，再由 Manager 或容器 provider 在选中 child 时继续读取。

## Meta 使用方式

Meta 负责把 engine 扫描结果转成 `Candidate`，调用 `common/dataitem.ResolveItems`，再把 `ResolvedItem` 转成可落库的 item。

目录 / prefix 扫描链路：

```text
engine list children
  -> []dataitem.Candidate
  -> dataitem.ResolveItems
  -> Meta claims / recursive scheduling
  -> Meta attributes normalizer
  -> meta_item
```

Meta 仍负责：

- 扫描深度和任务调度。
- node 与 item 的关系。
- `meta_item.name/full_name/item_type/fingerprint`。
- `attributes.item.organization/data_type/format/component_files`。
- `type_info`、`format_info`、`capabilities` 的 normalizer。
- deep scan 时是否进一步读取内容。

`common/dataitem` 只提供组织解析结果，不直接写 attributes。

### Meta 扫描到哪个地步

对外只保留两层扫描深度：`basic` 和 `deep`。不要新增 `enrich scan` 层，避免用户理解和触发配置复杂化。

| 层级 | 行为 |
|---|---|
| basic scan | 枚举当前 scope 的候选对象，解析 item 边界，写入轻量 attributes；可以调用低成本 info provider，例如 table schema、document/media 基础信息、container 轻量 children 索引 |
| deep scan | 在 basic 基础上做成本较高的增强，例如 content index、正文抽取、缩略图、统计、空间范围、数据库字段深扫和全文索引 |

内部实现里可以保留 `enrichXXX` 这类函数名，但它只是 basic 或 deep 阶段中的实现动作，不成为新的扫描深度。

容器外层在 basic 阶段可以写入轻量 children 索引，但默认不递归解析所有 child 的完整组织结果。深层容器、child 字段、样本行和正文应在 Manager 选中 child 后动态读取；如果未来需要对容器内部 child 建立独立治理对象，再通过规范明确升格为独立 `meta_item`。

## 容器 child 动态解析方式

容器内部 child 的再识别放在 Manager 动态执行，结果用完即弃，不写回 Meta。

ZIP / TAR / RAR 这类通用压缩容器：

```text
Manager open container
  -> list entries as []Candidate
  -> dataitem.ResolveItems(ScopeKindContainer)
  -> map ResolvedItem to preview children
  -> preview active child
```

Excel / SQLite / GeoPackage 这类原生可查询容器：

- 外层仍是 `organization=single`、`data_type=container`。
- Meta basic scan 可以保存 sheet、table、layer 的轻量 children 索引。
- Manager 打开容器时可以实时刷新 children，并按当前选中 child 读取内容。
- 如果 child 是普通文件式 entry，Manager 使用 `common/dataitem` 组织。
- 如果 child 是 engine-native table / sheet / layer，provider 可以直接生成 child info，但仍使用同一套 child DTO 字段。

### 嵌套容器

ZIP 中还有 ZIP 时，不需要额外写一套递归规则：

1. Manager 枚举外层 ZIP entries，并把内层 ZIP entry 识别为一个 `data_type=container`、`format=<detected>` 的临时 child。
2. Manager 选中该 child 后，通过 `ContainerChildResolver` 获得 child reader。
3. 根据 child 的 format 找到对应 container provider，再枚举其 children。
4. 第二层 children 再走同一套 `common/dataitem.ResolveItems`。

递归深度、children 数量和读取成本由调用方 options 控制。

## Manager 读取影响

Manager 不应自己枚举 sibling 或 container entry 猜测组件。它只消费：

- 已入库 item 的 `attributes.item.component_files`。
- 容器 child 的 `component_paths` / `components`。
- `data_type`、`format`、provider / reader capability。

需要补齐的通用读取形态：

| child 形态 | Reader |
|---|---|
| 单 entry child | `io.Reader` 或 `resource.ResourceReader` 派生的 child stream |
| multi component child | `resource.ComponentReader` |
| whole scope child | `resource.ResourceReader + scope ref` |
| native query child | format provider 的 native options，例如 sheet/table/layer name |

读取分发建议：

```text
active child
  -> data_type=table
      -> ComponentTableProvider / ScopeTableProvider / TableSampleReader
  -> data_type=document
      -> DocumentInfoProvider / DocumentTextReader / raw-range fallback
  -> data_type=media
      -> MediaInfoProvider / thumbnail or raw-range fallback
  -> data_type=container
      -> ContainerInfoProvider + ContainerChildResolver
```

这意味着 Manager 可以支持 `shapefile.zip` 中的多组件 child，也可以支持普通 ZIP 中的 PDF、Markdown、图片和嵌套 ZIP，而不写 ZIP 专用 preview handler。

## multi item 组件查看

multi item 的默认展示对象是“组合后的逻辑数据项”，不是组成它的单个文件。无论这个 multi item 来自 Meta 扫描目录 / prefix，还是 Manager 动态解析 container child，都应该具备同一套组件查看能力。

统一语义：

| 层级 | 含义 | 是否默认展示 |
|---|---|---|
| data item / child item | 可被平台识别和处理的逻辑数据项，例如一个 Shapefile 表 | 是 |
| component | 组成 multi item 的原始资源引用，例如同名的一组组件文件 | 否，作为 item 详情展开 |
| raw entry | 容器内真实存在的条目，例如 ZIP entry | 否，除非未被忽略且未被组合为 component |

原则：

1. Meta 中的 `organization=multi` item 应保存 components 关系，并允许用户查看任一 component 的原始预览。
2. Manager 动态解析出的 container multi child 也使用同一结构表达 components，并允许查看任一 component。
3. container child 如果自身还是 container，选中后递归进入同一套 child item / component 查看规则。
4. 上层不硬编码具体格式的组件语义。例如 `.shx` 是索引、`.prj` 是投影，这些标签只应由 Shapefile format provider 提供；Meta、Manager 和前端只渲染 format 返回的通用 component descriptor。
5. component preview 不改变默认 item 预览。用户点击 component 时，Manager 根据 component 自身的 data type / format 走通用对象预览分发；无法识别时至少提供文本、二进制信息或下载。

建议在 `common/format` 补充组件描述接口：

```go
type ComponentDescriptor struct {
    Key      string
    Path     string
    Role     string
    Label    string
    Required bool
    Primary  bool
    DataType string
    Format   string
}

type ComponentDescriptorProvider interface {
    Provider
    DescribeComponents(components []resource.ComponentRef) []ComponentDescriptor
}
```

说明：

- `ComponentSpecProvider.ComponentSpecs()` 负责“如何识别和组合”。
- `ComponentDescriptorProvider.DescribeComponents()` 负责“如何向用户解释组件”。
- 没有实现 descriptor provider 的格式，平台使用通用文件名、扩展名和 `required/primary` 展示，不猜测具体业务含义。

Manager 对外 DTO 中建议把组件结构保持为通用字段：

```json
{
  "name": "示例数据.shp",
  "organization": "multi",
  "data_type": "table",
  "format": "shapefile",
  "components": [
    {
      "key": "main",
      "path": "示例数据.shp",
      "label": "主文件",
      "required": true,
      "primary": true,
      "data_type": "file",
      "format": "shapefile"
    }
  ]
}
```

这里的 `label` 来自 format provider，而不是 Manager 内置。组件点击预览时可以使用：

```text
child_key=<logic-child-key>
component_key=<component-key>
```

或：

```text
child_key=<logic-child-key>
component_path=<component-path>
```

接口最终命名需要结合现有 `child_name` 参数收敛，但语义上必须区分“逻辑 child 预览”和“component 原始预览”。

## container 计数与解释

容器预览需要同时表达“真实条目数”和“可预览逻辑子项数”，否则用户会看到 `child_count=6`、下拉只有 3 个子项而无法理解原因。

建议 summary 至少包含：

| 字段 | 含义 |
|---|---|
| `raw_child_count` | 容器原始 entry 数量，来自 container provider |
| `visible_child_count` | 展示给用户的逻辑 child item 数量 |
| `ignored_child_count` | 被 IgnorePolicy 过滤的系统噪声数量 |
| `grouped_component_count` | 被 multi item 组合吸收的 component 数量 |
| `grouped_item_count` | 产生的 multi item 数量 |

前端文案建议避免“已加载子项”这种含糊表达，改为：

```text
原始条目 6
可预览子项 3
已过滤 2
已组合 1 组
```

如果后续需要解释详情，可在 child selector 附近增加“查看组织明细”，列出 ignored 和 grouped 的原因；第一阶段只显示 summary 即可。

## 和 common/format 的接口缺口

实现前需要检查并补齐以下小接口：

1. 是否已有统一方法从 format ID 获取 component specs。若没有，可先从已注册 provider 中查询 `ComponentSpecProvider`。
2. `FormatDescriptor.Identification` 是否足够表达 extension / MIME / priority。若 detection 仍散在函数里，需要封装统一检测入口。
3. `ContainerChildInfo` 是否能表达 `organization=multi`、`component_paths`、`component_list`。
4. `ContainerChildResolver` 是否能返回 component child reader。
5. `ParseOptions` 是否能承载 child path、sheet/table/layer name、container depth 等通用上下文，而不是用格式专用参数散落。

## 迁移计划

### 第一步：只加设计文档

当前步骤只新增本文档，不改代码。

### 第二步：新增 common/dataitem 包

建议文件：

```text
common/dataitem/types.go
common/dataitem/resolve.go
common/dataitem/ignore.go
common/dataitem/format_rules.go
common/dataitem/resolve_test.go
```

先实现：

- `Candidate` / `ResolveInput` / `ResolvedItem` / `ResolveResult`。
- 默认 `IgnorePolicy`。
- format detection adapter。
- single / multi / residual single 组织。
- 基于 `ComponentSpecProvider` 的 multi 组件匹配。

whole scope 可以先保留接口和测试占位，不抢在本轮实现复杂场景。

### 第三步：Meta 迁移

把 `meta/backend/internal/dataitem` 或 `metaitem` 中已有的通用 single / multi 识别逻辑迁移到 `common/dataitem`，Meta 侧保留薄 adapter：

```text
Meta DetectedItem
  <- dataitem.ResolvedItem
```

迁移时删除重复规则，不保留兼容旧 detector 分支。

### 第四步：Manager 容器 child 动态接入

ZIP provider 保留轻量 `ContainerInfoProvider` 能力用于 Meta basic scan 的父容器摘要，但不承担完整 child item 组织。Manager 容器 child 预览改为：

1. 枚举 ZIP entries。
2. 转成 `dataitem.Candidate`。
3. 调用 `dataitem.ResolveItems(ScopeKindContainer)`。
4. 映射为预览侧临时 children。
5. 选中 child 后按 data type / format reader 分发预览。

完成后 `shapefile.zip` 应只显示一个 multi child，而不是多个组件 child。

### 第五步：Manager component child 支持

补齐：

- container child DTO 表达 multi components。
- child resolver 返回 component reader。
- table preview 优先走 `ComponentTableProvider`。
- document / media / nested container child 使用同一套 data type 分发。

### 第六步：普通压缩容器扩展

ZIP 稳定后，再考虑 TAR / RAR / 7z。它们应只新增 container entry 枚举和 child stream resolver，不复制 item 组织规则。

## 验证用例

### 目录级

```text
/sample/
  farmland.shp
  farmland.shx
  farmland.dbf
  farmland.prj
  readme.pdf
  table.csv
```

期望：

- `farmland.*` 解析为一个 `organization=multi` item。
- PDF、CSV 分别解析为 single item。
- Shapefile 组件不会重复落为独立 item。

### 对象存储 prefix

同目录级用例，验证 NFS 与 MinIO 输出一致。

### shapefile.zip

```text
shapefile.zip
  farmland.shp
  farmland.shx
  farmland.dbf
  farmland.prj
  farmland.cpg
```

期望：

- ZIP 外层是 single container item。
- children 中只有一个 `organization=multi` 的 Shapefile child。
- child 预览走 component reader，不再报“需要 component input”。

### 普通 test.zip

```text
test.zip
  __MACOSX/...
  .DS_Store
  readme.md
  manual.pdf
  image.jpg
  data.csv
  nested.zip
```

期望：

- 系统噪声被 ignored。
- Markdown / PDF 是 document child。
- JPG 是 media child。
- CSV 是 table child。
- nested.zip 是 container child，选中后可继续展开。

### Excel / SQLite / GeoPackage

期望：

- 外层都是 single container item。
- 父容器只存轻量 children。
- SQLite / GeoPackage 父级不写 table info。
- child 的 table info / rows / spatial info 只在选中 child 后读取。

## 新会话执行提示

如果后续开新会话继续实现，请按以下顺序接手：

1. 先读本文档。
2. 再读：
   - `docs/concepts/addp数据项体系图.md`
   - `docs/spec/addp数据项探测器规范.md`
   - `docs/spec/addp数据类型与格式能力规范.md`
   - `common/format/provider.go`
   - `meta/backend/internal/dataitem/`
   - `meta/backend/internal/metaitem/`
   - `common/format/plugins/zip/`
   - `manager/backend/internal/service/preview_provider_container_child.go`
3. 新增 `common/dataitem`，不要先改 Manager 前端。
4. 把 Meta 现有 detector 中可通用的规则迁到 `common/dataitem`。
5. 删除重复规则，不做旧链路兼容。
6. Manager 容器 child 动态接入后先跑后端单测，再补 component child。
7. 最后用真实样例核实前端体验。

## 待确认问题

1. common 层最终命名使用 `ResolvedItem`；Meta 可保留 `DetectedItem` adapter，后续逐步迁移。
2. `ComponentSpec` 建议补充明确的 `Primary` 字段，避免用 `Role=="main"` 约定主组件。
3. IgnorePolicy 第一阶段只提供默认策略，不做租户级配置；如有业务需要再扩展。
4. whole scope 本轮只保留接口，不同步迁移 Parquet whole scope 逻辑。
5. 容器内 child 暂不生成持久 fingerprint；Manager 动态预览可用 `parent_item_id + child_path + child_format` 作为临时 key。
