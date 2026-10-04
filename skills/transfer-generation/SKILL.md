---
name: transfer-generation
description: 理解 ADDP Transfer 平台能力，发现并确认源和目标、生成草稿，并在用户复核后创建未启动的传输任务。用户要求导入、导出、同步或复制已有数据，例如 MongoDB 到 PostgreSQL 时使用。创建不等于运行。
---

# Transfer 任务生成与创建

先读取平台能力语义，再复用数据发现方法确认唯一源资源和真实目标父节点。当前创建只支持 bounded snapshot native-table；可以保存任务定义，不能启动、定时执行或写业务数据。不要求用户已有领域本体、Standard、Model、Quality 或 Catalog 内容。

## 核心流程

1. 调用 `platform.capability.context(capability="transfer.task.create")`，理解其概念、前置条件和效果。`availability=not_observed` 不是可用性证明；实际 Tool 错误需明确呈现，不假装其他模块存在。
2. 用 `engine.list` 确认用户可访问的源/目标引擎。按 `data-discovery` 只搜索源侧数据项，必要时限定源 engine_id；多个集合或引擎候选等待用户选择。
3. 通过 `resource.ancestors.get → resource.facts.get` 确认源身份和最新结构，不用样本猜 Schema。缺扫描事实则要求补扫描，不能读取原始行兜底。
4. 用 `resource.children.list(engine_id=<目标>)` 从 owner 返回的真实根节点开始逐层浏览；后续父 locator 只取自返回结果。让用户确认目标 `parent_locator + name`，目标尚不存在时不能把它当成已有输入搜索。不可假定 PostgreSQL schema 是 public。
5. 确认 bounded/snapshot、显式 apply_mode（replace/append/upsert）、必要 keys、目标字段及类型。MongoDB 嵌套字段必须先确认行粒度；只读 MQL `$project`/单次 `$unwind` 明确整形，field_mapping 只引用实际查询输出。不能递归摊平、多数组猜粒度或凭业务名猜字段。复杂计算交给 Develop，不伪造本次 Tool 能力。
6. 如需名称、描述或直接字段映射辅助，可调用 `transfer.draft.generate`；它仍只产生草稿，不负责创建，且不能覆盖用户确定的 endpoint/边界/策略。已有完整确认配置时不必调用 Copilot。
7. 展示任务名、源、目标、行粒度、字段映射、运行边界、装载和目标策略，用 clarification 请用户明确确认创建。未确认不调用写 Tool；只要用户要求草稿就停在草稿。
8. 确认后调用 `transfer.task.create`，只提交 Manifest 接受的 name/description/config/batch_size。成功必须返回真实 ID、idle/stopped、无计划；报告“任务已创建，尚未运行”，给出 Transfer 页面入口。不宣称数据已同步，不调用其他执行 Tool。创建响应不确定或失败时不自动重发，要求用户到 Transfer 核对。

## 配置编码要求

- `source.query.statement` 是序列化后的单个 JSON command object，不是 mongosh / JavaScript 文本。基础投影使用 `{"aggregate":"<已确认的 query_names.mql>","pipeline":[{"$project":{"<已确认的字段>":1}}]}` 的结构；占位符必须替换为 owner 事实，不能提交示例占位符。禁止 `db.<collection>.find(...)`、`db.<collection>.aggregate(...)` 或只提交 `$project` 阶段。不要从 full_name 或 locator 拼接 collection 名。
- `field_mapping.fields[].target_type` 使用 ADDP 的标准字段类型，不使用目标数据库的 DDL 类型。字符串与布尔字段分别提交 `string`、`bool`；用户提出 PostgreSQL `text`、`boolean` 时，复核中可保留其物理类型意图，但任务配置必须使用 `string`、`bool`，具体数据库类型由 owner Provider 映射。其他类型依照平台字段类型事实确定，不猜测。
- 目标 schema 等父节点用 `resource.ancestors.get` / `resource.children.list` 确认；`resource.facts.get` 只查询已存在的数据项，不用于 schema、database 或尚未创建的目标。

## 必须澄清

- 未识别出唯一源资源；
- 目标父节点、目标名称或目标引擎未确认；
- bounded/continuous、snapshot/incremental 或目标写入策略未确定；
- 字段映射引用了不存在的源字段；
- 用户要求立即运行、continuous、incremental 或定时计划，但当前创建 Tool 不提供这些能力；
- MongoDB 嵌套结构的行粒度和查询输出字段没有明确；
- 草稿与创建配置的字段、类型或目标策略不一致。

## 验证

- source locator 来自已确认并重新校验的 `ResourceFact`。
- target 使用 owner 确认的 `parent_locator + name`，没有虚拟 locator。
- 草稿没有 credential、connection info、内部 URL 或已删除的旧配置字段。
- 只根据创建 Tool 成功返回的任务身份报告已创建，草稿结果不是创建证据。

## 反模式

- 不把目标资源当成已有输入资源搜索或确认。
- 不让模型改变运行边界、装载模式、目标策略或引擎选择。
- 不根据常见字段名猜测映射，不保留模型生成的未知源字段。
- 创建只用 Manifest 已登记的 `transfer.task.create`；同名 Permission 不代表执行授权。
- 不绕过 Transfer owner API 创建或运行任务。
