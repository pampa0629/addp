# Storage Stream 下载与预览架构结论

## 背景

在 ADDP 的典型用户环境中，浏览器不能假设可以直接访问真实数据存储引擎。真实存储可能位于内网、专有网络或只允许后端服务访问，例如 MinIO、S3、NFS、NAS、HDFS 等。

因此，ADDP 对前端暴露的原始内容下载与预览入口不应绑定具体存储引擎，也不应默认返回对象存储直连 URL。浏览器的稳定访问对象应是 ADDP 后端服务。

## 总体结论

原始内容读取、媒体预览、PDF Range 读取、逻辑对象下载等能力统一走 ADDP 后端代理：

```text
Browser
  -> ADDP Gateway / Manager Backend
  -> common/engine 插件
  -> 真实数据存储引擎
```

但这些能力不能混成一个 API 语义。Manager 应区分两层：

- `storage-stream`：读取一个存储叶子内容，支持 Range，服务在线预览和单叶子流式读取。
- `storage-download`：下载一个逻辑存储对象，内部通过 DownloadPlan 自动决定单叶子流式下载、multi refs 打包下载或后续导出任务。

因此，前端右上角“下载”不应直接理解预览材料，也不应自己解释格式或 refs；它应打开 Manager 给出的逻辑下载 URL。

## 核心概念

- `storage_ref`：后端可打开的存储叶子内容引用。
  - 对象存储 catalog 中为 `bucket/path/to/file`。
  - 文件 catalog / NFS 中为 `path/to/file`。
  - ZIP entry、Excel sheet、SQLite table 等容器内部 child 不是存储叶子内容，不应伪造 `storage_ref`。
- 逻辑存储对象：用户看到的一个可管理数据对象。它可能是一个存储叶子，也可能由多个相关存储叶子组成，例如 Shapefile。
- `DownloadPlan`：Manager 后端把 `engine_id + storage_ref` 解析成的统一下载计划。
  - `stream`：单存储叶子下载。
  - `bundle`：多个相关存储叶子打包下载。
  - `export`：非存储叶子导出，例如数据库表或查询结果，后续接 Transfer/export 任务。
- `storage-stream URL`：ADDP 后端代理流 URL，语义是读取单个存储叶子。
- `storage-download URL`：ADDP 后端逻辑下载 URL，语义是下载一个逻辑存储对象。
- `external_url`：未来可选的插件优化能力，例如 MinIO/S3 presigned URL。它不是默认契约，也不能替代 `storage-stream`。

## DownloadPlan

所有用户触发的原始下载都应先解析为 DownloadPlan，而不是在前端或 Handler 中按格式写分支。

计划结构语义：

```text
DownloadPlan
  kind: stream | bundle | export
  filename
  content_type
  refs:
    - storage_ref
      role
      required
      primary
      filename
```

解析流程：

1. 校验用户认证、租户隔离和引擎访问权限。
2. 将入口 `storage_ref` 解析为对应引擎的 catalog path。
3. 尝试通过 Meta 根据 `engine_id + storage_ref` 找回 item 元数据。
4. 读取 item attributes 中的通用事实，例如 `item.layout`、`item.refs`、`item.format`、`storage.physical_path`。
5. 生成 refs：
   - single item：refs 只有入口 `storage_ref`。
   - multi item：必须使用 Meta 中已裁决的 `item.refs`。
   - multi item 缺失 refs、缺少唯一 primary 或 required refs 不完整时，应返回明确错误并触发重新扫描，不在 Manager 下载链路中重新猜测 sibling content。
6. 校验 refs 都属于当前引擎下的合法存储叶子；required ref 缺失时失败，optional ref 可跳过。
7. 决定执行形态：
   - refs 数量为 1：`stream`。
   - refs 数量大于 1：`bundle`。
   - 没有 storage leaf，例如数据库表：`export`。

`DownloadPlan` 是 Manager 后端内部抽象。前端不应从 `content.metadata.refs` 拼下载 URL，也不应理解 Shapefile 等格式的组成规则。

## `storage-stream` 能力

`storage-stream` 使用同一个 HTTP API 支持两种读取模式，且永远只读取一个存储叶子：

- 不带 `Range` header：返回完整内容，HTTP 状态码为 `200 OK`。
- 带 `Range: bytes=start-end`：返回指定字节范围，HTTP 状态码为 `206 Partial Content`。

Manager 后端负责：

- 用户认证、租户隔离、引擎权限校验。
- 将 `storage_ref` 转换为插件层 `CatalogPath`。
- 调用插件 `OpenContent` 或 `OpenRange`。
- 设置 `Content-Type`、`Content-Length`、`Content-Disposition`、`Accept-Ranges`、`Content-Range` 等响应头。
- 以流式方式把真实存储内容写回浏览器。

`storage-stream` 不负责 multi item 自动打包，也不应根据 item layout 悄悄改变返回内容类型。它的稳定职责是单叶子流式读取。

## `storage-download` 能力

`storage-download` 是用户点击“下载”时的默认入口，语义是下载一个逻辑存储对象。

后端流程：

```text
GET /storage-download?engine_id=...&storage_ref=...
  -> Resolve DownloadPlan
  -> stream: 复用单叶子 OpenContent/OpenRange 能力返回原文件
  -> bundle: zip.NewWriter 流式写入 refs
  -> export: 返回明确的导出任务语义，数据库表后续交由 Transfer/export
```

`storage-download` 可以根据 DownloadPlan 返回原文件流或 zip 包。这个行为是透明且符合语义的，因为它下载的是逻辑对象，而不是一个固定的存储叶子。

