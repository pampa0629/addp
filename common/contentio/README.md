# common/contentio

`common/contentio` 是 ADDP 基于 Go `io` 之上的内容 I/O 抽象层。

它只表达 content 的定位、读取、写入和多 content 组合，不连接 engine，不解析格式，不返回上层 DTO。

## 核心概念

- `Ref`：一个已确定 content 的定位器。
- `Reader` / `Writer`：按 `Ref` 打开输入流或创建输出流。
- `RangeReader`：按 `Ref` 打开字节范围流。
- `MultiReader` / `MultiWriter`：围绕一组 `Ref` 的组合读写抽象。
- `RelatedRefSpec`：从主 `Ref` 推导相关 refs 的规则。

engine 到 contentio 的适配放在 `common/engine/contentadapter`。
