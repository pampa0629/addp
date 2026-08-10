---
name: query-generation
description: Generate a candidate SQL, MQL, Cypher, or other engine-declared query from natural language within an ADDP query workbench context. Use when an agent needs to turn a business question into query text for one explicitly selected query engine while discovering and confirming only resources accessible in that engine.
---

# 查询生成

生成查询候选，不执行查询。数据身份、字段和空间事实必须来自 owner Tool，查询语言和方言必须来自当前查询引擎的 capability。

## 工作流

1. 要求调用方提供当前查询工作台的 `engine_id` 和 `query_language`。没有当前引擎时先让用户选择，不做全平台自动选择。
2. 用户已经选择资源时，使用 `resource.ancestors.get` 和 `data.preview` 校验 locator、字段、几何列和 CRS；locator 的 Engine 必须等于当前 `engine_id`。
3. 用户未选择资源时，从需求中提取独立输入角色和跨语言检索词，调用带当前 `engine_id` 的 `data.search` 粗筛，再逐个执行 `resource.ancestors.get` 和 `data.preview`。
4. 同一角色只有一个已验证候选时可以直接确认；存在多个候选时完整展示并让用户单选。不得让大模型编造 locator、字段或删除仍然合理的候选。
5. 对全部已确认资源调用 `query.draft.generate`，传入原始需求、`engine_id`、`query_language` 和资源事实。
6. 将候选查询展示给用户编辑。只有用户明确要求执行时，才调用 Develop 查询执行路径；执行前继续遵守 Develop preflight、效果授权和高风险确认。

## 约束

- 查询工作台只允许单一当前 Query Engine。跨引擎查询必须由用户显式选择具备联邦查询能力的 Runtime，并按其公开语法引用已授权 Source Engine；不得把工作流的全租户资源发现规则套到查询工作台。
- 不硬编码 PostgreSQL、`public`、`geom`、`geometry`、表名、引号或空间函数。所有标识符、字段、几何列和 CRS 都来自已验证事实，语法来自当前引擎和查询语言。
- 不向 Copilot 传递连接信息，不让 Copilot 拼接 ResourceLocator，不把候选查询视为已授权执行。
- 无法确认数据源、字段不足或当前引擎不支持所需查询能力时返回澄清，不生成看似可执行的占位查询。
