---
name: query-generation
description: Generate a candidate SQL, MQL, or Cypher query from natural language within an ADDP query workbench context. Use when an agent needs to turn a business question into query text for one explicitly selected query engine while discovering and confirming only resources accessible to that runtime.
---

# 查询生成

生成查询候选，不执行查询。数据身份、字段和空间事实必须来自 owner Tool，查询语言和方言必须来自当前查询引擎的 capability。

## 工作流

1. 要求调用方提供当前查询工作台的 `engine_id` 和 `query_language`。没有当前引擎时先让用户选择，不做全平台自动选择。
2. 用户已经选择资源时，按 `data-discovery` 使用 `resource.ancestors.get → resource.facts.get` 校验 locator、字段、几何列和 CRS；原生查询的 locator Engine 必须等于当前 `engine_id`，联邦查询按当前 Runtime capability 校验 Source Engine。
3. 用户未选择资源且没有明确 Catalog 范围时，按 `data-discovery` 提取独立输入角色和跨语言检索词，原生查询调用带当前 `engine_id` 的 `data.search` 粗筛，再确认结构事实。工作台已有 MongoDB database 范围且编辑器为空时，调用 `query.draft.generate` 并传入 `resource_scope_locator` 获取 owner 验证的 collection 候选，不把 database 放入 `resources[]`。已有 MQL 时，由 Develop 从命令解析主 collection 及关联 collection 并在当前 database 的 Owner Catalog 中确认；`current_query` 不能替代资源或字段事实，解析失败不得退回模糊发现。
4. 同一角色只有一个已验证候选时可以直接确认；存在多个候选时完整展示并让用户单选。不得让大模型编造 locator、字段或删除仍然合理的候选。
5. 对全部已确认资源调用 `query.draft.generate`，传入原始需求、`engine_id`、`query_language` 和资源事实，可携带编辑器已有的 `current_query`。Copilot 负责领域规划、生成和确定性校验；MQL 使用强类型语义计划及编译器，不由 Agent 绕过生成 Tool 自行编译。
6. 将候选查询展示给用户编辑。只有用户明确要求执行时，才调用 Develop 查询执行路径；执行前继续遵守 Develop preflight、效果授权和高风险确认。

## 约束

- 查询工作台只允许单一当前 Query Engine。跨引擎查询必须由用户显式选择具备联邦查询能力的 Runtime，并按其公开语法引用已授权 Source Engine；不得把工作流的全租户资源发现规则套到查询工作台。
- 不硬编码 PostgreSQL、`public`、`geom`、`geometry`、表名、引号或空间函数。所有标识符、字段、几何列和 CRS 都来自已验证事实，语法来自当前引擎和查询语言。
- 不向 Copilot 传递连接信息，不让 Copilot 拼接 ResourceLocator，不把候选查询视为已授权执行。
- `data.preview` 只用于明确查看样本的请求，不用于生成前的结构校验；缺扫描事实时要求刷新扫描，不读取原始行兜底。
- 无法确认数据源、字段不足或当前引擎不支持所需查询能力时返回澄清，不生成看似可执行的占位查询。
