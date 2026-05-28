# Manager 资源树全文检索收敛记录

## 背景

当前文档型数据的全文检索链路已经和访问定位索引拆开：

- 表格型文件的定位索引使用 `attributes.access_index.<data_type>`，例如 `access_index.table` 的稀疏行号到字节偏移索引。
- 文档正文全文索引写入 Meilisearch，Meta attributes 只记录 `capabilities.extraction.index_ref`。
- 原始内容哈希写入 `attributes.storage.content_hash`，用于判断内容是否变化。
- `index_ref=meilisearch:assets:<item_fingerprint>` 引用的是 item 指纹对应的外部索引记录，不是内容哈希。

因此 Manager 资源树搜索不应再按文件格式、`DocumentInfo`、`access_index` 或某个 attributes 状态字段自行判断是否能检索正文。全文搜索入口应统一走 Meilisearch / HybridSearchService。

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

也就是说，资源树搜索现在只能搜资源名 / 节点名，不能搜 Meilisearch 中的正文内容。用户在资源树搜索文档正文关键词时，如果文件名不包含该关键词，会搜不到。

## 问题

资源树搜索和全局搜索当前是两条入口：

- 资源树搜索：`GET /api/v1/manager/tree/:engine_id/search`
- 全局混合搜索：`GET /api/v1/manager/search`

全局搜索已经使用 `HybridSearchService.SearchDocuments` 查询 Meilisearch / 向量索引；资源树搜索没有接入该服务。

这会造成：

- 文档 deep scan 已经写入 Meilisearch，但资源树搜索搜不到正文。
- 用户会误以为文档没有被抽取或没有被索引。
- 后续新增格式 reader 后，如果资源树搜索仍不走统一搜索入口，会继续出现“某处可搜、某处不可搜”的分叉。

## 建议方案

### 方案 A：资源树搜索合并全文命中

保留现有资源树名称搜索，同时把 `HybridSearchService` 注入 `ExplorerService`：

```text
SearchNodes
  -> 构建完整 TreeNode 树
  -> 递归按 label 搜索，保留现有行为
  -> 调用 HybridSearchService.SearchDocuments(query)
  -> 过滤 engine_id
  -> 将命中 item 映射回 TreeNode
  -> 合并去重后返回
```

映射 TreeNode 时可使用多键索引：

- `TreeNode.Metadata.full_name`
- `TreeNode.Locator` 解析后的 path
- `TreeNode.Metadata.item.full_name`
- 未来如果 TreeNode metadata 带 item fingerprint，则优先用 fingerprint

全文命中可以附加到 `TreeNode.Metadata.search_match`：

```json
{
  "search_match": {
    "document_id": "<item_fingerprint>",
    "asset_id": "<item_fingerprint>",
    "match_methods": ["keyword"],
    "score": 0.98,
    "content_preview": "...",
    "highlights": {}
  }
}
```

优点：

- 前端资源树搜索接口不变。
- 现有按名称搜索行为不丢。
- 用户在资源树搜索正文关键词时能命中文档 item。

注意：

- 不要在 Manager 中按 docx / pptx / pdf / wps 分支。
- 不要通过 `metadata_extracted`、`type_info.document`、`access_index` 判断是否需要补抽。
- 搜索服务不可用时应降级为现有名称搜索。

### 方案 B：资源树搜索直接委托统一搜索

资源树搜索不再构建完整树搜索，只调用统一搜索服务并把结果转换成资源树节点形态。

优点是路径更统一；缺点是可能改变现有“按节点名搜索目录 / schema / bucket”的行为，迁移风险更高。

当前建议先采用方案 A。

## 待确认点

1. `TreeNode.Metadata` 是否应显式带 `fingerprint`。
   现在 `TreeBuilder.ConvertMetaItems` 通过 `withMetaItemFacts` 合并了 item facts，但需要确认 `common/models.MetaItem` 中的 fingerprint 是否已经进入 `Attributes` 或 metadata。若没有，建议补一个只读展示事实，方便全文命中稳定映射回树节点。

2. 全文命中是否需要返回高亮片段。
   `HybridSearchService` 已经有 `Highlights` 和 `ContentPreview`，资源树搜索可先放入 `metadata.search_match`，前端后续再决定是否展示。

3. `node_types` 过滤是否应用于全文命中。
   建议应用。全文命中映射成 TreeNode 后，按 TreeNode.Type 过滤。

4. limit 语义。
   建议先对合并后的结果统一截断；调用全文搜索时 page size 可使用同一个 limit，默认 50。

## 推荐下一步

在新会话中从 `ExplorerService.SearchNodes` 开始实现方案 A，并补测试：

- 搜索服务未配置时，只返回原有名称匹配结果。
- 名称搜索和全文搜索命中同一节点时去重。
- 全文命中 engine_id 不匹配时过滤。
- 全文命中可通过 full_name / path 映射回 TreeNode。

