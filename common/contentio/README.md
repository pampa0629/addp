# common/contentio

`common/contentio` 是 ADDP 基于 Go `io` 之上的内容 I/O 抽象层。

它只表达 content 的定位、读取和写入，不连接 engine，不解析格式，不返回上层 DTO。

## 核心概念

- `Ref`：一个已确定 content 的定位器。
- `Reader` / `Writer`：按 `Ref` 打开输入流或创建输出流。
- `Lister`：按 scope `Ref` 列举子 content。
- `RangeReader`：按 `Ref` 打开字节范围流。
- `Stat`：`Reader.Stat` 返回的轻量内容状态，只包含存在性、大小、MIME、修改时间等单 content I/O 事实，不承载 ADDP Meta 或 scope 统计语义。

多个 content 共同构成一个 format item 时，由 format / dataitem / 调用编排层显式传递上层相关引用集合；`contentio` 不定义 `multi` 组织模型。
多 content 的相关 ref 规则属于 `common/format.RelatedRefSpec`，不属于 `contentio`。

`NewRef` 是带默认规范化的构造器：去除路径首尾 `/`，并将 `Role` 规范为小写。显示名按需从 `Path` 派生，例如使用 `BaseName(ref)`。必需性和 primary 等集合标注不属于 `Ref`，由 format、dataitem 或调用编排层的上层结构承载。

scope 列举能力通过独立 `Lister` 表达；按扩展名选择、物化到本地目录等编排工具不属于 `contentio` 核心。

engine 到 contentio 的适配放在 `common/engine/contentadapter`。
