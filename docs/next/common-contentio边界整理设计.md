# common/contentio 边界整理设计

更新时间：2026-05-18

本文记录 `common/contentio` 边界整理后的稳定结论和迁移摘要。正式规范以 `docs/spec/addp内容IO抽象规范.md` 为准。

## 一、稳定结论

`common/contentio` 是 ADDP 基于 Go `io` 之上的平台内容 I/O 抽象层。

它只负责在 engine 和 format 之间表达底层内容访问边界：

```text
engine plugin
  -> contentadapter
  -> contentio.Ref + Reader / Writer / Lister / RangeReader
  -> format provider
```

`contentio` 不负责：

- data item 边界识别。
- format 相关 refs 推导规则。
- Manager / Frontend DTO。
- engine registry、连接信息、权限、连接池。
- 本地临时目录物化、按扩展名选择、格式探测等编排工具。

## 二、保留概念

### Ref

`Ref` 是一个已确定 content 的定位器。

它不是 data item，不是 catalog node，也不是 ADDP Meta。它只表达“要打开哪个 content”。

当前字段：

- `Path`
- `Role`

其中 `Role` 只表达当前格式或调用链里这个 content 的角色，例如 `main`、`index`、`attributes`。它不能上升为 item 组织模型。

`NewRef` 是带默认规范化的构造器：去除路径首尾 `/`，并将 `Role` 规范为小写。它不保存显示名，也不根据 `Role` 推导 `Primary`；显示名可按需从 `Path` 派生。

`Required` / `Primary` 已从 `contentio.Ref` 迁出。它们本质上描述的是“某个 ref 在一个 ref 集合中的约束和主次关系”，不是单 content 的定位事实。目前由以下上层结构承载：

- `common/dataitem.ItemRef`：Meta detector 识别出的 item refs。
- `meta` attributes：写入 `attributes.item.refs`。
- `common/format.RelatedRef`：format provider 的 multi ref 入参。
- `manager`：从 attributes 构造 `RelatedRef`，展示相关内容、选择 ref 预览。
- `transfer`：从 format 规格推导目标 `RelatedRef`。
- `common/format`：描述 refs、物化 multi refs、写出 optional sidecar。

`format.RelatedRef` 是当前共享的“已解析 ref + 集合标注”结构：

```go
type RelatedRef struct {
    Ref      contentio.Ref
    Required bool
    Primary  bool
}
```

### I/O 接口

当前接口边界：

```go
type Reader interface {
    Open(ctx context.Context, ref Ref) (io.ReadCloser, error)
    Stat(ctx context.Context, ref Ref) (*Stat, error)
}

type Lister interface {
    List(ctx context.Context, scope Ref) ([]Ref, error)
}

type Writer interface {
    Create(ctx context.Context, ref Ref) (io.WriteCloser, error)
}

type RangeReader interface {
    Reader
    OpenRange(ctx context.Context, ref Ref, offset, length int64) (io.ReadCloser, error)
}
```

`Lister` 独立于 `Reader`，只有 scope / 目录型格式需要列举时才按需使用。

### Stat

`Stat` 采用 Go / Unix 的 `stat` 语义，表示单 content 的轻量 I/O 状态。

它只包含：

- 是否存在。
- 大小。
- MIME / content type。
- 修改时间。

它不包含：

- format。
- data type。
- field schema。
- spatial info。
- ADDP Meta attributes。
- engine-native field type。
- scope 子项计数。

## 三、迁出概念

以下概念已从 `contentio` 迁出或删除：

| 概念 | 处理 | 归属 |
|---|---|---|
| multi reader / writer 组合对象 | 删除 | 多 content 通过 `Reader/Writer + []format.RelatedRef` 显式传递 |
| 静态 multi 包装器 | 删除 | 调用编排层直接持有 `[]format.RelatedRef` |
| `OpenRole` / `Refs` / `Commit` / `Abort` | 删除 | format writer 会话或 engine / 模块编排层 |
| `RelatedRefSpec` | 迁出 | `common/format` |
| `SameBasenameRefs` | 删除 | 使用 `common/format.SameBasenameRelatedRefs` |
| `NormalizeExtension` | 迁出 | `common/format` |
| `FirstByExtension` | 删除 | 如需恢复，应放到具体编排层或纯 `[]Ref` 工具 |
| scope 物化到本地目录 | 删除 | 如需恢复，应放到 format / manager / transfer 编排层 |
| `Required` / `Primary` | 已迁出 | dataitem / format / 调用编排层的 ref 集合结构 |

## 四、分层判断

