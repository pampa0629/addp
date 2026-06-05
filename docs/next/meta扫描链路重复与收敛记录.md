# Meta 扫描链路重复与收敛记录

本文记录当前 Meta 扫描链路中已经暴露出的重复、概念混用和后续收敛方向。本文仅作为 `docs/next` 下的工作记录，不直接替代正式规范。

## 已澄清概念

1. refs 与 addp locator 没有必然绑定关系。
   refs 表达的是一次扫描或识别所需的一组底层内容引用；addp locator 表达的是 ADDP 资源树中的定位事实。refs 可以由 locator 间接解析得到，也可以来自 Transfer 执行结果、engine catalog、content provider 或其它 scan source。

2. catalog 不等于对象存储。
   catalog 是引擎暴露资源结构的抽象模型。对象存储的 bucket / prefix / object 只是其中一种 catalog model；数据库 namespace / table、文件系统 directory / file、容器内部目录和子内容也都可以有自己的 catalog 或内容引用表达。

3. addp locator 的核心价值是资源树定位，不是 Meta scan 内核。
   locator 可以作为 API 层的 selector，帮助用户、Manager、Transfer 或其它模块定位资源树节点、item 或路径；但 Meta scan 的内部主链路不应依赖 locator 作为唯一 scan primitive。Meta scan 内核应在解析 selector 后进入统一的 scan scope / refs / item detection 流程。

4. 给定 refs 的扫描能力应是 Meta 的通用能力。
   Transfer 只负责生成 content，并知道本次生成了哪些 content refs；识别这些 refs 是否构成一个 item、item 的 format / layout / data type 以及后续 deep enrich，都属于 Meta。

## 当前重复与问题

### API 输入语义混杂

当前 Meta scan request 同时支持：

- `engine_id`
- `node_id`
- `item_id`
- `targets`
- `catalog_paths`

这些输入混合了几类不同语义：

- engine 范围；
- 资源树节点或 item 定位；
- locator 目标；
- 引擎 catalog path；
- 需要扫描的实际内容范围。

其中 `catalog_paths` 被当成通用扫描入口使用，但它本质上依赖具体 catalog model：数据库里可能表示 schema，文件系统里可能表示目录或文件，对象存储里可能表示 bucket / prefix / object。不同引擎下含义不同，不能作为 refs 的通用替代物。

### target 解析存在多处入口

历史上异步任务创建、即时扫描和 item refresh 存在各自的目标解析路径：

- execution 创建会把 node / item / targets 解析为 `catalog_paths` 并写入 execution config。
- `ScanService.ScanEngineWithOptions` 也会处理 scan options 并解析 targets。
- item refresh 需要从已有 item attributes 反推刷新目标。

这些路径本质上都在做“从用户选择或已有 metadata 推导扫描范围”的事情，因此后续收敛方向是统一到 scan target / scope resolution 层，避免不同入口再次出现行为不一致。

### dispatch 分支过早绑定引擎类型

当前 `dispatchScan` 根据 engine plugin 的 catalog scan strategy 分到不同实现：

- tabular scan；
- branch-leaf scan；
- object catalog scan；
- filesystem catalog scan。

这种分派本身是必要的，因为不同引擎读取 catalog / content 的方式不同。但当前一些本应通用的工作也散落在各分支里：

- 路径归一；
- fallback scope；
- refs / resources 的构造；
- item detection；
- deep enrich；
- persist / upsert；
- scan depth 与 force 策略。

这使得“给定 refs 后识别 item”这种能力难以作为通用能力接入，只能不断补到某个具体 scanner 分支里。

### item detection 与 persist 不在一条主路径

当前已有多个 item 识别或入库路径：

- 单资源历史上通过 `InferSingleResource` 和 `catalogSingleItemProcessor` 入库，当前应统一进入 `detectedItemProcessor`。
- 对象存储组合项通过 `DetectObjectCatalogCompositeItems` 再走 composite persist。
- 文件系统复合项通过 filesystem scan 内部的 resolver 处理。
- 已有 item refresh 走 `RefreshItem` 和独立 refresh 逻辑。
- `metaitem.ResolveItems` / `common/dataitem.ResolveItems` 已具备识别复合 item 的能力，但并未成为所有 scan 入口的统一中枢。

因此容易出现局部修复，例如“先生成多个 sidecar item，再合并、软删”。这类后验归并不适合作为 Transfer 生成物扫描的主路径。

### refs group 缺少一等表达

Shapefile 这类格式需要一组 refs 才能识别为一个逻辑 item，例如：

