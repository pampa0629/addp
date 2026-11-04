---
name: transfer-generation
description: 理解 ADDP Transfer 平台能力，发现并确认源和目标、生成并复核配置、创建停止态任务；用户要求运行时，在 Agent 中展示 Transfer 一次性运行审批，用户手动批准后由 Runtime 启动并查询真实执行。创建不等于运行，受理不等于成功。
---

# Transfer 任务生成与创建

## 独立的任务运行与结果查询

运行与创建是独立操作，条件分别来自 `transfer.task.start` 与 `transfer.task.create` 本体。用户只要求创建、检查或看草稿时，不申请运行审批、不启动。用户明确要求运行已有任务时，使用用户指定或 Owner 已返回的真实 task_id；不按名称猜 ID，不为启动重建任务。创建后需要运行时仍须单独展示运行审批，不能把创建复核当作运行批准。

`transfer.task.start` 首次只提交 task_id。Owner 返回 approval_required 后立即等待用户在当前 Agent 页面手动确认；不调用决定 API、不伪造 approved、不通过普通澄清冒充运行确认，也不要求跳转 Transfer。用户的批准、拒绝及已消费状态都由 Runtime 从正式 Owner 状态恢复，模型不能接管恢复提交或重复启动。

审批过期、配置／身份／语义变化或结果不明时明确解释，不重发启动、不改配置后沿用旧批准。启动成功只记录真实 execution_id；查询状态使用 `transfer.execution.get`，不用 Develop 的 `execution.get`。pending/running 是未完成，success 才是 Owner 记录的执行成功；保留真实计数与失败诊断，尤其是目标可能已变化。执行成功和计数仍不证明业务语义或逐行全量一致，不自动扫描、读样本或直接查询业务库补证，不自动重试任务。

先读取平台能力语义，再复用数据发现方法确认源资源和真实目标父节点。条件、效果和支持边界以 `platform.capability.context` 为唯一语义来源；本 Skill 只说明获取事实、补齐缺口和组织交互的方法。不要求用户已有领域本体、Standard、Model、Quality 或 Catalog 内容。

## 核心流程

用户在当前请求中明确附带唯一完整配置并要求按原参数只预检时，直接使用该原配置，不调用构建器改名、补参数或重新映射。完整 JSON 缺失、多个配置或外层损坏时先澄清，不从内层对象或模型复述恢复配置。Runtime 绑定正式构建步骤、用户原始输入或历史草稿来源，模型不能改写绑定参数；需要修改先取得明确要求并重新构建。

Runtime 已明确绑定原草稿时，只沿用恢复的候选参数，不重建任务名、描述、目标表名、查询、映射或可选参数，不新增 batch_size 等默认值。重新读取当前平台能力及必要资源事实，不把历史参数当作已确认资源或权限。最终 `transfer_create_review` 的 `operation_review` 只提交 `{tool:"transfer.task.create"}`，不提交 arguments；Runtime 使用原参数预检和复核。确认后的正式创建输入同样由 Runtime 绑定，不能借模型输入改写参数。不额外调用 draft.generate 或显式预检来重建原稿。若当前定义不支持原参数，明确解释；需要改配置时结束原参数创建，再按用户要求进入修改和新的复核流程。

1. 理解 Runtime 在本次 Harness 进入时经 `platform.capability.context` 读取并保存的所选能力：创建为 `transfer.task.create`，运行为 `transfer.task.start`，结果查询为 `transfer.execution.get`；无需重复读取。Runtime 按该操作与本体依赖收窄 Skill 的 Tool 白名单，不强制其他操作已发布。需要主动刷新，或进入已声明的可选后续操作时，仍用同一正式 Tool 读取对应定义，失败不能沿用旧结果。理解其概念、条件和效果；`availability=not_observed` 不是可用性或权限证明，不假装其他模块存在。
2. 用 `engine.list` 确认用户可访问的源/目标引擎。按 `data-discovery` 只搜索源侧数据项，必要时限定源 engine_id；多个集合或引擎候选等待用户选择，不能用排序、相似度、单复数或名称更相近来代替确认。Runtime 对新草稿的多源搜索结果会直接暂停并展示正式选项，收到选择后只使用确认的 locator。搜索失败时明确报告并终止当前运行，不能把失败当作零召回，也不能静默改用源目录枚举来继续生成或创建。
3. 通过 `resource.ancestors.get → resource.facts.get` 确认源身份和最新结构，不用样本猜 Schema。缺扫描事实则要求补扫描，不能读取原始行兜底。
4. 用 `resource.children.list(engine_id=<目标>)` 从 owner 返回的真实根节点开始逐层浏览；后续父 locator 只取自返回结果。让用户确认目标 `parent_locator + name`，目标尚不存在时不能把它当成已有输入搜索。不可假定 PostgreSQL schema 是 public。
5. 根据能力定义分别确认行粒度和字段保留范围，再补齐写入策略、目标字段及转换意图。不展开数组不等于删除数组字段。缺少业务决定时澄清，不能静默删字段或改行粒度。复杂计算交给 Develop。
6. MongoDB 到 PostgreSQL 的新 bounded snapshot 草稿必须调用 `transfer.task.draft.build`。提交已确认的 `source_locator`、`target.parent_locator/name/apply_mode`、`row_grain` 和有序 `fields` 选择，字段 path 使用正式结构中的原始名称分段。用户不需要填写查询或配置 JSON。Transfer 自行读取当前事实并构建查询、类型映射和完整参数，后续原样使用返回的 `arguments` 预检或复核，不由模型重新拼装。构建失败按诊断澄清或修正业务选择，不换用 Copilot 或手拼配置绕过；已绑定历史原稿仍沿原参数复核。其他组合仅在当前本体及正式 Tool 明确覆盖时使用对应入口，不把此切片扩张为所有 Transfer 能力。
7. 调用 `request_clarification`：`reason="transfer_create_review"`。未绑定原草稿时提交 `operation_review={"tool":"transfer.task.create","arguments":<完整创建参数>}`；已绑定原草稿时只提交 `operation_review={"tool":"transfer.task.create"}`，原参数由 Runtime 提供。options 提供 value 为 `confirm` 和 `cancel` 的两个选项及用户语言的标签。Runtime 自动通过正式 `transfer.task.config.check` 预检完整参数，再生成配置复核与绑定指纹；不必由模型重复调用预检。unsupported 必须修正配置，不能继续创建；unknown 不是通过，Runtime 展示未检查项及运行时要求，并取得用户知情确认。supported 也不保证执行成功。prompt 用简短业务摘要说明源/目标名称、行粒度、边界/装载方式、字段处理概要、写入策略及其影响，并明确只创建、不运行；不要在摘要中重复 locator、MQL、完整字段清单或 JSON。不能用摘要替代 Runtime 的完整参数和报告复核。只有用户选择 Runtime 绑定的确认选项后才提交同一配置，不能改字段、查询、策略、名称或批大小；文字回答是补充要求，不是创建确认，有修改先重新复核。只要求草稿时不进入此步骤，也不能宣称已经预检通过。
8. 用户选择正式“仅创建、不运行”后，由 Runtime 恢复同一份完整参数，刷新本体、源结构和目标父节点事实，并重新预检；指纹匹配后沿同一 `transfer.task.create` Adapter、确认消费与持久提交屏障创建一次，不再请求模型选择下一步或收尾。模型不得重复复核或重新拼装已确认参数。配置、本体、事实或稳定报告变化后由 Runtime 重新展示同参数复核；条件不满足、依赖失败或创建结果不明终止，不自动重试。成功固定提示“任务已创建，尚未运行”，只消费正式返回的真实 ID、idle/stopped、无计划，不宣称数据已同步，不调用其他执行 Tool；结果不明时核对 Transfer 审计。

