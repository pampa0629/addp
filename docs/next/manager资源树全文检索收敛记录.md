# Manager 资源树搜索与数据检索收敛记录

## 背景

当前文档型数据的全文检索链路已经和访问定位索引拆开：

- 表格型文件的定位索引使用 `attributes.access_index.<data_type>`，例如 `access_index.table` 的稀疏行号到字节偏移索引。
- 文档正文全文索引写入 Meilisearch，Meta attributes 只记录 `capabilities.extraction.index_ref`。
- 原始内容哈希写入 `attributes.storage.content_hash`，用于判断内容是否变化。
- `index_ref=meilisearch:assets:<item_fingerprint>` 引用的是 item 指纹对应的外部索引记录，不是内容哈希。

因此 Manager 不应按文件格式、`DocumentInfo`、`access_index` 或某个 attributes 状态字段自行判断文档正文是否可检索。全文检索能力应统一走 Meilisearch / `HybridSearchService`，资源树能力应保持在资源浏览和定位边界内。

## 概念边界

ADDP 的资源到平台语义主链条是：

```text
engine -> node -> data item
```

- `node` 表示 engine 内的资源树组织，例如 schema、bucket、prefix、目录、catalog 分组节点。
- `data item` 是平台真正管理、预览、检索、授权、传输和治理的数据对象。
- 文档正文全文索引是 data item 深度扫描或提取任务产生的内容处理结果，不改变 node 和 data item 的身份边界。

据此，Manager 中应区分两个功能：

| 功能 | 入口 | 语义 | 搜索对象 |
| --- | --- | --- | --- |
| 资源树搜索 | `GET /api/v1/manager/tree/:engine_id/search` | 数据探查页内的树节点定位 / 过滤 | node / 资源树展示节点 |
| 数据检索 | `GET /api/v1/manager/search` | 全局数据资产检索 | data item / Meilisearch 资产索引 / 向量索引 |

资源树搜索不应成为第二套全文检索入口。数据检索结果需要回到资源树时，应通过定位能力展开并选中对应资源节点。

## 当前事实

### Meta deep scan

Meta 的 deep scan 对文档正文抽取已经走统一格式接口：

```text
catalog item deep scan
  -> format.GetDocumentTextReader(formatType)
  -> capabilities.extraction.{status, extractor, plain_text_preview, index_ref}
  -> Meilisearch assets index
```

DOCX / PPTX 已实现 `DocumentTextReader`，WPS 暂不支持后端正文抽取，deep scan 会记录 unsupported 并仍计算 `storage.content_hash`。

### Meilisearch 记录语义

索引记录语义已统一为：

- `id`：item fingerprint，Meilisearch 主键。
- `asset_id`：item fingerprint。
- `document_id`：item fingerprint，用于全文检索和向量检索结果合并。
- `content_hash`：`storage.content_hash`，用于内容变化判断。

### Manager 资源树搜索现状

`manager/backend/internal/service/explorer_service.go` 的 `SearchNodes` 当前仍然是：

1. 通过 System 获取 engine。
2. 从 Meta 拉取完整 metadata tree。
3. 用 `TreeBuilder.BuildFromMeta` 构建完整资源树。
4. 递归遍历树节点，只按 `TreeNode.Label` 做大小写不敏感的字符串包含匹配。

也就是说，资源树搜索现在只能搜资源名 / 节点名，不能搜 Meilisearch 中的正文内容。这个行为符合“树内定位”的功能边界，但实现和前端契约还没有完全收敛。

### Manager 前端入口现状

Manager 前端存在两套搜索相关 UI：

- 数据探查页 `DataExplorer.vue` 引入了 `ExplorerSearch`，但 `showSearch` 当前固定为 `false`，普通用户界面不可见。
- `ExplorerSearch.vue` 和 `explorer` store 已实现调用 `/manager/tree/:engine_id/search` 的逻辑。
- 数据检索页 `DataRetrieval.vue` 是独立路由 `data-retrieval`，调用 `/manager/search`，并支持搜索历史、分页、全文高亮和定位按钮。

另有一个契约问题需要单独处理：`ExplorerSearch.vue` 期望结果项形态为 `result.node / result.path / result.match_type / result.score`，但后端当前直接返回 `TreeNode` 列表。因为资源树搜索入口被隐藏，这个问题目前未暴露到用户路径。

## 收敛结论

