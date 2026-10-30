# Ontology 模块

Ontology 管理租户领域本体的形式化语义，不接管 Standard、Model、Catalog 的事实所有权，也不读取业务数据库。总体边界见 [本体定义与发布规范](../docs/spec/addp本体定义与发布规范.md)。

## 已确认的设计与依赖边界

2026-10-03 明确三层：平台能力语义辅助 Agent 选择功能，租户领域语义说明业务概念与规则，运行时业务事实支撑具体对象判断。当前覆盖原生 Tenant 定义、手工假设性试算、首个平台 Transfer 能力的部署发布、PG 权威版本选择及 FalkorDB 依赖消费，以及平台用户只读核实；Tenant 专业来源引用、本体数据绑定和可信业务事实求值仍未实现。

Ontology 自身 PG、FalkorDB 和 System 当前注册构成 Ready 前置；除 System 外，不假定任何业务模块存在或被用户使用。Meta、Standard、Model、Quality、Catalog、Manager、Develop、Service、Agent 等只在使用其能力的请求中经正式 Client SDK/API 协作，不进入启动/Ready 检查。远端不可达、无权限、没有内容和版本变化按 owner 契约明确表达，不自动换源、不把错误当作无数据。

本体数据绑定目标由 Ontology 唯一管理，固定本体修订与类/属性、读取 owner 来源及输出契约、对象身份、类型化路径、转换语义和绑定版本；Catalog 提供可选企业身份、责任与治理关联，不维护第二份可编辑绑定。Catalog StandardMapping 与 Model 概念实现映射仍属于各自 owner。绑定不持有业务凭据、不执行查询、不代表事实已观察；用户业务数据继续由数据读取 owner 授权访问。首个真实切片为单条 Outdoor 活动的有效性判断，目标契约见[后续设计的绑定、可信求值与准入](../docs/next/ADDP%20Ontology最小设计契约.md)，不能把当前试算接口当作真实求值入口。

## 当前交付范围

平台 Transfer 修订 44 复核 `560caff55`：修复 Agent 配置入口的导航来源及跨 Console iframe 重载丢失文字草稿，复用既有 addp-module 白名单和 4 KiB／60 秒单次 NavigationDraft；不扩大桥接来源、不保存草稿正文或继承原批准、预检、资源事实。仅更新 chat_renderer 来源与当前修订，保留 32 项来源及所有语义／覆盖断言。真实管理员页面已验证配置跳转与文字保留，未保存配置、发送消息或创建运行；不代替新后端偏好的真实模型验收。

平台 Transfer 修订 43 复核 `96fb59007`：Agent 工作区布局、现有配置直达、浏览器 User/Tenant 使用偏好及文字复制／导出／另开会话落地；新 Run 的表达偏好冻结到既有 checkpoint，仅影响模型行文，固定报告、复核、确认、原参数及权限不变。另开会话仅填文字草稿，不继承 Interaction、批准、预检、草稿引用或资源事实。同步十项既有来源；保留所选创建切片的全部 32 项来源、覆盖断言与固定上限。新增表达偏好和文字／资源展示投影由 Agent 模块专项及受控浏览器门禁验证，不作为新平台能力或创建效果。Transfer 创建仍限停止态 native table；无新增创建、执行或样本权限，不把受控门禁记为真实模型／Hosted 验收。

平台 Transfer 修订 42 复核 `4d970f632`：普通工作流引擎/算子查询的既有 Skill 路由边界补足，不以平台目录存在对应定义为前置条件，也不增加目录失败回退；原参数创建、条件、复核、权限和效果不变。共享草稿长名称换行及侧栏按钮样式只修复窄屏展示。四项来源和绑定修订同步；仍区分受控门禁、真实只读会话与 Hosted 验收。

平台 Transfer 修订 41 复核并冻结 `77442db24` 的 Agent 计划投影修复：一次回复使用稳定 Activity 身份，流式与持久消息只替换当前计划，安全事件及历史回复保留原快照。正式草稿、预检、确认、创建权限和业务效果均不变；仅更新三项 Agent 来源、用途和修订绑定，不修改发布／投影内核或新增验收入口。真实会话暴露的重复卡片保留原始证据；修复后的真实复验仍须加载新 Agent 代码，不能用静态冻结冒充运行时激活。

平台 Transfer 修订 40 复核并冻结 `151e6a263` 的持久 Cleanup 围栏错误分类：人工启动／恢复被未核清的外部清理操作拒绝时返回 409、稳定 `transfer_cleanup_unresolved` 和双语安全消息；删除清理失败仍优先返回 503，不暴露底层包装错误。已逐项对照旧 handler：委托创建、租户权限、bounded/snapshot/native table、idle/stopped、无计划／扫描／执行的条件与效果均不变；人工运行与 Cleanup 仍不纳入当前创建能力。本轮只更新 handler 来源身份、摘要、用途和修订绑定，保留全部来源、覆盖与拒绝校验；既有来源审查、Ontology Go T1 及 CI Go／Platform 自动发现覆盖，不新增入口或依赖。未修改发布／投影内核、schema 或前端，不以来源冻结宣称运行时已激活、真实模型或 Hosted 验收通过。

修订 40 验证：非缓存 `make test-platform-review` 先准确复现 handler 漂移，冻结后通过；选用现有 Node 24.11.1 后 `make test-module MODULE=ontology GO_ONLY=1` 通过。默认 PATH 的 Node 25 曾被标准运行时前置检查拒绝，未执行模块测试；未放宽该检查。32 项来源逐一核对当前文件、真实提交字节和锚点全部匹配，只有 handler 来源变化，定义除 revision 外逐字段相同，来源拒绝测试和覆盖绑定未改。未运行完整 T0、PG／FalkorDB T2、前端 T3 或 Hosted T4；本轮未变更其对应运行内核，既有自动发现入口继续覆盖。证据 `/tmp/addp-ontology-r40-review-red.log`、`/tmp/addp-ontology-r40-review-green.log`、`/tmp/addp-ontology-r40-t1-node24.log`、`/tmp/addp-ontology-r40-source-audit.json`。

平台 Transfer 修订 39 复核并冻结 Agent 对话工作区实现 `da8794a89`：归档只控制新请求接纳，计划只展示模型报告的处理进度，成果与运行记录复用现有受限投影。审批状态在会话锁前只读预取，锁内重读 Interaction 并核对当前绑定；原配置预检、最终确认与唯一创建 Tool 路径不变。创建仍只保存未运行任务，不新增执行、扫描、写入或业务授权。该修订仅更新 Agent 来源审核与摘要，不改 Ontology 发布/图投影机制；来源审查、Ontology Go T1 与 Agent 既有门禁覆盖，未作运行时激活或真实模型验收声明。

平台 Transfer 修订 32 复核 `d01c5772f` 的角色专用脱敏接入：Planner 仅增加可选的来源字段选择；没有选择的委托创建仍保存 idle/stopped 的 bounded/snapshot/native table 定义，无 execution、调度、扫描或业务写入。角色方式只允许当前第一方 Tenant User，不能借用委托人或服务身份；新增 Owner 回归确认 Delegated 带角色选择返回 409 且不读取本人角色、不增加定义／执行。手动执行的 EA／当前角色核验与 Develop 文件交付均不属于当前创建能力，覆盖说明显式标记角色专用方式尚未建模。创建 Tool／权限、确认、条件、效果和排除效果不变，不新增角色配置或运行能力。来源冻结到已提交的 Owner 代码／回归与共享本人角色客户端，新修订不是运行时已激活的证明。门禁沿现有 `make test-platform-review`（强制 count=1）、Transfer／Ontology Go T1；发布／投影实现、schema、前端和依赖未变，已有 Ontology PG／FalkorDB 自动发现继续覆盖，无新 Makefile 或 CI 入口。

修订 32 验证：`make test-platform-review` 非缓存执行通过（`-count=1`），`make test-module MODULE=ontology GO_ONLY=1` 和 `make test-module MODULE=transfer GO_ONLY=1` 通过；来源先提交为 `6a188526a` 再冻结，审查同时核对所引提交的真实字节与当前文件。初轮新增 Owner 断言误用不存在的错误码，按既有 `transfer_protection_roles_unavailable` 精确契约修正后完整复验；未改产品错误码或放宽断言。影响矩阵实际选中 ontology、transfer。此次没有改存储／发布运行内核，未重跑 PG／FalkorDB T2、前端 T3 或 Hosted T4，不将来源复核计为正式激活或 CSV 撤销验收。日志 `/tmp/p145-transfer-semantic-{review-final,owner-t1-final,ontology-t1}.log`。

