---
name: transfer-generation
description: 理解 ADDP Transfer 平台能力，发现并确认源和目标、生成草稿，并在用户复核后创建未启动的传输任务。用户要求导入、导出、同步或复制已有数据，例如 MongoDB 到 PostgreSQL 时使用。创建不等于运行。
---

# Transfer 任务生成与创建

先读取平台能力语义，再复用数据发现方法确认源资源和真实目标父节点。条件、效果和支持边界以 `platform.capability.context` 为唯一语义来源；本 Skill 只说明获取事实、补齐缺口和组织交互的方法。不要求用户已有领域本体、Standard、Model、Quality 或 Catalog 内容。

## 核心流程

1. 理解 Runtime 在本次 Harness 进入时经 `platform.capability.context(capability="transfer.task.create")` 读取、并放入当前受信任状态的能力定义；无需重复读取。需要主动刷新时仍调用同一 Tool，失败不能沿用旧结果。理解其概念、前置条件和效果。`availability=not_observed` 不是可用性证明；实际 Tool 错误需明确呈现，不假装其他模块存在。
2. 用 `engine.list` 确认用户可访问的源/目标引擎。按 `data-discovery` 只搜索源侧数据项，必要时限定源 engine_id；多个集合或引擎候选等待用户选择。搜索失败时明确报告并终止当前运行，不能把失败当作零召回，也不能静默改用源目录枚举来继续生成或创建。
3. 通过 `resource.ancestors.get → resource.facts.get` 确认源身份和最新结构，不用样本猜 Schema。缺扫描事实则要求补扫描，不能读取原始行兜底。
4. 用 `resource.children.list(engine_id=<目标>)` 从 owner 返回的真实根节点开始逐层浏览；后续父 locator 只取自返回结果。让用户确认目标 `parent_locator + name`，目标尚不存在时不能把它当成已有输入搜索。不可假定 PostgreSQL schema 是 public。
5. 根据能力定义逐项补齐运行边界、装载、写入策略、目标字段及类型。有嵌套字段时让用户决定行粒度，再用明确的 MQL `$project`/单次 `$unwind` 表达；配置编码只覆盖直接字段引用，不生成复杂表达式。复杂计算交给 Develop，不伪造本次 Tool 能力。
6. 如需名称、描述或直接字段映射辅助，可调用 `transfer.draft.generate`；调用前由本步骤的 Owner Tool 确认资源和字段并提交完整上下文。Copilot 不再代替 Agent 访问资源，不申请二次委托，只产生草稿；结果不是授权或资源真实性证明，不负责创建，且不能覆盖用户确定的 endpoint/边界/策略。没有资源时 Tool 只返回意图检索词，由 Agent 继续调用 Owner Tool，不把检索词当作候选身份。已有完整确认配置时不必调用 Copilot。
7. 调用 `request_clarification`：`reason="transfer_create_review"`，`operation_review={"tool":"transfer.task.create","arguments":<完整创建参数>}`，options 提供 value 为 `confirm` 和 `cancel` 的两个选项及用户语言的标签。prompt 用简短业务摘要说明源/目标名称、行粒度、边界/装载方式、字段处理概要、写入策略及其影响，并明确只创建、不运行；不要在摘要中重复 locator、MQL、完整字段清单或 JSON。Runtime 核验条件、追加可展开查看的确切参数并绑定指纹，不能用摘要替代完整参数复核。只有用户选择 Runtime 绑定的确认选项后才提交同一配置，不能改字段、查询、策略、名称或批大小；文字回答是补充要求，不是创建确认，有修改先重新复核。只要求草稿时不进入此步骤。
8. 确认后调用 `transfer.task.create`，只提交 Manifest 接受的 name/description/config/batch_size。成功必须返回真实 ID、idle/stopped、无计划；报告“任务已创建，尚未运行”，给出 Transfer 页面入口。不宣称数据已同步，不调用其他执行 Tool。创建响应不确定或失败时不自动重发，要求用户到 Transfer 核对。

## 配置编码要求

- `source.query.statement` 是序列化后的单个 JSON command object，不是 mongosh / JavaScript 文本。基础投影使用 `{"aggregate":"<已确认的 query_names.mql>","pipeline":[{"$project":{"<已确认的字段>":1}}]}` 的结构；占位符必须替换为 owner 事实，不能提交示例占位符。禁止 `db.<collection>.find(...)`、`db.<collection>.aggregate(...)` 或只提交 `$project` 阶段。不要从 full_name 或 locator 拼接 collection 名。
- `field_mapping.fields[].target_type` 使用 ADDP 的标准字段类型，不使用目标数据库的 DDL 类型。字符串与布尔字段分别提交 `string`、`bool`；用户提出 PostgreSQL `text`、`boolean` 时，复核中可保留其物理类型意图，但任务配置必须使用 `string`、`bool`，具体数据库类型由 owner Provider 映射。其他类型依照平台字段类型事实确定，不猜测。
- 目标 schema 等父节点用 `resource.ancestors.get` / `resource.children.list` 确认；`resource.facts.get` 只查询已存在的数据项，不用于 schema、database 或尚未创建的目标。
- 可选 `transfer.draft.generate` 的 `resources[]` 使用已确认的 `ResourceFact`，必须包含 `role + locator`；源用途可明确为 `role="source"`。不要直接提交搜索候选，也不要混入 `name`、`item_type`、`row_grain`、`project_fields`。行粒度与投影通过正式 `source.query` 表达，不是资源事实字段。
- 草稿调用中的 `task` 使用 `name/description/task_type/config` 层级，`task_type="sync"`。`runtime/load/source/target/transforms` 全部放入 `task.config`，写入策略放在 `task.config.target.policy`，字段映射放在 `task.config.transforms` 的 `field_mapping` 中；源查询放在 `task.config.source.query`。不能把创建 Tool 的顶层 `config` 参数误当作草稿调用的 `task`，也不能自造顶层 `field_mapping`。缺少目标或配置时先澄清，不让 Copilot 猜测。

## 补齐条件

以当前能力的 `operation.inputs_required` 及对应概念含义检查缺口。缺少用户业务决定时逐项澄清；缺少 owner 事实时先取事实。用户目标超出 `effects` 或落入 `excluded_effects` 时解释本次能力边界，不能缩减用户目标后继续创建。Runtime 返回 `platform_condition_unsatisfied` 时按条件身份补齐，不重复调用写 Tool，也不把程序拒绝当作创建成功。

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
