# common/contentio 边界整理设计

更新时间：2026-05-18

本文记录 `common/contentio` 边界整理过程。当前代码、format provider、engine adapter、Manager / Meta / Transfer 主要调用链已完成收敛；正式结论已同步到 `docs/spec/addp内容IO抽象规范.md`，本文保留为迁移设计记录。

## 一、核心问题

`common/contentio` 的根本价值不是定义数据项、格式组织方式或元数据模型，而是在 engine 和 format 之间提供一个足够小的内容 I/O 边界：

```text
engine plugin
  -> CatalogPath / ConnectionInfo / OpenContent / CreateContent / OpenRange
  -> contentadapter
  -> contentio.Ref + Reader / Writer / RangeReader
  -> format provider
```

换句话说，`contentio` 只解决“给定一个 content 定位器，如何打开或创建内容流”的问题。

它让 format 层不需要知道：

- engine id
- engine registry
- 连接信息和凭据
- engine-specific CatalogPath
- 权限校验和连接池
- NFS / S3 / MinIO / 本地文件系统等具体实现

## 二、应该保留的底层概念

### Ref

`Ref` 是一个 content 的定位器。

它不是 data item，不是 catalog node，也不是 ADDP Meta。它只表达“要打开哪个 content”，可以携带 role 作为辅助信息。

当前字段方向可以保留：

- `Path`
- `Name`
- `Role`
- `Required`
- `Primary`

其中 `Role` 只表达当前格式或调用链里这个 content 的角色，例如 `main`、`index`、`attributes`。它不能上升为 item 组织模型。

### Reader / Writer / RangeReader

`Reader` 和 `Writer` 是 contentio 的核心：

```go
type Reader interface {
    Open(ctx context.Context, ref Ref) (io.ReadCloser, error)
    Stat(ctx context.Context, ref Ref) (*Stat, error)
}

type Writer interface {
    Create(ctx context.Context, ref Ref) (io.WriteCloser, error)
}

type RangeReader interface {
    Reader
    OpenRange(ctx context.Context, ref Ref, offset, length int64) (io.ReadCloser, error)
}
```

当前实现中 `RangeReader` 嵌入 `Reader`，表示同一个内容读取器在完整读取之外额外支持范围读取。

### Stat

`Stat` 采用 Go / Unix 的 `stat` 语义：查询 content 的轻量状态。

它不是 statistics，也不是 static，更不是 ADDP Meta。它只应包含 I/O 层事实：

- 是否存在
- 大小
- MIME / content type
- 修改时间
- 子项计数等轻量状态

不应包含：

- format
- data type
- field schema
- spatial info
- ADDP Meta attributes
- engine-native field type

## 三、当前不应继续放在 contentio 的概念

### MultiReader / MultiWriter

`multi` 更像 data item / format 层的组织方式，不是 contentio 的底层 I/O 原语。

Shapefile 需要 `.shp/.shx/.dbf/.prj/.cpg`，这是 Shapefile format 对多个 content 的组织规则。Parquet directory、Zip child、container child 也都是上层组织语义。

contentio 不应固化“多个 content 是一个 multi item”这个概念。更自然的 Go 表达是：

```go
reader contentio.Reader
refs   []contentio.Ref
```

format provider 如果需要多个 refs，可以在 `common/format` 层定义调用参数，或者直接接收 `contentio.Reader` / `contentio.Writer` 加 `[]contentio.Ref`。

### StaticMultiReader / StaticMultiWriter

迁移前 `StaticMultiReader` / `StaticMultiWriter` 的实际含义是：

- 底层只有 `Reader.Open(ref)` / `Writer.Create(ref)`。
- 调用方已经知道一组 refs。
- 包装器把 reader/writer 和 refs 绑在一起，额外提供 `Refs()`、`OpenRole()`、`Commit()`、`Abort()`。

这里的 `Static` 是“refs 静态给定”的意思，不是静态文件，也不是静态类型。这个名字表达实现细节，不表达架构职责。

从边界上看，它不应作为 `contentio` 概念保留。当前代码已删除这组临时实现，改由 format 层或调用编排层显式传递 `Reader/Writer + []Ref`。

### FirstByExtension / MaterializeScope

这类函数不是底层 I/O 原语，而是基于一组 refs 或 scope 的选择 / 物化工具。

它们是否保留在 `contentio` 需要单独判断：

当前已从 `contentio` 删除。后续如果确实需要类似能力，应按使用场景放到 format / manager / transfer 编排层，或沉淀为只处理 `[]Ref` 的纯函数工具，不能重新把格式猜测、本地临时目录和 scope 物化塞回底层 I/O 包。

## 四、推荐边界

`common/contentio` 只保留：

- `Ref`
- `NewRef`
- `NormalizeExtension`
- `SameBasenameRefs`
- `Reader`
- `Writer`
- `RangeReader`
- `Stat`
- 基础错误，例如 `ErrContentNotFound`

`common/contentio` 不保留：

- MultiReader / MultiWriter 作为核心接口
- StaticMultiReader / StaticMultiWriter 作为长期概念
- format / data type hint
- ADDP Meta 相关命名
- engine registry / CatalogPath / ConnectionInfo
- Manager / Frontend DTO
- item / container / dataset 组织模型

## 五、对 format 和 engine 的作用

### 对 engine

engine plugin 继续负责真实存储访问：

- `CatalogProvider`
- `ContentReadableProvider`
- `ContentWritableProvider`
- `RangeReadableProvider`
- `ResourceDeleteProvider`

`common/engine/contentadapter` 负责把 engine 能力适配为 contentio：

```text
CatalogPath + ConnectionInfo + engine provider
  -> contentio.Reader / Writer / RangeReader
```

### 对 format

format provider 只消费 contentio，不反向依赖 engine：

```text
contentio.Reader + Ref
  -> decode / describe / sample / read table

contentio.Writer + Ref
  -> encode / write table
```

如果某个 format 需要多个 content，format 层应显式接收：

```go
reader contentio.Reader
refs   []contentio.Ref
```

或者在 `common/format` 内定义一个非常薄的调用参数结构。命名要谨慎，避免为了包装而制造新术语。

## 六、迁移方向

1. 先保持现有功能可用，不在 `docs/spec/` 宣布未完成规范。
2. 已在 `common/format` 梳理 multi-table provider 的输入，把 `contentio.MultiReader/MultiWriter` 从 provider 接口中迁出。
3. 已将 Transfer / Manager / Meta 改为构造 `Reader/Writer + []Ref`。
4. 已将 Shapefile provider 改为用 refs 的 role/path 查找对应文件，不依赖 contentio multi 接口。
5. 已删除 `StaticMultiReader/StaticMultiWriter`。
6. 已删除 `FirstByExtension`、`MaterializeScope`；`Reader.List` 暂时保留为 scope content 列举接口。
7. 已把稳定结论同步到 `docs/spec/addp内容IO抽象规范.md`。

## 七、暂定结论

`contentio` 的边界应越小越好：它是基于 Go `io` 的平台内容流抽象，不是数据项组织模型，不是格式能力模型，也不是 ADDP Meta 模型。

多个 content 如何组成一个 item，应由 Meta / data item / format 层表达；contentio 只负责按 ref 打开或创建内容。