### 1. 资源树搜索保持树内定位语义

资源树搜索应负责：

- 在指定 engine 的资源树中按节点名、路径或轻量展示元数据查找节点。
- 支持 node type 过滤。
- 返回可用于前端展开、选中、预览的定位信息。

资源树搜索不负责：

- 文档正文全文检索。
- 向量语义检索。
- 基于文件格式判断是否可全文检索。
- 通过 `DocumentInfo`、`access_index` 或 attributes 状态补推导抽取结果。

### 2. 数据检索统一负责全文和语义检索

`GET /api/v1/manager/search` 应继续作为 Manager 的全文检索和语义检索入口：

- 查询 Meilisearch / 向量索引。
- 返回 data item 维度的检索结果。
- 返回 `content_preview`、`highlights`、`match_methods`、score 等检索展示信息。
- 记录搜索历史。

如果用户要搜文档正文，应进入“数据检索”，而不是资源树搜索。

### 3. 两个功能通过定位能力协作

数据检索结果应能定位到数据探查资源树：

```text
DataRetrieval result
  -> engine_id + item identity / locator facts
  -> DataExplorer
  -> 展开资源树路径
  -> 选中对应 node / item
  -> 加载预览
```

定位契约统一为：

- `/api/v1/manager/search` 的每条检索结果必须返回标准 `locator`。
- 新索引记录由 Meta 写入标准 `locator`；Manager 后端优先透传该字段。
- 对尚未重建的旧索引记录，Manager 后端只允许基于 `engine_id`、`asset_type`、`full_name` 或标准 storage 字段，调用 `common/catalogview` 的统一 ResourceLocator 构造逻辑生成 locator。
- 前端只消费 `locator`，不得按 engine type、bucket、path、file name 等字段自行拼接定位符。
- 如果当前索引记录缺少稳定定位字段，应补齐 Meta 索引记录，不得把字段推断下放到前端。

Locator 类型必须来自 catalog item / node 的稳定资源类型：

| 引擎 / 资源 | locator type |
| --- | --- |
| PostgreSQL / MySQL 表 | `table` |
| MongoDB collection | `collection` |
| Neo4j graph | `graph` |
| MinIO / S3 对象 | `object` |
| NFS / 本地文件系统文件 | `file` |

不得因内容格式、是否可表格预览、是否可抽取正文而改变 locator type。例如同一个 CSV 在对象存储中仍是 `object`，在文件系统中才是 `file`。

## 不采用的方案

### 资源树搜索合并全文命中

此前曾考虑：

```text
SearchNodes
  -> 构建完整 TreeNode 树
  -> 递归按 label 搜索
  -> 调用 HybridSearchService.SearchDocuments(query)
  -> 过滤 engine_id
  -> 将命中 item 映射回 TreeNode
  -> 合并去重后返回
```

该方案可以让用户在资源树搜索框中搜到文档正文，但会带来概念混淆：

- 资源树搜索会变成第二套全文检索入口。
- 同一个关键词在“数据探查”和“数据检索”中有不同返回结构、排序、历史和高亮语义。
- node 和 data item 的职责边界被模糊。
- 后续新增 reader 或索引能力时，容易继续产生“某处可搜、某处不可搜”的分叉。

因此当前不建议采用。

## 已执行收敛

1. 资源树搜索入口已在数据探查页启用。
   - 后端仍返回 `TreeNode` 列表。
   - 前端 store 将 `TreeNode` 包装为 `{ node, path, match_type, score }` 供 `ExplorerSearch` 展示。

2. 数据检索定位已收敛为只消费 `locator`。
   - `DataRetrieval` 不再用 `engine_id/bucket/objectKey` 拼 query。
   - `DataExplorer` 不再兼容旧 query，只接受 `locator`。
   - 定位后加载完整资源树，展开路径、选中节点并加载预览。

3. 统一 locator 构造逻辑已下沉到 `common/catalogview`。
   - `Meta` 索引写入时保存标准 `locator`。
   - `Manager` 搜索结果优先使用索引中的 `locator`。
   - 旧索引缺少 `locator` 时，Manager 使用同一套 common 逻辑按 `asset_type/full_name` 生成，不再默认把未知资源猜成对象。

4. 保持 Manager 不解析文档正文。
   - 文档正文抽取、索引写入、索引引用继续由 Meta / 搜索链路统一负责。
   - Manager 只消费搜索结果和定位事实。
