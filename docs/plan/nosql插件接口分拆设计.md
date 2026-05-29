# NoSQL 插件接口分拆设计

> 状态：历史方案已收口。当前实现以 provider 化 engine plugin 体系为准。

本文只保留 MongoDB 与 Neo4j 的当前语义结论。正式接口规范见 [../spec/addp引擎插件接口规范.md](../spec/addp引擎插件接口规范.md)，路径规范见 [../spec/addp存储引擎路径体系规范.md](../spec/addp存储引擎路径体系规范.md)。

## 当前结论

- MongoDB 是文档型存储，Catalog Model 为 `database -> collection`。
- Neo4j 是图存储，Catalog Model 为 `database -> label/relationship`。
- 上层模块不再通过统一的 NoSQL `ListCollections` 语义处理 MongoDB 与 Neo4j。
- 目录发现统一走 `CatalogProvider.ListChildren`。
- 叶子元数据统一走 `ItemMetadataProvider.DescribeItem`。
- MongoDB 动态字段推断可使用 `DynamicSchemaSamplingProvider`。
- Neo4j 图查询和预览走 `GraphQueryProvider`。

## 落库语义

| 引擎 | Node Type | Item Type |
| --- | --- | --- |
| MongoDB | `database` | `collection` |
| Neo4j | `database` | `label` / `relationship` |

Neo4j 不得再把 label 或 relationship 折叠为 `collection`。

## 验证重点

- MongoDB database 能展开 collection，collection 可扫描字段并预览样本。
- Neo4j database 能展开 label 和 relationship，二者 locator 分别使用 `type=label` 与 `type=relationship`。
- Manager 预览 Neo4j label/relationship 时走 graph runtime。