## 草稿阶段的只读检查

每次有效正式预检由 Runtime 自动展示固定报告卡片，状态、检查范围和完整诊断直接来自 owner。历史卡片是该次检查快照，不代表当前配置已通过或运行已获授权。

正式预检参数及字段映射同时由 Runtime 固定展示，并绑定草稿引用。用户后续只解释已有草稿时读取该引用的精确参数，不以新获取的源结构或旧回复正文重建原映射；创建任务只保存定义，目标建表或清理写入发生在实际执行阶段，不能混淆。修改配置须沿原事实、澄清、预检与复核流程，旧草稿引用不继承确认或授权。

显式检查返回有效报告后，Runtime 展示固定报告、精确草稿和固定完成提示并结束本轮，不再调用模型收尾。不要同批请求检查、创建或澄清；同批只有首个正式检查会执行。完整参数已确定后只提交一次检查，unknown 不是通过，unsupported 不静默修正，supported 不保证执行成功。后续用户解释已有草稿时读取原草稿引用；模型不重建参数或改写报告，也不把历史解释当作创建复核或运行授权。

用户明确要求检查配置或预检，同时要求只给草稿、不创建、不运行时，在完整参数已确定后主动调用 `transfer.task.config.check`；只在参数或业务决定仍缺失时澄清，不反问是否需要用户已经要求的检查。不调用 `transfer_create_review`，不提供创建确认按钮，也不调用 `transfer.task.create`。该只读检查不需要用户批准创建。

显式检查结束后用户要求修改时，沿原流程重新获取必要事实、澄清、构造及检查，不静默改变已确定的字段范围、行粒度或写入策略。创建流程中的自动预检仍将完整诊断提供给模型，unsupported 必须修正后重新复核，unknown 仍需知情确认。只读草稿不要求用户批准创建；预检服务或权限失败明确报告，不当作 unknown、不自动重试。只有没有要求检查的普通草稿，才仅展示草稿且明确尚未正式预检。

## 草稿构建要求

- 当前 MongoDB → PostgreSQL 切片的查询编码与字段类型规则由 Transfer 构建器持有。LLM 提交字段选择和转换意图，完整配置以正式 Tool 的 `arguments` 为准。默认每个字段只提交 `path` 和 `target`；不复制源 `type` 为 `target_type`，也不主动填写 `nullable`、`precision` 或 `scale`。Transfer 根据事实选择默认类型，保留的数组由 Owner 默认映射为 JSON。只有用户明确要求类型转换或列约束时才提交这些可选覆盖参数；`target_type` 使用 ADDP 标准类型，物理 DDL 类型由 Provider 处理。
- 整体保留对象数组、嵌套数组、混合数组或元素类型未知的数组时，让构建器选择可表达的表示，再交正式预检。源 `element_type` 不是目标可写性证明，unknown 不是通过。不得删除数组字段或擅自改变用户确认的表示以规避诊断。
- 目标父节点用目录和祖先入口确认，尚未创建的目标表没有可供事实读取的虚拟 locator。字段缺失、结构覆盖不完整或多个候选必须明确处理，不用自报 schema 补齐。
- 构建成功仅表示生成了候选参数。只有正式预检返回才展示预检结果，只有创建 Tool 成功才报告任务已创建。

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