multi item 默认下载 zip 包，而不是一次点击触发多个浏览器下载。原因：

- 浏览器对多文件下载限制多，体验不稳定。
- multi item 是一个逻辑对象，zip 更符合完整交付语义。
- 后端可以统一控制 entry 文件名、required/optional ref 校验、缺失错误和流式写出。

用户查看 multi item 中的某个单独 ref 时，仍可通过该 ref 的 `storage_ref` 走单叶子下载。

## 存储引擎差异

MinIO / S3：

- 插件通过对象存储 SDK 的 `GetObject` 读取内容。
- Range 请求通过 SDK 的 range options 下推到对象存储。
- 即使底层支持 presigned URL，默认仍应走 ADDP 后端代理流。

NFS：

- 插件通过 NFS client 打开文件。
- Range 请求通过 `Seek` 到偏移位置，再限制读取长度实现。
- NFS 没有天然浏览器可访问的 HTTP 下载 URL，因此必须走 ADDP 后端代理流。

## 前端下载原则

右上角“原始下载”不应使用 axios `responseType: blob` 拉完整文件。这样会把大文件完整放入浏览器 JS 内存，并受 axios timeout 影响。

更合理的方式是：

- 对 Manager 返回的 `storage-download URL` 创建原生 `<a>` 下载链接。
- 认证 token 可追加到 query 参数中，由后端认证中间件转换为 `Authorization`。
- 浏览器直接从 ADDP 后端流式下载，JS 不持有完整文件内容。
- 前端不根据 `format`、`preview_material`、`frontend_renderer` 判断原始下载能力。
- 前端不解释 multi refs，也不拼 zip；multi 由 `storage-download` 后端计划统一处理。

表格预览导出、文本导出、base64 小内容导出仍可以使用 Blob，因为这些内容本身来自前端已持有的预览材料。

数据库表、SQL 查询结果、计算结果不是存储叶子。当前页 CSV 导出可以作为预览材料导出保留，但它不是“原始下载”。完整数据库表下载应归入 `export`，后续交由 Transfer/export 任务生成文件，再返回新的 `storage_ref` 下载。

## Raw Binary 与 Unsupported 预览

raw binary 是通用读取和下载兜底，不是通用在线预览材料。

当格式无法被 ADDP 当前内置能力识别时，平台仍应尽可能保留原始字节读取能力：

- 对存储叶子内容，通过 `storage-download` 下载逻辑对象；单叶子内部可落到 `storage-stream` 能力。
- 对 `format=unknown` 且非文本内容，通过 `BinaryContentReader` 读取原始字节片段，供后续传输、计算端或专业解析器使用。

Manager 在线预览层不能因为具备 raw binary 读取能力，就把未知二进制当作可预览材料展示。未知格式应返回 `preview_material=unsupported`、`frontend_renderer=unsupported`，由前端展示“不支持在线预览”；但只要存在合法 `storage_ref`，原始文件下载仍应可用。

因此：

- `preview_material=unsupported` 表示“不支持在线预览”，不表示“不可下载”。
- `raw_binary` / `BinaryContentReader` 表示“可读取原始字节”，不表示“前端应展示二进制内容”。
- 下载能力应由 `storage_ref` / `storage-download` / DownloadPlan 决定，预览能力由 `preview_material` / `frontend_renderer` 决定。

## 大文件策略

后端不主动按固定段大小拆分完整下载。分段读取由客户端通过 HTTP Range 决定：

- PDF.js 可配置固定 Range chunk size。
- video 标签通常由浏览器自行发送 Range 请求。
- 普通下载默认由浏览器下载栈处理完整响应。

后端必须稳定支持 Range，因为这是大文件预览、媒体播放和部分浏览器下载优化的基础能力。

## 责任边界

- `common/format`：声明格式能力和 related refs 规范，例如 `RelatedRefSpecs()`；不组装 Manager 下载 DTO，不访问具体引擎。
- `meta`：保存 item 的 layout、format、refs、storage physical path 等事实。
- `manager/service`：把 `engine_id + storage_ref` 解析成 DownloadPlan，并执行 stream/bundle。
- `manager/api`：暴露 `storage-stream` 和 `storage-download`，维护 HTTP 语义和响应头。
- `manager/frontend`：打开后端下载 URL；只在没有 storage leaf 的预览材料上做显式“导出当前页/导出预览内容”。

## 禁止事项

- 不得把 multi item 的完整下载逻辑放到前端。
- 不得让前端从 `content.metadata.refs` 拼接下载 URL。
- 不得把 `storage-stream` 改造成有时返回单文件、有时返回 zip 的混合语义。
- 不得仅因 table preview 就把原始下载降级成当前页 CSV。
- 不得用 unknown binary reader 代替原始下载链路。
- 不得为某个格式在下载 Handler 中写特殊分支；格式差异应收敛到 Meta 已入库的 item refs 和 DownloadPlan 的统一消费。

## 后续建议

1. 前端右上角下载从 axios blob 改为浏览器原生下载 ADDP `storage-download` URL。
2. Manager 后端新增 DownloadPlan resolver，统一处理 single / multi / export。
3. `storage-stream` 保持单叶子 Range 流能力，继续服务 PDF、图片、视频等在线预览。
4. 数据库表完整下载后续接 Transfer/export 任务，不与存储叶子原始下载混用。
5. 未来如需支持 presigned URL，应作为插件声明的可选优化能力，不作为 Manager 预览/下载默认路径。
