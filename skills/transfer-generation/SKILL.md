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
5. 根据能力定义分别确定行粒度和字段保留范围，再补齐运行边界、装载、写入策略、目标字段及类型。不展开数组不等于删除数组字段；用户要求保留顶层字段时，数组和对象也应保留，不能把嵌套叶子误当成顶层列。不能确定目标表示时通过正式预检或澄清处理，不得为规避类型检查而静默删字段。有嵌套字段时让用户决定一文档一行还是单数组元素一行，再用明确的 MQL `$project`/单次 `$unwind` 表达；配置编码只覆盖直接字段引用，不生成复杂表达式。复杂计算交给 Develop，不伪造本次 Tool 能力。
6. 如需名称、描述或直接字段映射辅助，可调用 `transfer.draft.generate`；调用前由本步骤的 Owner Tool 确认资源和字段并提交完整上下文。Copilot 不再代替 Agent 访问资源，不申请二次委托，只产生草稿；结果不是授权或资源真实性证明，不负责创建，且不能覆盖用户确定的 endpoint/边界/策略。没有资源时 Tool 只返回意图检索词，由 Agent 继续调用 Owner Tool，不把检索词当作候选身份。已有完整确认配置时不必调用 Copilot。
7. 调用 `request_clarification`：`reason="transfer_create_review"`，`operation_review={"tool":"transfer.task.create","arguments":<完整创建参数>}`，options 提供 value 为 `confirm` 和 `cancel` 的两个选项及用户语言的标签。Runtime 自动通过正式 `transfer.task.config.check` 预检完整参数，再生成配置复核与绑定指纹；不必由模型重复调用预检。unsupported 必须修正配置，不能继续创建；unknown 不是通过，Runtime 展示未检查项及运行时要求，并取得用户知情确认。supported 也不保证执行成功。prompt 用简短业务摘要说明源/目标名称、行粒度、边界/装载方式、字段处理概要、写入策略及其影响，并明确只创建、不运行；不要在摘要中重复 locator、MQL、完整字段清单或 JSON。不能用摘要替代 Runtime 的完整参数和报告复核。只有用户选择 Runtime 绑定的确认选项后才提交同一配置，不能改字段、查询、策略、名称或批大小；文字回答是补充要求，不是创建确认，有修改先重新复核。只要求草稿时不进入此步骤，也不能宣称已经预检通过。
8. 确认后调用 `transfer.task.create`，只提交 Manifest 接受的 name/description/config/batch_size。Runtime 在提交前重新预检；配置或稳定报告变化后必须重新复核，不能绕过。预检服务、权限或响应失败终止当前运行，不当作 unknown，也不自动重试。成功必须返回真实 ID、idle/stopped、无计划；报告“任务已创建，尚未运行”，给出 Transfer 页面入口。不宣称数据已同步，不调用其他执行 Tool。创建响应不确定或失败时不自动重发，要求用户到 Transfer 核对。

## 草稿阶段的只读检查

每次有效正式预检由 Runtime 自动展示固定报告卡片，状态、检查范围和完整诊断直接来自 owner。历史卡片是该次检查快照，不代表当前配置已通过或运行已获授权。

正式预检参数及字段映射同时由 Runtime 固定展示，并绑定草稿引用。用户后续只解释已有草稿时读取该引用的精确参数，不以新获取的源结构或旧回复正文重建原映射；创建任务只保存定义，目标建表或清理写入发生在实际执行阶段，不能混淆。修改配置须沿原事实、澄清、预检与复核流程，旧草稿引用不继承确认或授权。

报告展示后，事实概要由 Runtime 固定卡片承担；使用用户当前语言，默认只给关键检查解释：

- 固定卡片已按确切参数显示源、目标、运行边界、装载、写入策略及映射数量，详细引用、映射和参数可以展开。不要另写“草稿概要”，不要重复源/目标、字段数量、字段清单、locator、MQL 或配置 JSON；不要根据概要推测行粒度或物理类型，也不要混淆保存定义与目标建表。
- 只提炼影响当前决定的一至两个关键原因，并指向正式报告卡片。unknown 只表示未核验，不能称为“未通过”或配置错误；unsupported 才说明拒绝原因或必要修改；supported 也不保证执行成功。不要逐项重列检查范围、未检查项、运行时要求或字段诊断，也不另建“正式预检结果”清单。用户未要求创建时明确未创建、未运行，但不把报告当作当前执行授权。

用户明确要求详细说明时，可以基于同参配置和 owner 报告解释所问部分；已有字段映射或参数的精确展示仍由固定卡片承担，不以模型新表格或代码块替代。短答不删减模型收到的诊断，不静默修改草稿，不把解释当作创建复核，也不自行生成报告卡片。

用户明确要求检查配置或预检，同时要求只给草稿、不创建、不运行时，在完整参数已确定后主动调用 `transfer.task.config.check`；只在参数或业务决定仍缺失时澄清，不反问是否需要用户已经要求的检查。不调用 `transfer_create_review`，不提供创建确认按钮，也不调用 `transfer.task.create`。该只读检查不需要用户批准创建。

依据完整正式报告分析配置，再按上述短答方法解释。unsupported 时解释原因或修正草稿，但不能静默改变用户已确定的字段范围、行粒度或写入策略；修改后重新检查。unknown 不是通过，也不要求用户为只读草稿知情批准创建。预检服务或权限失败明确报告，不当作 unknown、不自动重试。只有没有要求检查的普通草稿，才仅展示草稿且明确尚未正式预检。

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