多 content 的职责分层如下：

- `contentio`：按 `Ref` 打开、创建、range 读取、按 scope 列举 content。
- `format`：声明某个格式有哪些相关 refs，以及如何读写这些 refs。
- `dataitem` / Meta detector：决定哪些 content 最终组成一个 data item。
- Manager / Transfer：根据已确认 refs 构造 reader / writer 并调用 format provider。

`format.Layouts` 是格式可支持的 content layout 声明；`dataitem.Layout` 复用同一组值，表示某个已识别 item 的最终 layout。字段名保留 `layout` 是因为它写入 `attributes.item.layout`，但它不再是一套独立取值体系。

关键原则不是“contentio 只能处理单 content”，而是：

`contentio` 可以按多个 `Ref` 打开多个 content，但不定义这些 content 如何组成 item 或 format 语义。

## 五、当前代码职责定位

当前代码中，“相关内容从哪里来、如何映射到 engine、由谁消费”的职责如下：

| 职责 | 当前归属 | 代码定位 |
|---|---|---|
| 声明某格式需要哪些相关内容 | `common/format` 及具体 format plugin | `common/format.RelatedRefSpec`；`common/format/plugins/shapefile.RelatedRefSpecs` |
| 扫描期识别哪些 content 组成一个 item | `common/dataitem`，由 Meta detector 调用 | `common/dataitem.ResolveItems`、`resolveMultiItems`、`matchMultiRule`；`meta/backend/internal/metaitem/commonDataItemResolver` |
| 预览期获取 multi refs | Manager preview | 优先读取 `attributes.item.refs`；缺失时由 `refsForPreview` 调用 `format.SameBasenameRelatedRefs` 兜底 |
| Transfer 读写期构造 multi refs | Transfer executor | `encodedContentTableSource.refReader`、`encodedContentTableTarget.refWriter`、`multiTargetResourceDeleter` |
| 同 basename 的纯路径推导 | `common/format` | `format.SameBasenameRelatedRefs` |
| scope / 目录列举 | `contentio.Lister` 的具体实现 | Manager preview 的 catalog reader、Meta table file reader；format provider 只按需消费 `Lister` |
| `contentio.Ref` 到 engine catalog path 的映射 | `common/engine/contentadapter` 或模块内 reader | `contentadapter.CatalogPath`、`NewReader`、`NewMappedReader`、`FixedPathMapper`、`ObjectPathMapper` |
| 真实内容读写 | engine plugin | `ContentReadableProvider.OpenContent`、`ContentWritableProvider.CreateContent` |
| format 解析/写出 | format provider | `OpenMultiTableReader`、`DescribeMultiTable`、`SampleMultiTable`、`OpenMultiTableWriter` |

边界原则：

- format provider 可以声明 `RelatedRefSpecs`，但不负责目录列举、sibling content 搜索、engine path 拼接或访问策略判断。
- `format.SameBasenameRelatedRefs` 只是纯路径便利函数，应由 Manager / Transfer / 测试等编排层显式调用。
- `RelatedRefSpec` 和已解析的 `RelatedRef` 集合都必须有且只有一个 primary；公共校验函数在 `common/format`，不下沉到 `contentio`。
- engine plugin 不依赖 `contentio`；`contentadapter` 是 `contentio.Ref` 与 engine `CatalogPath` 之间的边界。
- 已扫描入库的 item 应优先使用 Meta 中保存的 refs，避免预览阶段重新猜测 item 边界。

## 六、当前状态

代码已完成主要迁移：

- `common/contentio` 保留 `Ref`、`Reader`、`Writer`、`Lister`、`RangeReader`、`Stat`。
- `common/format` 承担相关 ref 规则，并用 `RelatedRef` 承载 format provider 的 multi ref 入参。
- `common/dataitem` / Meta 承担 item 组织识别。
- Manager / Transfer 使用 `Reader/Writer + []format.RelatedRef` 调用 format provider。

后续如需新增内容 I/O 能力，应先判断它是底层 I/O 原语，还是 format / item / 模块编排语义。只有前者可以进入 `common/contentio`。

## 七、后续检查建议

后续主要检查点不再是迁移字段，而是防止边界回流：

1. 新增内容 I/O 能力时，先判断它是否是底层 I/O 原语；不是则放到 format、dataitem 或具体模块。
2. 新增 multi 格式时，优先声明 `RelatedRefSpec`，并通过 `RelatedRef` 传递已解析 refs。
3. 不再向 `contentio.Ref` 添加显示名、必需性、primary、format、data type、schema 等上层语义字段。