- `.shp`
- `.shx`
- `.dbf`
- `.cpg`
- `.prj`

Transfer 在执行完成后天然知道本次生成了哪些 content refs，但当前 Meta API 没有一个通用的 refs group 输入。将这些 refs 转成父目录扫描会扩大 scan scope；将它们逐个作为 path 扫描又会丢失 group 关系。

正确方向应是：Meta 支持“给定一组 refs，在这组 refs 的边界内识别 0..N 个 item”。Transfer 不识别 item，只提交 refs group。

## 后续收敛方向

### 建立统一 scan scope resolution 层

Meta API 可以继续支持多种 selector：

- engine；
- node；
- item；
- locator target；
- catalog path；
- refs group。

但这些输入应先统一解析为内部 scan scope，而不是在 API 层或 task 层过早转成某种具体引擎的 `catalog_paths`。

建议内部目标形态分层：

```go
type ScanSelector struct {
	EngineID     uint
	NodeID       uint
	ItemID       uint
	Targets      []string
	CatalogPaths []string
	RefGroups    []ScanRefGroup
}

type ScanScope struct {
	EngineID  uint
	Mode      string
	Paths     []string
	RefGroups []ScanRefGroup
}

type ScanRefGroup struct {
	Primary string
	Refs    []string
}
```

以上只是方向示意，不代表最终字段命名。关键是：API selector、内部 scan scope 和底层 content refs 要分层。

### refs group 不绑定 addp locator

`ScanRefGroup.Primary` 和 `ScanRefGroup.Refs` 不应被设计为必须是 addp locator。它们可以是经过解析后的 content ref、catalog ref 或 provider ref。addp locator 可以作为 API selector 的一种输入，但不应成为 refs 的唯一表示。

最终应由 Meta 的 scan scope resolver 根据输入来源决定：

- locator 输入：先解析资源树定位，再转成 scan scope；
- catalog path 输入：按 engine catalog model 解析；
- Transfer 生成物输入：直接表达本次生成的 refs group；
- 已有 item 输入：从 item attributes 中解析已有 scan refs 或物理内容范围。

### item detection 统一进入 dataitem / metaitem 主路径

不论输入来自目录扫描、已有 item refresh，还是 Transfer 给定 refs，最终都应进入统一识别链路：

```text
scan selector
  -> scan scope
  -> content refs / storage resources
  -> metaitem.ResolveItems / common.dataitem.ResolveItems
  -> detected item
  -> enrich
  -> persist
```

这样 Shapefile 这类 multi layout item 可以一次识别、一次入库，不需要先生成 sidecar meta item 再合并删除。

### scanner 分支只保留读取差异

对象存储、文件系统、数据库、容器等 scanner 分支仍然需要存在，但它们应主要负责：

- 如何枚举 catalog；
- 如何解析 provider ref；
- 如何打开 content；
- 如何获取 size / modified time / content type 等 storage facts。

而以下能力应逐步上移为统一能力：

- scan target resolution；
- refs group 识别；
- dataitem detection；
- item attributes 标准化；
- deep enrich；
- item persist / refresh。

## 与 Transfer 的关系

Transfer 的边界应保持简单：

1. 根据任务配置执行数据传输。
2. 记录本次实际生成的 content refs。
3. 调用 Meta scan，请求 Meta 对这些 refs 进行 deep scan。

Transfer 不应：

- 判断 refs 是否已经构成 meta item；
- 调用 `common/dataitem` 识别 item；
- 为了触发识别而扩大扫描到父目录；
- 生成多个 meta item 后再要求 Meta 合并。

Transfer 后的 Meta scan 应满足：

1. 使用 deep scan。
2. 只扫描本次生成的 refs group。
3. 不扩大到父目录，除非父目录本身就是这次生成的 scope layout。
4. 由 Meta 在 refs group 边界内识别 item。

## 待办

1. 为 Meta API 设计通用 refs group 输入，避免误用 `catalog_paths` 表达 refs。
2. 建立 scan selector 到 scan scope 的统一解析层。
3. 将已有 node / item / targets / catalog paths 的解析逻辑迁移到统一 resolver。
4. 将对象存储、文件系统中的复合 item 识别统一收敛到 refs group detection 主路径。
5. 清理“先生成 sidecar item，再合并、软删”的后验路径。
6. 调整 Transfer 后 Meta scan：Transfer 只提交本次生成的 refs，Meta 负责识别和入库。
7. 补充针对 Shapefile Transfer 输出的端到端测试：Manager 中只出现一个 Shapefile item，不出现 `.shp/.shx/.dbf/.cpg` 多个独立 item。