平台 Transfer 修订 31 复核 `c0153c89a` 的原生表执行保护：已纳管源在读取和目标变更前，以同一保护投影核验实时完整字段；未纳管不新增 Catalog 读取。来源文档仅新增该运行阶段说明，当前选定能力仍只创建停止态 native table 定义，不执行、调度或扫描；参数、权限、确认、效果、排除效果及未建模范围均不变。本次同步 config 的已提交来源、审核说明和修订号，未修改发布／投影机制或 schema；静态编译和来源门禁通过不表示修订已在运行时激活。

平台 Transfer 修订 30 复核 `543e2accd` 的动态 encoded reader 写前字段核验：reader 尚无字段时通过同格式 Describe 验证当前源，复用本次已有结果，不跳过字段校验；已有 Meta 声明时增加一次源读取。该执行行为仍在选定创建能力范围之外，停止态 native 创建的参数、权限、确认、效果与排除效果不变。本轮只更新 config 来源冻结，未改变发布／投影实现或 schema；使用来源审查与 Ontology Go T1 验证，不据此声明运行时已激活。

平台 Transfer 修订 28 复核 `699503341` 的 CSV/TSV 表头及字段一致性说明和 `477199916` 的 Planner 表头继承实现，更新 config/planner 来源冻结。有效任务 `header` 优先于 Meta `native.has_header`，encoded 单资源字段冲突在目标 IO 前拒绝并保留 Meta 类型；这些读取行为仍在创建能力选定范围之外。创建能力继续只保存停止态 native table 定义，不执行、调度或扫描，原权限、确认条件、replace/append 约束与 encoded 未建模声明不变。本次静态来源刷新使用平台来源审查与 Ontology Go T1 核验编译和恢复；发布/投影实现及 schema 未变，现有 PostgreSQL/FalkorDB CI 门禁保持覆盖，不据此声明运行时已激活。无新增入口或依赖。

平台 Transfer 修订 25 复核 `7b337b9c4` 的结构审批补扫来源：handler 独立固化当前审批用户，Transfer 发布已应用审批事实，Meta 核对事实、有效领取与精确目标表后创建独立扫描。这里只更新 handler 来源冻结；创建仍只保存停止态定义，条件、权限及选定覆盖范围不变。沿用平台来源审查、Ontology Go 与 PostgreSQL/FalkorDB 门禁，无新增入口或依赖。

平台 Transfer 修订 24 复核 `ebe4ebb99` 的当前用户来源固化与产物扫描归属变更，更新 handler/service 的已提交来源及摘要。人工启动、恢复和重放使用当前用户，创建能力仍只保存停止态定义，不创建执行或扫描；选定覆盖、权限及确认条件不变。沿用平台来源审查、Ontology Go 与 PostgreSQL/FalkorDB 发布门禁，无新增入口或依赖。

平台 Transfer 修订 23 复核 `001b0a4db` 的运行后扫描范围修正：bounded、continuous 和结构变更后的扫描只提交目标表 locator，不扩大到 schema／database。这里只更新两份规范来源冻结；创建能力仍禁止自动扫描，不改变条件、权限、确认或选定覆盖范围。现有平台来源审查、Ontology Go、PG／FalkorDB 发布门禁覆盖修订，无新增入口或依赖。

平台 User 的 Console `/ontology/platform/definitions` 与 `/:capability` 只读核实当前 active 定义，展示概念关系、条件/效果/排除效果、事实入口、选定覆盖范围及逐项冻结依据。目录/详情 API 为 `GET /platform/definitions` 与 `/{capability}`，详情从一次 PG active 读取恢复同一快照的 context/review，不读仓库文件或业务实例。独立 `ontology.platform_definition.read` 仅 Platform Scope、不可委托，System 向前迁移 000203 默认组合进平台系统管理员；不扩张 Tenant 或机器 Runtime。原 Agent 精简消费契约及权限不变。前端直接复用 common-frontend 的 OntologyView 和锁定的 G6 4.8.24，模块仅适配已发布成员；无在线编辑或任意图查询。已有 Go、IAM PostgreSQL、Ontology PostgreSQL、共享前端、Console 和 Ontology 前端标准入口覆盖 API/迁移/鉴权及确定性浏览器；CI 自动发现覆盖同一入口，无新增 Job。个人环境须由用户重启 System/Ontology 才加载权限及 API，不把受控 T3 当作正式 T4。

