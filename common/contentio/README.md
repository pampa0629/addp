# common/contentio

`common/contentio` 是 ADDP 基于 Go `io` 之上的内容 I/O 抽象层。

它只表达 content 的定位、读取和写入，不连接 engine，不解析格式，不返回上层 DTO。

## 核心概念

- `Ref`：一个已确定 content 的定位器。
- `Reader` / `Writer`：按 `Ref` 打开输入流或创建输出流。
- `RangeReader`：按 `Ref` 打开字节范围流。
- `Stat`：`Reader.Stat` 返回的轻量内容状态，只包含存在性、大小、MIME、修改时间等 I/O 事实，不承载 ADDP Meta 语义。
- `RelatedRefSpec`：从主 `Ref` 推导相关 refs 的轻量规则。

多个 content 共同构成一个 format item 时，由 format / dataitem / 调用编排层显式传递 `[]Ref`；`contentio` 不定义 `multi` 组织模型。

scope 列举能力只保留在 `Reader.List` 接口上；按扩展名选择、物化到本地目录等编排工具不属于 `contentio` 核心。

engine 到 contentio 的适配放在 `common/engine/contentadapter`。
