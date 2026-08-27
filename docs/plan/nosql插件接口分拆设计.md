# NoSQL 插件接口分拆设计

> 状态：历史方案已收口。当前实现以 provider 化 engine plugin 体系为准。

本文只保留 MongoDB 与 Neo4j 的当前语义结论。正式接口规范见 [../spec/addp引擎插件接口规范.md](../spec/addp引擎插件接口规范.md)，路径规范见 [../spec/addp存储引擎路径体系规范.md](../spec/addp存储引擎路径体系规范.md)。

## 当前结论

- MongoDB 是动态 schema 记录集合型存储，Catalog Model 为 `database -> collection`，其中 `collection` 是 catalog leaf。
- Neo4j 是图存储，Catalog Model 为 `database -> graph`，其中 `graph` 是 catalog leaf；label、relationship type 和 endpoint pattern 属于 `type_info.graph` 结构事实，不作为 catalog leaf。
- 上层模块不再通过统一的 NoSQL `ListCollections` 语义处理 MongoDB 与 Neo4j。
- 目录发现统一走 `EngineCatalogProvider.ListChildren`。
- Catalog leaf facts 统一走 `EngineCatalogFactsProvider.DescribeEngineCatalogFacts`。
- MongoDB 动态字段推断可使用 `DynamicSchemaSamplingProvider`。
- Neo4j 图查询和预览走 `GraphQueryProvider`。

## 落库语义

| 引擎 | Meta Node Type | Meta Item Type |
| --- | --- | --- |
| MongoDB | `database` | `collection` |
| Neo4j | `database` | `graph` |

Neo4j 不得再把 label 或 relationship 折叠为 `collection`，也不得把 label / relationship type 作为独立 catalog leaf 落库。

## 验证重点

- MongoDB database 能展开 collection，collection 可扫描字段并预览样本。
- Neo4j database 能展开 graph leaf，图结构事实进入 `type_info.graph`。
- Manager 预览 Neo4j graph leaf 时走 graph runtime / graph sample provider。