平台能力目录 `GET /platform/capabilities` / `platform.capabilities.list` 从同一只读 PG 快照选择 active revision/digest/ready generation，再经 FalkorDB 查询同版能力依赖、从冻结内容补全；按 capability 升序，最多 32 项、128 KiB、5 秒。未激活不列出，空目录为成功空数组；损坏、超限或图失败整次拒绝，不截断/跳过/换源。沿用 `ontology.semantic.read` 和精确委托，拒绝 query、不缓存、不证明业务操作权限。Agent 以目录语义选择能力，再核对 Skill/Tool 装配；当前仅 Transfer 创建。平台核实目录复用该路径，核实详情仍从 PG 恢复完整 context/review。门禁沿用 Go、Ontology PG/FalkorDB、Agent eval、授权与 Swagger 标准入口，CI 自动发现，无新增依赖或 Job。真实部署验收待用户重启，详见[本体定义与发布规范的能力消费](../docs/spec/addp本体定义与发布规范.md#八平台能力与图依赖消费)。

平台能力的 PG 内部记录使用独立的 `platform_capabilities`、`platform_revisions`、`platform_revision_events`，迁移 004。严格比较最后修订基线，允许代码部署跳过修订号；完整规范化 TEXT 快照、摘要和平台主体审计同事务存入，修订/审计不可更新或删除。记录本身不是授权或发布入口，不使用 Tenant Actor。迁移 005 增加同步平台发布投影及审计、机器发布 generation fencing 和 active 指针；迁移 006/schema 6 要求新记录使用包含来源证据的 v2 快照，保留旧字节但不支持旧格式消费。测试复用现有 PostgreSQL 门禁及 from 1/2/3/4/5 升级测试，详见[本体定义与发布规范的来源、编译和平台发布](../docs/spec/addp本体定义与发布规范.md#六平台定义来源与编译)。

`internal/platform` 唯一管理代码发布的 Transfer 能力定义，包含概念、关系、前置条件、效果、修订与摘要。2026-10-08 确认先不提供平台管理员在线修订。人工整理的 `transfer.json` 与仅供溯源的 `transfer.review.json` 经唯一 `Compile` 路径严格校验，形成绑定 `addp.platform-definition/v2` 与编译器版本的不可变快照；`Restore` 拒绝损坏、非规范及旧版本内容，不自动升级或退回源文件。Transfer 当前修订 34 的摘要同时绑定定义、来源与覆盖声明；修订 33 复核 KML 的格式固定 CRS 规划与 CRS84 的 XY 等价处理，仅影响未建模的 encoded 执行，不扩展停止态 native 创建条件、权限或效果；修订 31 复核已纳管原生源运行前实时结构保护且不扩大停止态创建能力；修订 30 复核动态 encoded reader 的写前 Describe 字段核验，仍不将 encoded 执行纳入创建能力；修订 29 复核组织范围功能角色退役后的认证拒绝测试。业务创建条件与效果不变。每个成员均须关联依据，来源 owner/类别/路径/锚点/提交/文件 SHA-256 固定，标准 T1 检测漂移。修订 6 捕获创建 Tool v3 的标准目标类型与快照 replace/append 约束，并引入 Transfer 正式支持矩阵和 Planner 依据；删除快照 upsert 及 keys 的错误路径。输入枚举只由 Manifest 声明，不在 Ontology 复制；配置通过不证明实际值转换或执行成功。覆盖只声明当前创建切片及选定未建模范围，不宣称全模块完整。来源和覆盖保存在同一个 PG payload；FalkorDB 确定性投影概念及能力、条件、入口组、Tool、效果依赖，完整定义仍以 PG 为权威。Agent 上下文 v1 不携带来源全文。快照不是平台发布授权、PG published 或图 ready 凭证。`platform.capability.context` 通过 `GET /platform/capabilities/transfer.task.create` 返回 `knowledge_kind=platform_definition`；不伪造 Tenant 本体或运行可用性，不查询业务数据。现有 `ontology.semantic.read` 与精确 Tool scope 控制读取。定义中的 Tool/Skill 引用由 Manifest 和根 Skill 校验，不复制权限、HTTP 地址或凭据。

`skills/transfer-generation` 先消费平台语义，再经事实 owner 确认引擎、源结构与目标父节点，信息不足要求澄清。新增 `transfer.task.create` 仅创建 bounded/snapshot/native table、无计划、未启用且不自动扫描的任务定义，返回 idle/stopped；不能运行任务。System 迁移 181 只开放已有创建权限的委托标志，不给角色增加权限。确定性 Agent 场景 `platform-transfer-create`、Common Python HTTP 契约、Transfer owner 拒绝与零 execution 断言沿现有标准门禁执行；真实用户/LLM 对话验收待新服务加载后进行。

`backend/internal/semantic` 是正式 Go 语义内核；`internal/service`、`internal/repository` 管理原生定义的 PG 修订生命周期。`cmd/server` 已装配统一 Lifecycle、管理 HTTP API 和投影 Supervisor；FalkorDB 是私有 Infra。`frontend` 提供 Console 原生定义建模与修订管理页面；两个只读 Agent Tool 提供激活定义消费，不影响 Graph 的当前功能。2026-09-18 已在用户启动的个人开发环境完成真实用户的建模、发布激活和 Agent 定义读取联调；不把本地手工联调或 T1/T2/T3 当作正式隔离 T4 上线验收。

只读消费使用 `GET /ontologies/{ontology_id}/semantic/classes` 和 `GET /ontologies/{ontology_id}/semantic/classes/{class_id}`。后者必须绑定目录返回的 revision、generation、activation_version，激活变化返回 409；仅从同一 PG 只读快照核对过的 published/ready 定义读取，不读取草稿或实例。返回 `knowledge_kind=native_definition`，结果超过 128 KiB 直接拒绝，不截断。独立的 Tenant `ontology.semantic.read` 可委托给精确 Tool scope，管理权限仍不可委托。迁移 154 只登记权限，不给既有角色扩权。根目录 `skills/ontology-exploration` 组合目录和上下文工具，要求身份澄清、版本一致和定义/事实区分。

新增消费链路沿用 `make test-module MODULE=ontology` 的 T1/T2、`make test-agent-eval`（含 Common Python）、`make test-copilot` 与 System IAM `--package migration --test ontology-backend` 标准门禁。Skill/eval/SDK 路径由现有共享影响矩阵和 CI 自动发现；无新增外部依赖或 CI Job。本地真实身份联调已通过，正式 T4 仍未通过。

`internal/falkor` 提供唯一的 go-redis 薄适配及确定性图投影构建/全量校验；投影执行器连接首次发布/失败重建准入、Common 租约、构建前/激活前 System 授权消费和 PG 激活指针。Backend 绑定 HTTP 后异步注册，Ready 同时验证 PG、FalkorDB 非零查询预算及 System 注册，只有 Ready 才领取；退出等待在途执行与注销。

- 自有类、属性、继承和关系声明的严格校验；拒绝环、悬空引用、继承属性歧义及不一致逆关系。
- 确定修订的不可变内存快照；规范化 JSON 和 SHA-256 摘要包含租户、本体、修订、内核契约与编译器版本。它不是数据库发布成功的凭证。
- 受限 CEL 分类：布尔/字符串、等值、逻辑运算、字符串字面量有限集合成员判断；禁止宏、算术、动态类型、时间、正则和任意函数。
- 有限枚举同时校验输入值和直接比较/集合条件中的字面量，拼错的码值不能发布为永远不匹配的规则。未知状态应传 `unknown`，不将不认识的已知码值自动视为未知或 false。
- 已知、已确认缺失、未知、无效四种输入；输出 matched、not_matched、unknown、error。只有输入声明明确允许的布尔属性，才将已确认缺失映射为 false；未读取不适用该策略。
- 解释保留规则、输入状态与缺口，但仅为 `hypothetical` 试算，不认证业务事实。原始事实值不回显。内部错误不是 HTTP 错误格式，由试算 API owner 翻译及授权过滤。

Console `/ontology/ontologies/:ontology_id/trial` 提供当前激活规则试算，复用唯一 CEL 内核与共享参数输入组件。`POST /ontologies/{ontology_id}/semantic/rules/{rule_id}/trial` 仅允许 Tenant User 的 `ontology.semantic.read`，不对 Agent/Delegated 开放。手工输入四态及已知值，必须绑定 revision/generation/activation_version；执行前后检查 active，版本变化返回 409。输入最多 16 项、请求 96 KiB；不读取业务库、不持久化、不回显原始值。编辑输入会取消在途请求并清除结果，冲突保留输入且要求重新加载。现有 Ontology T1/T2/T3 自动发现覆盖 DTO/权限拒绝、四态求值、租户/版本隔离、撤回拒绝、页面与迟到响应；无新增依赖或 CI Job，未将此切片宣称为真实业务事实求值或正式 T4 验收。

关系声明目前只校验，不执行传递/逆关系的实例推理。仅支持原生定义；未来来源引用须遵守 owner 快照契约，不能加可编辑来源副本。

试算页不再把令牌轮换时短暂的 AuthContext 空值当作重载定义：重取期间隐藏内容、取消在途试算并清除结果，同一 User/Tenant/Membership 且语义读取权限仍有效时保留内存输入；身份变化、退出、失权或重取失败则清空。继续使用共享认证实现，页面不解析 Token、不保存凭据、不自动重发试算，也不解除已有版本冲突锁。T3 使用真实共享 Auth Store 与受控轮换令牌夹具验证刷新保留、身份隔离、失权/失败清空和迟到响应；沿现有前端与模块 CI 自动发现执行。

平台 Transfer 修订 7 在已有概念关系中显式区分 `reads_via`（条件到读取入口组）、`configured_by`（配置）与 `confirmed_by`（用户确认）。编译校验条件和入口身份；图从 PG 选定 generation 查询固定三跳“能力 → 条件 → 入口组 → Tool”，全量比较后补全同版上下文的取证关系。入口不是充分事实或执行计划；搜索候选不能代替正式结构，草稿辅助不充当事实来源，Runtime 条件裁决和确认指纹不变。复用上下文 v1、快照 v2 和现有前端图，无新增迁移、接口或 CI Job，详见[本体定义与发布规范的条件取证路径](../docs/spec/addp本体定义与发布规范.md#条件取证路径)。部署新修订的真实消费须重启后另验，不把 T1/T2/T3 当作正式 T4。

修订 13 审核并冻结 Agent 对正式预检拒绝的反馈改进：unsupported 仍阻断复核和创建，但 owner 的范围及字段诊断进入当前模型 ToolMessage，修正草稿后重新预检与复核；服务故障仍终止。仅更新来源依据及发布修订，不改变操作条件、效果、图结构或 Provider 支持范围，不将 MongoDB 查询输出的纯检查声明为已实现。

修订 14 复核并冻结已提交的 `a049fc71d` 配置预检说明与 Handler 双语注释：可选 Provider 纯查询定义分析只消费已保存结构快照，不执行查询；源输出仍未知时继续校验显式目标约束及常量，unsupported 优先于 unknown。完整覆盖、实时结构、业务数据、Security、源读取 Grant 和逐值转换仍须运行时验证。创建切片的条件、效果、权限与选定覆盖不扩张；只有配置与 Handler 两项来源的依据、提交和摘要改变，形成新的不可变发布修订，不覆盖旧快照。该来源审阅不代替真实查询、写入或部署后平台语义消费验收。

修订 15 明确行粒度与字段保留范围分开确认，不展开数组不授权删除数组字段；草稿明确要求检查时使用已有正式只读预检入口，不进入创建复核或批准创建。Skill、Runtime 指导及受控 Harness 回归冻结到 `ac57feb3f`，不新增操作条件、效果、权限或 Provider 规则。这是语义与交互指导，不声称自然语言意图被确定性解析；部署后的自由模型复验需重启 Agent/Ontology，仍只检查草稿、不创建、不运行。

## PG 修订与发布

修订 19 冻结 `ee6450e37` 的固定草稿概要、消息顺序修复及 Skill 方法收敛，补充共享 Renderer 和前后端消息回归的来源依据。概要直接读取已有参数，不在 PG 或 FalkorDB 存第二份运行摘要；两库的权威／投影职责和平台条件、效果、权限、Provider 支持范围不变。实时、持久消息和安全回放按事件顺序展示，完整文字不截断或过滤；Skill 只指导关键解释。完整 `make test-module MODULE=agent`、69 项前端单测、7 项受控浏览器测试、构建、179 项共享前端测试、来源审查及 PG/FalkorDB 联动门禁通过；工作区 `make test-changed` 因其他 owner 的 T2 连接配置缺失而阻断，不计为通过。恢复旧前端排序时浏览器回归按预期失败，恢复修复后通过。FalkorDB 门禁的模拟退出测试首次出现 10 秒超时，未改脚本或放宽阈值，原配置两次复跑通过；不据此宣称偶发超时根因已修复。会话 29 的旧卡片在前端热更新后显示新概要，原参数及引用未变，但不重排历史 parts。受控验证不证明自由模型简洁性，真实新 Run 消费仍待用户重启 Agent/Ontology 后复验。

修订 18 将草稿引用、路由、协议持久化与隔离／回放回归冻结到 `1c81c635c`，并复核同提交的 Skill 和条件适配器来源。正式预检后的精确配置留在 Agent 原步骤输入和同参报告中，固定展示并用 AgentDraftRef 支持新 Run 的纯历史解释；不将运行草稿、主体或报告写入平台静态本体图，也不增加平台操作条件、效果、权限或 Provider 支持范围。历史解释不开放业务 Tool，不继承旧创建确认，修改／创建保持正式平台语义路径。T1／T3 受控门禁不代替重启后的真实模型复验。

2026-10-13 新会话 30 的真实 Run 消费平台修订 19，能力摘要为 `5b0e8e17df69f798409f9ed8f56a52a82461be085f178da78708b8fe984752b4`。一次正式预检生成 unknown 报告与 16 条映射，后续纯历史解释只读取同参草稿引用；消息、回放及刷新均保持已到卡片先于后续模型解释，原参数未改变，无创建或业务数据操作。此本地复验通过平台语义消费、固定事实与消息顺序，不能替代 Hosted T4 或证明配置可运行；模型仍重复概要/检查清单，默认关键解释未通过，完整情况见 Agent 模块说明。运行参数和报告没有纳入静态平台图，也未改变 PG 权威／FalkorDB 同版依赖投影职责。

2026-10-13 重启后的会话 29 首轮消费修订 18，正式预检一次并展示 16 条精确映射和 unknown 报告。后续历史解释仅执行 Agent Runtime `draft.inspect`，不读取平台能力或业务资源；引用绑定原预检步骤及完整参数摘要，不复制创建确认。两轮的参数、映射、报告投影与已保存原证据一致，消息、安全事件及刷新恢复一致；无创建、执行、样本读取或扫描 Tool。本地真实联调仅验证该草稿／解释切片，不代替 Hosted T4，也不证明 unknown 配置可运行。首轮仍重复报告范围和字段清单，默认短答尚未收敛，不能把事实准确性通过等同于完整交互验收通过。

修订 17 将 Skill、Runtime 指导及预检 Harness 回归冻结到 `88329610a`：报告后的默认短答方法由 Skill 单点维护，Runtime 不再要求重复逐项解释范围；完整诊断仍供模型分析和修正，不增加前端过滤、参数规则、条件、效果或权限。受控门禁验证生产 Skill 组合的注入和三态诊断保留。2026-10-12 重启后的会话 28 两轮均消费修订 17，固定报告与 Owner 一致且无创建、执行或样本读取调用，但自由模型短答未达标：仍重复范围，并对任务保存与目标建表作歧义描述。详解作为新 Run 仅带入正文历史，未消费上一轮精确草稿引用；实际数组目标类型为 json，模型转述为 json/array。不能把这次联调计为回复收敛通过；草稿事实的稳定引用与确定性展示需先讨论职责契约再实现，不继续仅靠提示词修补，也不把运行草稿写入静态本体。

修订 16 将 Skill、Runtime 和预检 Harness 回归的三项来源冻结到 `a892f4d01`：有效预检由 Runtime 生成固定只读报告投影，模型只解释诊断及限制，服务失败不产生 unknown 卡片。平台概念、条件、效果、权限和能力覆盖不变；只更新来源证据形成新不可变修订，不将报告快照写入静态本体。2026-10-12 用户重启后，会话 27 的简短输入消费修订 16，正式预检一次并展示一张 unknown 报告卡片（6 项已检查、11 项未检查、6 项运行时要求、35 条诊断）；六个投影字段与 checkpoint 中 Owner 报告逐项相等，刷新后仍恢复。审计无创建复核、创建、执行、样本读取或扫描调用。本地真实联调不替代 Hosted T4，也不证明配置可运行；模型仍重复转述了较多报告内容，简洁性尚待收敛。

平台 Transfer 修订 11 将正式只读预检纳入创建条件，图通过 `readonly_config_check → config_precheck → transfer.task.config.check` 暴露读取入口。API、共享 SDK 与 Tool 的唯一输入及三权限交集不变，Runtime 在最终复核和创建前通过正式 Adapter 自动预检：unsupported 阻止创建，unknown 明确限制后知情确认，仅保存未运行定义；supported 也不保证执行成功。确认绑定完整参数、能力版本、结构及稳定报告事实；仅顶层报告时间及含时间的 binding_hash 不进入确认指纹，结构扫描时间仍绑定。报告保存在运行 checkpoint，不写入平台静态本体，不作为授权凭据。创建前失败清除旧报告且不能自动重试或创建；部署后的真实消费须重启 Agent/Ontology 另验。修订 12 仅将其中 7 项来源的 Git 引用冻结到实际包含已审查内容的 `c1f993b1fa7c5d5e403e463c371c46b2adf4ae80`，文件摘要及操作语义不变；来源引用改变仍形成新不可变快照，不重写修订 11，也不宣称其他 owner 的全量改造已验收。

- `CreateDraft` 只接受同一本体的下一修订号；同一时刻最多一个 draft / in_review。
- `SaveDraft` 和 `Transition` 必须携带精确 version，状态沿 draft → in_review → published → withdrawn，允许审核退回草稿。错误版本不自动重试。
- `semantic.Restore` 严格核对规范化内容、摘要、内核契约和编译器版本；读取异常快照失败关闭，不隐式升级历史定义。
- 发布在同一事务内冻结修订、写审计、创建 `common.task_executions` 的 pending 构建意图，绑定修订、摘要和 generation；审计失败时全部回滚。数据库触发器保护审核/发布内容和追加式审计。
- 发布同时保存 pending 投影及激活基线。构建通过全量校验后，ready、active revision/generation、激活审计与 execution success 同事务提交；并发完成只允许基线匹配者激活。每次切换或清空指针都递增 activation_version，阻止空指针 ABA。
- 撤回与 pending 构建意图取消、当前激活指针清空同事务；不伪装取消已运行工作，也不回退旧版本。执行器在激活前再次核实撤回、授权、当前 attempt/token/租约及基线，不把 published 当作可运行。
- 单槽 Supervisor 仅在显式 Ready 时领取，使用 Common 队列、30 秒租约、5 秒续租和 25 秒执行预算。过期租约收敛失败，不重放结果不确定的图写入；失败保留原激活版本，不删除部分图。历史图清理尚未实现。
- `RebuildProjection` 必须显式指定仍为 published 的修订 version、failed generation 和当前 activation_version；旧 execution 必须终结且无租约。新 generation、新 execution、前驱关联及请求审计原子创建，同一失败批次至多一个后继，同一修订至多一个 pending/building 投影。重复/过期请求拒绝，不回放旧写入、不改原定义、不复用旧授权。ready 投影的主动替换不在当前范围。
- 修订的 generation/build_execution_id 是首次发布的不可变历史身份，不是当前投影指针。首次构建和重建共用 owner 投影记录、准入及执行路径；撤回会取消该修订尚未领取的后继投影，并阻断所有在途投影激活。
- Actor 只承载调用方已核实的租户/主体/成员/授权版本。创建投影先保存无授权的 pending 意图，唯一准入方法 `AdmitProjection` 显式接收 generation，用当前请求的 User Token 调用 System，核对该 execution 的请求主体及全部范围后原子附加授权；领取必须要求授权。重建可由当前已获发布权限的用户请求，不借用原发布者身份。签发或附加失败只关闭目标 generation 仍未授权的 pending 意图，撤回优先于迟到授权。不保存或交给 Worker 用户 Token，不得把内部服务直接暴露为未鉴权 API。
- `repository.Migrate` 使用 `common/schema.Migrate` 协调不可变的 `001_revisions.sql`、`002_projection_runtime.sql`、`003_projection_rebuild.sql`、`004_platform_revisions.sql`、`005_platform_publication.sql` 与向前的 `006_platform_review.sql`，重复同版本不执行。历史快照不重写、不自动解释为新格式；必须部署新修订激活后才可消费。生产 Ontology 只用 `schema.Require` 检查 common，不创建共享执行表。

## 代码与测试

### FalkorDB 适配边界

- `scripts/infra/falkordb.yml` 是 Infra Compose 与 T2 共用的数据库定义。常驻服务使用独立密码与卷，仅开放回环 `16479`；T2 使用随机密码/端口及退出必删的独占卷，并覆盖图快照在容器重建后的恢复。数据库启动就绪不等于本体投影 ready。

- 每条命令使用独占连接、无自动重试、RESP2 标量结果；取消关闭本请求连接并拒绝迟到结果，不承诺服务端立即硬中止。最多 4 条本地在途命令，满额立即拒绝。
- 查询必须携带 1–2000 ms 服务端预算，且不超过调用方剩余 deadline。服务端必须同时启用非零 `TIMEOUT_MAX/TIMEOUT_DEFAULT`；写超时回滚可能额外耗时。取消/网络错误后的写入结果不确定，禁止原地自动重放。
- `Plan` / `PlanPlatform` 只从已校验的不可变快照和 owner generation 派生物理 graph key。Tenant 图保存类型、属性、关系、规则身份/名称及结构引用；平台图另保存能力的 owner/Skill/Tool、条件、入口组及效果依赖。入口组到 Tool 是声明关联，不表示每项都必须调用；不猜测条件与事实对应关系。CEL、规则依据全文与业务事实不复制。传递/逆关系实例推理未实现。
- `Build` 原子保留空 generation，拒绝覆盖已有图；分阶段写入后由 `Verify` 比较摘要、完整成员/边集合及总数。失败可能留下部分图，不能据此激活；清理必须由后续 PG owner 核实引用与 lease 后协调，不在取消路径直接删除仍可能写入的图。
- 适配不是授权器。执行器在构建前核实授权和租约，在激活前再次核实取消、撤回、授权、lease/fencing 和激活基线。网络授权调用不能置于 PG 行锁事务中；owner 统一按本体头、修订、投影、execution 加锁，最后终态写入再次核实数据库时间。当前不提供任意 Cypher API，也没有跳过准入直接图写入的旁路。

System 的 `ontology` 内部执行授权固定绑定 semantic_projection、修订、摘要与 generation；`addp-ontology` 的 Tenant Runtime 只有 `system.execution_authorization.execute`，Platform Runtime 只有自身模块注册及固定平台定义发布权限。执行授权消费接口验证当前 running attempt/token 和 User 授权版本，不能消费引擎连接。平台定义发布不借用 Tenant 执行授权。System 启动需要独立 `ONTOLOGY_SERVICE_CLIENT_SECRET`。

### 管理 API 与部署

- 唯一前缀 `/api/v1/ontology`，Backend 端口 `8195`，Swagger `/swagger/index.html`。路由和 DTO 详见 `backend/docs/swagger.json` 及[本体定义与发布规范的管理入口](../docs/spec/addp本体定义与发布规范.md#四管理入口与建模页面)。
- `ontology.revision.read` 读取本体头、确定修订、确定投影；`ontology.revision.update` 创建/保存/提交/退回；`ontology.revision.publish` 发布/撤回，发布与重建另需 `system.execution_authorization.create`。全部由 Tenant 自定义角色显式分配，不自动扩张既有角色。
- Tenant 管理 API 只接受当前 Tenant 的 User Token。拒绝 Service、Delegated、资源票据以及非 Tenant 作用域的 Permission 候选；身份来自 AuthContext，请求体不能提交 scope、tenant_id 或 Actor。平台核实 API 独立要求 Platform User 和 Platform Scope 的读取权限，不开放 Tenant 管理操作。
- 发布/重建成功为 202，仅代表已准入。准入失败为 502 `projection_admission_failed`，响应 `intent` 保留已提交 revision/version/generation/execution_id，不能盲重发发布。重新读取确认后显式重建 failed generation；进程中断留下的未准入 pending 不自动补签，可撤回修订。
- `revision.initial_generation/initial_execution_id` 是首次发布身份；当前激活事实只从本体头读取，具体投影从 generation 读取。不回显执行授权、租约 token、图 key 或底层错误。
- 修订下 `GET /projection` 沿重建前驱链读取最新尝试，响应丢失时可找回身份；它不表示 active，没有投影返回 404，不依赖墙钟排序。
- 管理浏览使用 `GET /ontologies` 与 `GET /ontologies/{ontology_id}/revisions`，分别固定按本体 ID 升序和修订号降序。仅接受 page/page_size（默认 1/20，最多 100），拒绝重复、未知、非规范参数及 OFFSET 溢出；沿用 Tenant User 的 `ontology.revision.read`。同一只读 PG 快照内计数和取页，列表只给本体头/修订摘要，不加载定义 payload，不把 published 当作 active。详情仍由确定修订接口提供。T1 覆盖分页与路由拒绝，T2 的 `TestPostgresRevisionLifecycle/management_lists` 覆盖租户隔离、顺序、空页、首次发布身份和取消；现有模块/CI 入口直接覆盖，无新增数据库、迁移或 Job。
- `INFRA_FALKORDB_ADDRESS` 宿主默认 `127.0.0.1:16479`，容器为 `falkordb:6379`；只读取独立 `INFRA_FALKORDB_PASSWORD`。构建、Compose、`start.sh -ontology`、`restart.sh -ontology` 已登记；前端开发端口 `5192`、Docker `8123:80`，Console 数据治理组按上下文展示 Tenant `/ontology/ontologies` 或 Platform `/ontology/platform/definitions`。
- 若本机已有忽略提交的 `go.work`，使用 `go work use ./ontology/backend` 纳入模块后再生成 Swagger/开发构建；T1 同时校验生成文档中的原生定义字段，拒绝只保留路由但请求类型变成空对象的假覆盖。
- System 向前迁移 `000153` 登记 read/update 与 Platform Runtime，`000201` 仅为 `platform.ontology_runtime` 增加 `ontology.platform_definition.publish`；既有角色绑定触发器推进授权版本，不修改已发布迁移。Ontology owner 使用 schema v6。
- 平台机器发布核验复用 Common System Client SDK，只接受已编译能力修订摘要；System 固定 `addp-ontology` Platform Service 凭据和发布权限，不接受 User/Tenant/委托凭据，不缓存授权观察。启动发布器已装配该 Authorizer，管理员无发布权限。详见[本体定义与发布规范的平台机器发布](../docs/spec/addp本体定义与发布规范.md#七平台记录机器发布与激活)。
- `PlatformPublisher.Publish` 是最多 45 秒的内部同步机器入口：记录前、构图前、激活前均重新核验，同身份修订仅复用精确字节/摘要。每次使用新 generation；新尝试取代旧 building fence，旧尝试不能迟到激活；只激活最后记录修订，阻止旧部署回滚。复用唯一 Falkor Build/Verify，平台图使用 `ontology:p:`，包含概念、多语言名称及显式关系；完整操作契约仍在 PG。ready、active 和审计同事务提交，失败保留旧版本，取消不后台补写；没有 Tenant execution、自动重放或取消路径删图。PG `platform_publication` 子测试与 Falkor 门禁的 `platform_publication_real_graph` 自动覆盖，详见 1.10。
- 启动先绑定 HTTP 并发起 System 注册，独立且退出时等待的 `PublishOnReady` 编译部署包、等待既有 Ready 后只发布一次；失败只记安全日志，不阻断 Tenant 功能，不自动重放。`GET /platform/capabilities/{capability}` 由 PG active 选择确定版本，再全量核对投影并查询一跳操作依赖与两跳入口组/Tool，按同版冻结定义补全文本和顺序；图调用不持有 PG 事务，总读取最多 5 秒。没有记录 404，尚未激活 409，损坏或图失败 500。响应字段及 32 KiB 限制不变；不缓存、不回退源 JSON 或 PG 图遍历。投影结构变化通过同修订新 generation 发布，不覆盖旧图或增加兼容分支。当前不是跨能力检索或自动规划，真实部署消费待加载新服务后验证，详见 1.11、5.1.2。

### 标准门禁

2026-10-14 本地会话 32 实际消费平台修订 21。草稿首轮正式预检一次；原参数创建按同用户/租户/会话引用恢复，创建阶段四次预检的精确参数均与原稿一致，未将运行参数或报告写入本体图。当前事实缺失先拒绝，补齐后正式复核；知情确认后模型又发起同身份复核，Agent 清除本 Run 确认并重新等待，零创建，真实创建切片未通过。该证据定位于 Agent 确认恢复调度职责，不表明本体读取或 Provider 故障；不新增本体修订或绕过确认，后续职责调整须先确认。包含依赖修正的 Platform CI `38393346356` 与 Release/T2 `38393346397` 已通过，不据此宣称对话或执行 T4 通过。

2026-10-13 CI 依赖纠正：之前 `11b2a7a26` 按包含未提交 Common 授权事实源码的工作区执行 Ontology `tidy`，错误地为已提交的 `main` 增加 9 项未使用的间接依赖和 129 条校验记录。`38379543401` 的 Go workspace Job 在 Ontology `tidy -diff` 失败；已在 `65de8a829` 已提交源码的受控快照复现并收敛，标准 `make test-go` 全部 22 个模块及依赖规约检查通过。Ontology 本身不因此增加 MinIO 或业务读取能力；本体修订、来源冻结、PG/FalkorDB 内容、接口和权限均未改变。受控快照只包含已提交源码；Common 授权事实 owner 后续提交其源码时须同次同步真实消费方的依赖，不能把工作区图与 CI 图混记。此次未重启、不执行业务操作，未重跑数据库 T2 或真实对话 T4；使用现有 Go workspace 和 Ontology T2 CI，无新增门禁或旁路。

平台 Transfer 修订 21 冻结 `1c07a4282` 的原参数创建职责：Agent 按身份恢复确切草稿输入，Runtime 绑定复核和正式 Adapter 参数，每次确认恢复重新读取；当前能力、资源事实、两次预检、知情确认和提交屏障仍独立核验。运行草稿和报告不写入静态本体，PG 权威与 FalkorDB 同版依赖投影职责不变；无新表、API、权限或 Provider 支持扩张。来源增加路由状态和检查点控制引用依据，原能力范围不变。另审查 `f6da6539d` 的 Parquet item-layout 读取说明，其属于 native-table 创建切片之外，仅更新规范来源冻结。

2026-10-13 本轮 Agent eval、Agent 前端（69 项单测、7 项受控浏览器及构建）、平台来源审查、Go 依赖规约、Ontology PG 及独占 FalkorDB 联动门禁通过。PG/FalkorDB 包含本修订记录、真实图投影、发布激活和 owner 自动清理。Ontology `go.mod/go.sum` 同步 Common 已引入的 MinIO 间接依赖，版本符合规约，原 CI 自动发现覆盖，无新增 Job。完整 Agent 门禁首次因 Meta 权限常量未定义失败；生成后串行复跑越过编译，最终在 System/Meta 新 authorization-facts 接口的 Swagger 路由覆盖不一致处失败，后续 owner 阶段未执行。Ontology 整套尝试在公共 Spark 启动夹具 15 秒超时处失败，后续串行 Agent 公共检查同项通过；未改阈值或宣称偶发原因已修复，不能计整套 Ontology 通过。工作区 changed 门禁因其他 owner 的 T2 连接配置缺失执行前阻断。修订 21 真实部署创建验收与 Hosted T4 未运行，须公共 Swagger 登记完成并重跑后，由用户重启 Agent/Ontology 再验；本轮没有创建目标表、运行 Transfer 或写入 Outdoor 数据。

平台 Transfer 修订 20 冻结 `a9b35fd89` 的显式检查终态与双语固定提示，以及生产 Harness 回归。显式有效三态报告展示后结束，不再调用模型；同批仅保留首个检查，避免其他工具副作用。输入错误可修参，失败与取消不生成完成提示。自动创建预检、诊断修正、创建确认和权限保持原路径。PG 仍拥有不可变定义与来源，FalkorDB 仍投影同版依赖；运行草稿、报告和解释不进入静态图。本轮不新增 API、表、权限、组件、依赖或 CI Job，现有 Agent eval/前端、平台来源审查及 Ontology PG/FalkorDB 自动发现覆盖；真实消费需重启后复验。

2026-10-13 修订 20 的 `make test-module MODULE=agent` 完整通过；专项 `test-agent-eval`、`test-agent-frontend`（69 项单测、7 项浏览器及构建）、`test-platform-review`、`test-ontology-postgres` 和 `test-ontology-falkor` 通过。PG/FalkorDB 包含修订记录、真实投影与发布激活，并按 owner 门禁清理自有测试资源。新增终态回归先在旧实现失败，再在修复后通过；覆盖显式三态、同批隔离、输入修正、双语提示、失败及取消，原自动预检回归保持通过。工作区 `make test-changed` 扩散到其他 owner，因缺测试连接配置在执行前阻断，不计为通过；不接管其他 owner 环境。真实模型新会话与 Hosted T4 未运行，受控门禁不证明配置可运行。本轮不创建、运行或写入任何 Outdoor 业务数据。

2026-10-13 用户重启后的会话 31 已真实消费修订 20，正式预检后固定卡片及 Runtime 完成提示正确展示；unknown 仍明确不是通过。历史解释只读原草稿引用，参数、映射、报告及持久消息/安全事件顺序一致，无创建或业务数据操作。刷新页面时 Agent 前端因终端输入 `read EIO` 退出，Console 模块入口 500，刷新恢复未通过，待用户重启后补验；不因此重建平台定义、投影或退回旧版本，也不把本地切片通过当作 Hosted T4 或可执行性结论。

同日用户再次重启 Agent 后，原会话 31 的刷新恢复补验通过：原参数、摘要引用和两轮固定卡片顺序与持久审计及安全事件一致，刷新前后的限定数据库证据逐字未变，没有新增 Run、预检或任务定义。本地首轮/历史解释/刷新切片完成，不重建或重新发布本体、不更新平台修订；前端 EIO 根因修复、配置实际执行与 Hosted T4 均不在此通过范围。

同日会话 31 的“按刚才草稿创建、不运行”切片未通过：当前能力/资源取证及自动 unknown 复核已执行，模型却改写任务名称/描述、补 batch_size，并在要求恢复时混淆任务名与目标表名。Agent 目前只在纯历史解释中读取精确草稿，创建路线仅消费文字摘要而重新生成参数；这是历史参数消费缺口，尚无证据表明平台本体读取或 Provider 预检故障。未确认或创建任务，未扩张本体条件、权限或执行支持。草稿作为候选参数、与授权重新验证分离的职责调整待确认，不通过静态本体复制运行草稿来处理此缺口。

`make test-platform-review` 是不依赖外部服务的平台来源漂移门禁，已纳入 `test-platform`，因此其他 owner 的标准模块门禁同样检查已捕获文件 SHA-256 与锚点，并读取来源固定 Git 提交核对同一文件内容；缺失提交或摘要/锚点不符均拒绝，不联网补取或跳过。先提交来源代码，再冻结实际提交与摘要；旧提交配工作区新摘要不能通过。Git 只属于开发/CI 门禁，不进入生产编译、发布或消费路径。现有相关 CI checkout 均提供完整历史，原 Go T1 自动发现临时 Git 夹具对错误提交、文件缺失及伪造摘要的拒绝测试。Go T1 另核验完整成员绑定、覆盖状态、摘要绑定、来源隔离和拒绝旧格式；来源变化须审查后更新依据及新发布修订，不能盲目刷新摘要。现有 Go/PG/Falkor CI 自动发现覆盖新增测试，未增加运行依赖或另起 CI 路线。

平台 Transfer 切片另用 `ADDP_SYSTEM_POSTGRES_TEST_DSN=<已核实端口的 addp_iam_test PostgreSQL URL> bash scripts/test/system-iam-postgres-gate.sh --package migration --test transfer-task-create` 精确验证迁移 181；默认 System IAM 门禁仍由 `AgainstPostgres$` 自动发现此测试，不将精确选择器视为全量通过。`make test-system-iam-runner` 验证选择器、默认发现和跨进程测试库互斥。

前端复用共享认证、主题、国际化、导航和离页保护；类型/属性/关系/规则编辑同一原生 definition，服务端独占校验与 CEL 编译。页面提供列表、修订历史、创建/保存/审核/发布/撤回及失败投影重建。精确 version 冲突或结果不确定时锁定后续写入且不覆盖编辑，显式重新加载后才解除；发布受理与当前 active 指针分开显示。原生成员移除只修改本地草稿，不级联删除引用，保存时由后端拒绝悬空引用。

`make test-ontology-frontend` 执行 T1 状态/请求契约、T3 当轮独立回环端口的 Playwright 受控 API 场景以及生产构建，不调用个人 Gateway。CI 的 Platform frontend matrix 已登记同一入口并安装 Chromium；模块门禁自动发现新前端。独立登录复用 AuthLoginFlow，支持 MFA 和上下文选择；平台上下文不提供建模权限。

后端模块：`github.com/addp/ontology`，Go 1.24.2。规则上限是版本化内核契约的一部分，不通过用户选项关闭；修改上限或表达式语义须升级契约版本。

v1 边界：64 类、256 属性、128 关系、64 规则；单类 8 个直接父类、继承深度 32；单规则 16 个输入、4096 字节表达式、256 个 AST 节点、深度 32、运行成本 256；字符串值最多 4096 字节，枚举/字面量集合最多 64 项，规范化前定义 JSON 最多 1 MiB。空属性枚举表示不限定字符串取值；字面量成员集合不允许空集合或重复值。属性不得覆盖继承链上的同名 key，菱形继承同一个属性不算冲突。v1 仅接受两端为同一类的传递关系声明；逆关系须双向显式引用，端点和传递性一致。

```bash
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-module MODULE=ontology
# 并发安全与覆盖率仍走同一标准入口：
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  GOFLAGS='-race -count=1 -cover' make test-module MODULE=ontology
# 图适配及 PG/FalkorDB 联动门禁；无需传入个人图数据库地址或密码：
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-ontology-falkor
# System 独立 IAM 测试库，只验证本切片的两步向前迁移：
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_iam_test?sslmode=disable' \
  bash scripts/test/system-iam-postgres-gate.sh --package migration --test ontology-backend
# 共享严格 JSON 绑定器与授权覆盖变更需扩散到 Go 消费方：
make test-go
```

标准模块门禁自动发现 `backend/go.mod`、PostgreSQL T2 和门禁自管的 disposable Compose T2，先预检连接条件，再执行 T0、T1、T2；T1 明确排除数据库连接变量与图集成开关。两个 T2 均进入根 `test-integration` 串行聚合、CI 独立 Job 和本地巡检的同一发现路径。

FalkorDB T2 固定 4.20.6 多架构镜像 digest，每轮独立 Compose Project、随机密码、动态回环端口、资源上限与非零服务端超时；不读取根 `.env`，不使用个人 Redis 或图实例。正常退出、失败和可捕获中断都删除自有容器/卷/网络并核验零残留。T2 覆盖投影往返/防覆盖/篡改检测/最大定义、参数转义与大整数、服务端读写超时/回滚、重复取消后的客户端关闭和服务端回收、并发结果隔离；同一门禁再连接 PG 验证发布、真实图构建、激活、失败保留旧版本及新 generation 重建恢复，使用可控授权夹具，不宣称正式 HTTP/IAM 端到端验收。PG owner 状态门禁另覆盖并发激活、ABA、授权失效、撤回、租约恢复、重建去重/祖先分叉拒绝、新主体授权、旧授权拒绝、审计回滚与 v1/v2 向前迁移。清理核对本轮修订及全部投影的 execution，不遗漏重建任务。服务端 TCP FIN 回收允许有界异步收敛，不以一次 CLIENT LIST 快照冒充泄漏判断。

2026-10-16 图残留门禁补充确定性回归：[固定版本的图释放实现](https://github.com/FalkorDB/FalkorDB/blob/v4.20.6/src/graph/graphcontext.c)在 `GRAPH.DELETE` 同步移除图键后，异步删除关联 `telemetry{graph}` stream，不能将命令应答等同于全库已清空。两个受服务端预算限制的读查询保留图引用，旧清理在删图后实际报出两个 telemetry stream 残留；修复仅在原有 10 秒清理预算内观察本轮已删除图的精确 stream，最终仍要求 `DBSIZE=0`。图键、未知键、错误类型、命令失败及永久 stream 残留均拒绝，不重试删图、不手工删除真实 telemetry、不改变取消和回滚断言。串行完整 `make test-ontology-falkor`（配置自检、11 组图子例及其中 5 个反向场景、PG 发布联动）与 `GOFLAGS='-race -count=1' make test-module MODULE=ontology GO_ONLY=1` 通过；无新入口、CI Job、依赖或运行行为，不新增本体修订。完整入口曾在脚本夹具的 10 秒／5 秒等待处失败，后续原阈值串行自检通过；新增永久残留回归曾错误地只按 context 错误分类，已改为核对单调时钟截止与残留仍存在，socket 超时同样失败。不宣称脚本夹具超时根因、完整模块 T0/T3 或真实 T4 已验证。

本地只允许回环地址的 `addp_test`，CI 只允许 Job 独占的 `addp_ontology_test`。门禁拒绝清空已存在的 ontology schema；当次新建 schema 带随机 Run ID 所有权标记，正常退出、失败及可捕获中断都通过同一 owner 清理函数删除本轮 execution 与 schema 并检查残留，不清空 common。强制 SIGKILL 无法运行退出钩子；若残留，核对原 Run ID 后显式使用 `make test-ontology-postgres-cleanup ONTOLOGY_POSTGRES_CLEANUP_RUN_ID=<UUID>`，并提供安全测试 DSN。此入口只恢复指定轮次，复用同一清理函数及 advisory lock，标记不符或锁占用均拒绝；不能直接删除未知 schema，也不放宽正常门禁的拒绝规则。CLI 策略归平台 T0，真实标记和并发锁拒绝归原 PG T2/CI，无新增依赖。

新增 CEL 依赖同时由 T0 的 `make test-go-dependency-policy` 检查；该入口覆盖单行/块状 `require`、错误版本拒绝和注释/排除声明不误识别，不能靠固定 go.mod 排版才能识别依赖。

Outdoor 合成示例用于测试夹具及用户显式创建的本地演示本体，不自动供应到其他 Tenant。北京是示例背景，不从活动名称猜测行政区。核心代码不得硬编码业务状态、集合名、字段路径或地名。`beijing_outdoor_demo` 修订 1 已在个人开发环境激活，Agent 会话 76 已验证两个定义 Tool 的真实调用；其中继承关系及简化报名集合是演示内容，不是可直接绑定真实 Outdoor 数据的业务模型，详见[本体定义与发布规范的夹具边界](../docs/spec/addp本体定义与发布规范.md#十outdoor-定义夹具与验证边界)。

正式业务定义 `internal/semantic/testdata/outdoor_business.json` 按已确认的 Outdoor 口径区分活动、人员和活动成员关系，无相互继承；三个规则分别表达统计有效活动、成员报名状态和成员实际参加状态。`business_fixture_test.go` 验证完整状态矩阵、上下文隔离、缺失/未知和错误输入；沿现有 `make test-module MODULE=ontology` 及 CI Go 自动发现执行，无新增运行依赖。本轮按用户确认不接 Catalog、字段映射或业务取数，不向业务定义包注入映射，也不自动将该文件发布到 Tenant。日期存在性是有明确完整性要求的输入观察；成员分类不隐式关联活动或构成统计人数。正式业务定义与合成演示用途不同，不能相互作为运行时回退。

2026-09-18 已由真实用户页面创建、提交并发布个人开发环境 Tenant 1 的 `outdoor_business` 修订 1，投影已激活；持久化定义与上述测试夹具逐项核对一致（忽略规范化数组顺序）。Agent 会话 77 的运行 `6877c35e-1d9a-4ce1-a194-85b71394c47d` 完成目录及两个类上下文的三次只读 Tool 调用，正确区分报名/参加口径、成员关系与活动粒度、日期缺失与未知，以及定义与真实业务证据。`make test-agent-eval` 通过；此记录仅为本地真实用户联调，不替代下述正式隔离 T4。

### 正式 Online 验收入口

`ontology-revision-lifecycle` 已登记为仅手工触发的 GitHub Hosted T4 suite；入口为隔离部署中的 `make test-online ONLINE_SUITE=ontology-revision-lifecycle`，生命周期由 `scripts/test/online-hosted-ontology-gate.sh` 复用统一 Hosted owner 管理。[2026-09-18 首跑](https://github.com/pampa0629/addp/actions/runs/35304960321) 在公共 Infra 拉取 `minio/minio:latest` 时被拒绝，未进入本体业务断言；退出证据为 `cleanup=passed`、`infra_cleanup=zero_residuals`。该项按用户决定暂缓，仍未取得真实 T4 通过证据，脚本单测和本地开发服务 Ready 不能替代它，也不阻断独立的建模入口开发。

场景复用 `internal/semantic/testdata/beijing_outdoor_online.json` 的北京 Outdoor 合成本体；语义内核 T1 同时验证此定义可编译，并区分明确北京、明确其他与未知地点。Hosted helper 只给 User 分配本体 read/update/publish 与执行授权派生四项权限，不创建 Engine Provisioner 或业务 Engine。所有本体操作通过 Gateway，核对两次发布的不可变快照、generation/execution、ready 与 active 指针、旧版本冲突、最新尝试发现，以及撤回第二修订不自动回退第一修订。

没有业务实例查询或分类 API 的验收。修订、审计和图历史在场景中保留，退出时停止当次应用、销毁整个独占 Infra 及数据卷和临时凭据；生命周期另查容器/网络/卷零残留。发现旧 Infra 容器或卷时必须在启动前拒绝，不能误用或删除既有环境。确定性脚本验证由 `make test-online-runner` 纳入 T0，实际执行由 `.github/workflows/online-t4-gates.yml` 的 `ontology-hosted-t4` Job 完成。

平台 Transfer 修订 10 仅同步 CSV / TSV 编码读取消费调整后的 Planner 来源审查：本次任务未显式指定编码时，Planner 复用 Meta 已扫描编码；显式任务读取参数保持原契约。已重新审查 `validateTableTransferSpec` 的快照目标约束，仍只接受 replace/append。创建条件、效果、权限和选定覆盖范围不扩张，读取能力回归不表示创建切片或真实执行均已验证；来源提交与文件摘要一起冻结，标准 T0 检查漂移。

2026-10-15 Transfer 修订 22 复核并冻结 `091391c54` 的配置预检修复：replace 额外要求实例与插件均声明删除能力且实现删除 Provider，已知缺失返回 unsupported，缺少结构快照不覆盖确定反证。创建流程、权限、三态含义、两次预检和现有能力覆盖保持原定义，只更新 config／handler 来源及发布修订；不新增 API、迁移、权限或 Provider 能力。来源先提交后固定真实 Git 身份；沿已有 `make test-platform-review` 与 Ontology Go T1 自动发现来源漂移和修订绑定回归，部署激活另需用户按标准入口重启 Ontology。

2026-10-27 平台 Transfer 修订 34 复核来源 `5227df6c72f08bd5baecf6d4a8b2de7d320d111f` 的单文件 encoded ORC Reader 与源版本绑定。逐项检查确认 Planner 只在 encoded 分支增加运行时读取准入和版本校验；当前平台定义仍只建停止态 bounded/snapshot/native table，原有条件、权限、效果、排除效果及文件执行未建模边界保持。定义除 revision 外逐字段相同，只冻结实际已提交来源与新摘要，不修改发布／投影实现或 schema，不宣称修订已在运行环境激活。验证沿原 `make test-platform-review` 与 Ontology Go 门禁；ORC 产品的 Native、Meta／Manager／Transfer 与正式 Hosted 由格式 owner 分别验收。


2026-10-27 平台 Transfer 修订35复核 `4c9e1e53ee1e895c398dd0e0aaa46de54d832422` 的导出保护记录增量。对照此前 `d01c5772f` 的 `task_service.go`，唯一新增是已核验 Manager/Develop ExportSession 的 CreateAdHocExecution 分支初始化私有 ExportProtectionReceipt；普通 CreateTask 仍保存 idle/stopped 定义，委托准入、权限、确认、条件、效果及排除效果均不变。记录初始化不授予权限、不变换字段、不写业务数据；Worker保护与再次交付仍在当前创建能力的未建模范围。仅更新 service 来源的用途、真实已提交身份和SHA-256，保留全部来源/绑定/覆盖条目；定义除revision外逐字段相等，不将导出执行纳入创建语义。没有额外生成文件或手工PG快照，沿既有唯一编译器冻结嵌入的定义与审查JSON。

本轮先以非缓存 `make test-platform-review` 实际复现 service 漂移，再同步修订35及原有修订断言；同一标准入口强制 `-count=1` 后通过，`make test-module MODULE=ontology GO_ONLY=1` 与 `make test-module MODULE=transfer GO_ONLY=1` 通过。原门禁保留真实Git提交字节/当前文件/锚点核验、伪造来源拒绝和完整绑定约束，未刷新其他来源、删源或放宽断言；已有Platform T0与Ontology Go自动发现，无新测试/构建/依赖/CI入口，未改schema、发布/投影内核或前端，未重复T2/T3。日志 `/tmp/p145-export-source-review-{red,green}.log`、`/tmp/p145-export-source-{ontology,transfer}-t1.log`，独立差异核对 `/tmp/p145-export-platform-review-audit.json`。4c原提交漏同步本项；此前T1/T2/T3不能替代本次强制非缓存来源门禁，已纠正交付记录。此结果不宣称新CI来源恢复、修订35已激活或真实CSV验收通过；CI由专门会话跟进，用户重启及实际CSV闭环仍待完成。


2026-10-28 修订35的来源漂移修复已有实际CI恢复证据：`79fa05e7b9907b0ad978ce8716ac050b56314a80` 的 Go workspace run40521342460/job122248960679 的 `Run all tracked Go module tests` 实际成功，平台来源包0.676s通过；包含修复的后继 `7ebf98370ebc965e09cb5744983dbf8ee7fbbbce` 的完整 Platform consistency run40523616999/job122257071998 的 `Run platform consistency gate` 实际成功，同包0.176s通过。专门CI会话确认终态，本会话只核对保存的原始日志摘要、Job元数据/来源/attempt及实际非跳过步骤，没有查询或重跑CI。审计 `/tmp/addp-ci-20261027-160029/Transfer_79fa_review_source_actual_CI_recovery_audit.json`，独立复核 `/tmp/p145-export-source-ci-return-independent-audit.json`，链接与范围详见 security/CLAUDE.md 本日记录。旧失败保留，仅收口该来源漂移；不等于修订35已激活或真实CSV验收通过，后者仍未收到已有脚本的运行证据。仅归档证据，无产品/规范/发布内核变化，复用既有门禁不重复执行。
