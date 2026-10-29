# Catalog 模块开发说明

Catalog 是企业资源目录的唯一事实源，负责稳定目录身份、来源绑定、业务语义关联、字段/组件标准映射、责任关系、目录可见性和治理状态。

## 必读文档

- [企业资源目录体系图](../docs/concepts/addp企业资源目录体系图.md)
- [企业资源目录实现规范](../docs/spec/addp企业资源目录实现规范.md)
- [术语表](../docs/concepts/addp术语表.md)
- [API 设计规范](../docs/spec/addp-API设计规范.md)
- [新模块开发指南](../docs/spec/addp新模块开发指南.md)

## 边界

- 职责与读取：[目录身份与模块边界](#目录身份与模块边界)、[专业事实与标准映射](#专业事实与标准映射)、[目录浏览与联邦读取](#目录浏览与联邦读取)。
- 业务授权：[责任与权限](#责任与权限)、[业务确认与依据核验](#业务确认与依据核验)、[正式准备与受理](#正式准备与受理)、[受理核清与自动签发](#受理核清与自动签发)、[授权工作区与记录读取](#授权工作区与记录读取)。

### 目录身份与模块边界

- `CatalogEntry` UUID 是企业稳定身份；Meta fingerprint 只是来源身份。

- 不复制 Meta、Model、Standard、Service、Develop、System 的完整专业事实；只保存目录身份、失效解释、列表和搜索所需的最小已观察投影，完整专业详情动态读取 owner。

- 不向 Meta 或 Standard 回写 Catalog ID 或关联投影。

- 跨模块只走公开 API 和 Tenant Service Access Token，不跨 Schema 查询。

- 除 System 注册和本模块必需基础设施外，任何业务模块不可达都不能阻止进程启动；Meta / Model / Standard / Service / Develop 同步失败只产生滞后并后台重试，各 owner 使用独立 checkpoint。

- 不提供 DataItem CatalogEntry 的手工创建和删除 API。

- Catalog 不提供泛化 `CatalogRelation` 或可配置关系类型；当前唯一自有跨条目关系是弃用条目的可选推荐继任项，只通过治理子资源 `PUT /entries/:id/governance` 维护，使用 CatalogEntry 聚合版本；普通编目更新不得读写它。推荐继任保持两个独立企业身份，不等同 `merged`。

### 专业事实与标准映射

- Model Entity / LogicalTable 的专业内生 Domain、Element、Metric 和建模关系归 Model，Catalog 不建立可编辑副本。

- Standard 五类标准的名称、定义、修订、状态、Domain、分类、单位和语义依赖归 Standard，Catalog 不建立可编辑副本。

- Service QueryService 的 SQL、发布快照、协议、输出契约、Consumer Descriptor、运行状态和端点归 Service，Catalog 不建立可编辑副本；QueryService 没有 owner Domain，primary Domain 仍归 Catalog。

- Develop 只为已持久化的 `query|workflow` DevTask 建立 `development_artifact`；`script` / Notebook、即时查询、execution、运行结果和 ToolApproval 不进入企业目录。DevTask 内容、DAG、参数、Engine 绑定与执行契约归 Develop，Catalog 只保存最小可重建观察摘要并动态解析当前详情。

- Quality 评分、Issue 和 execution 历史不是 CatalogEntry，不进入 Catalog 存储或搜索投影。只对 Meta PostgreSQL table DataItem 使用 owner 提供的 `engine_id + schema_name + table_name` 按需动态解析，不拆分 `full_name` 猜测定位。

- `StandardMapping` 是 CatalogComponent 到确定 `Standard.ElementRevision` 的可审核、可追溯关系事实，拥有独立 UUID、并发版本、来源、置信度和审核状态。企业落标关系只归 Catalog；Quality 方案的物理目标与冻结标准来源仅表达检查意图，不构成第二套企业映射，也不以 Catalog 为前置。

- 旧 `component_element_associations` 只在数据库迁移事务中读取：保留历史观察证据并转为未固定修订的 `legacy/proposed` 候选，随后删除旧表。业务 API 和 CatalogEntry 聚合更新只使用独立 StandardMapping；历史候选经人工固定发布修订并审核前，不得视为已落标。

### 责任与权限

- [责任关系](../docs/spec/addp企业资源目录实现规范.md#授权职责与直接许可)不自动授予 IAM Role、数据访问或授权办理资格。已确认的目标规则以 `docs/spec/addp授权上下文规范.md` 5.5 为准：Catalog 提供当前责任资格及业务入口，System 引擎访问控制领域唯一维护源数据规则；不在 Catalog 新建 Grant 副本，不以 Catalog 可达性阻塞已有合法数据访问或其他模块 Ready。schema 普通表 single 的业务确认、办理及自动签发绑定 DataItem 身份与摘要，Manager 预览／剖析／结果读取消费同一源规则；不代表文件组合及全部模块已受控。

- 业务共享确认和源读取授权办理使用独立功能 Permission，首版仅通过角色显式分配，不默认授予内置管理员，不从编目权限、责任身份或引擎管理委派推导。Permission 与真实消费入口同时发布，不能先启用占位权限。

- 责任历史按[持久化契约](../docs/spec/addp企业资源目录实现规范.md#责任历史与来源身份)维护：首次保存有效 `business_owner` 即建立，不等待完成编目。内部 nullable `business_responsibility_established` 新建为 false，责任写入同事务置 true，旧历史不足为 NULL；撤销编目、移交、失效或来源变化不得重置，不接受请求方编辑，也不单独证明当前资格或精确源资源的授权依据。

- [业务批准与直接许可](../docs/spec/addp授权上下文规范.md#555-直接授权许可业务批准与明确交接)分开：保存责任不改许可；业务批准不依赖许可开启，不查询、初始化、改写许可或携带旧批准要求版本。System 直接签发按显式许可优先、持久 Catalog 部署声明取默认值；未登记或读取失败拒绝，进程失联不推导允许。实际覆盖见[验证导航](#验证导航)。

- 确认人与办理人的版本规则以[授权职责契约](../docs/spec/addp授权上下文规范.md#551-事实与职责分开)为准：确认人的历史版本仅作审计，无关角色调整不作废决定；首次受理由 System 复核原账号／Membership 与当前确认权限，Catalog 复核原责任连续性和来源。办理命令严格匹配原请求版本；历史 GET 按[当前读取资格](../docs/spec/addp企业资源目录实现规范.md#共享历史与签发结果)独立判定，不要求历史版本等于当前版本或原确认人仍为业务负责人，查询不恢复新办理资格。实际覆盖沿[验证导航](#验证导航)，内部资格测试不替代真实 OAuth T4。

- 已弃用条目禁止新的共享确认与办理接受；已有访问规则单独撤销。来源同步先按 UUID 稳定顺序锁定本批既有条目，再锁 current 来源绑定，与人工维护保持同一锁顺序，不以来源重新出现撤销弃用或清除责任历史。

### 业务确认与依据核验

- `POST /entries/:id/sharing_decisions` 的权限、期限和历史恢复以[正式业务确认契约](../docs/spec/addp企业资源目录实现规范.md#业务确认与不可变决定)为准；当前限 Meta 已扫描数据库 table，要求条目读取、独立 `catalog.sharing_decision.create` 及当前业务负责人，自用或本人当前项目组受益须显式确认。`expiry_mode=at_time|until_revoked` 和用途必须显式填写：有限期为未来绝对日期，微秒精度、无统一天数上限；长期无到期日期，不默认永久、不用日期哨兵。`sharing_decisions` 是不可修改的决定历史，不是 Grant 或受理回执；同参重试匹配模式、日期和边界摘要，只恢复原记录。现有详情表单先以 `GET /entries/:id/sharing_boundary` 只读核验，新决定 POST 绑定 `expected_boundary_hash`；后续反查、首次受理与签发沿同一不可变绑定，不把历史恢复变成新批准。

- Meta 目标事实的唯一读取路线、完整性与拒绝边界见[授权资源描述契约](../docs/spec/addp元数据扫描机制规范.md#授权资源描述契约)。`common/client.MetaClient.ReadAuthorizationFacts` 只使用 Catalog 自身 `addp-catalog` Tenant Service 凭据，不冒用 System；非 complete、畸形或身份不一致的事实拒绝办理，不回退旧路线。数据项指纹与批准边界摘要随决定及办理冻结，System Grant 独立核验同一绑定。旧 native 测试确认及办理沿 owner 事务迁移清理，其他目录及通用审计保留。

- 正式准备与可信依据反查以[正式契约](../docs/spec/addp企业资源目录实现规范.md#准备绑定与可信依据)为准。`lockCurrentSharingDecisionBasis` 只在 Catalog 本地事务按条目→当前来源→原责任关系锁定并核验持久决定；业务确认创建在提交前复用，正式准备与 Runtime 反查共用，历史读取不调用。普通名称／说明修改不单因聚合版本增加使依据失效；换源、原责任失效或同账号重新接任不能复活旧决定。它不调用远端、不建立待核清，不证明 System 当前身份／权限／委派或跨模块可信来源；System 仍独立核验 IAM、办理权限及委派。

- 待核清目标的有效叶子、正 int64 Engine ID、64 个 segment 和完整 JSON 16 KiB 边界只由 `common/authorization.EncodeSharingTarget` 校验，与 System 仲裁复用；首次登记前拒绝不满足该边界的目标，不先冻结条目。节点身份完整保留，无效 UTF-8 不允许被 JSON 替换字符静默修正。该纯值校验不代替真实来源、认证和办理资格，也不改变现有业务确认 API。

### 正式准备与受理

- `prepareSharingFulfillment` 仅为内部事务登记原语：从持久决定派生业务参数，不可变绑定调用主体、原操作来源、决定及完整目标；同参恢复原待核清记录，已核清记录不重新打开，不建立 System 结果副本。原语不证明输入可信或办理资格，不能直接作为公开受理入口。

- `POST /entries/:id/sharing_fulfillments` 只接收请求编号和决定编号，不接受操作者、目标或接收方覆盖。正式消费者 `PrepareSharingFulfillment` 从可信 User AuthContext 派生原操作身份，经 System 核验独立办理权限和引擎管理委派，并读取当前 Catalog 服务身份。本地事务提交后才发送，通信不确定时保留待核清保护。现有前端显式入口复用该链，受理核清后调用唯一自动续办；POST 响应只表达受理，不声明 Grant 或访问已生效。

- `POST /runtime/sharing-fulfillments/:request_id/basis` 只允许 `addp-system` 当前 Tenant Service 身份及独立 `catalog.sharing_fulfillment.read`。按已提交且未核清的完整绑定复核当前来源与原业务负责人责任，只返回最小依据及原确认身份，不提供用途正文或通用决定枚举。System 在自身受理锁外反查、不跨 Schema；该依据不替代 System 当前资格与首次受理核验。实际覆盖按[验证导航](#验证导航)分别认定，内部函数或单接口验证不替代真实 OAuth 与数据读取验收。

- [受理分界与责任变化](../docs/spec/addp企业资源目录实现规范.md#受理分界与办理窗口)以[源规则生命周期](../docs/spec/addp授权上下文规范.md#553-责任变化与访问规则生命周期)为准。Catalog 核验当前责任与精确业务决定；受理以 System 事务提交回执为界，Grant 仍由 System 独立签发。责任变更、来源重绑或弃用先完成时旧依据不得支持新受理，移交须由新负责人重新确认；受理先完成只允许原同次、同参数继续，仍复核办理人、管理范围与接收主体。Catalog 不保存第二份可编辑接受状态，也不复制业务决定到 System。

- 自动办理窗口从 System 原受理数据库时间起 5 分钟，并受拟授权绝对到期时间截断；长期有效仍只有 5 分钟。它不是人员操作时限或数据访问期限，重试不刷新窗口。未签发且过窗须重新取得依据并受理；已签发原请求仅恢复原结果，不重复签发，不复活已撤销或过期 Grant。

- `fulfillment_checks` 是发送前持久待核清事实，用数据库保护受影响条目的依据写入，不保存 System 受理时间／截止时间。仅持 Catalog 本地锁调用 System 不能证明故障后的先后；顺序以权威受理事实核清。PG 普通表 single 的业务批准、Manager 真实预览、Deny、解除及撤销，按下文[验证导航](#验证导航)定位场景方法和新旧协议的固定版本证据，不合并为当前 HEAD 或完整链路通过。此范围不代表其他引擎或消费入口已贯通；编目更新、受理及签发历史都不能替代当前实际访问裁决。

### 受理核清与自动签发

- 受理核清和自动签发续办以[目录恢复契约](../docs/spec/addp企业资源目录实现规范.md#受理核清与自动续办)、[源规则生命周期](../docs/spec/addp授权上下文规范.md#553-责任变化与访问规则生命周期)为准。`resolved_at` 核清受理并解除责任／来源保护；`grant_reconciled_at` 仅核清自动处理，不是 Grant 或签发成功状态，不延长保护。旧记录不回填已处理；历史 GET 不触发续办、不改变受理响应，不以核清标记裁决访问。

- `reconcileSharingFulfillment` 只消费已提交的原待核清记录，在 Catalog 事务外先查 System 原结果，明确未找到才关闭同一请求；关闭也可能返回已受理结果。完整绑定一致且结果为 accepted／closed 后，按条目→待核清记录锁顺序填写一次数据库核清时间。普通 404、鉴权、通信、解析失败及未知结果不等于未找到，不得解除保护。历史核清不要求原责任／操作人／期限仍有效，不恢复新办理资格；不创建准备记录或新受理，正式准备仍由人类入口显式发起。

- 前台和 `SharingFulfillmentReconciliationRunner` 共用 `continueSharingFulfillment`，受理核清且原结果为 accepted 时，先按原参查 System 签发历史，明确未找到才签发；丢响应保留恢复。仅 closed、已有签发历史或 System 明确原窗口到期／关闭可终结自动处理，其余失败保留待续办。Runtime 核清／关闭／签发只使用 `addp-catalog` 当前 Tenant Service 身份及独立 `system.engine_access_fulfillment.execute`。

- 恢复沿来源同步周期扫描创建满一分钟、自动处理尚未核清的记录，每批 100 条按游标推进，单项续办限时 30 秒，不门控 Ready。调度等待与受理后的办理窗口分别认定。实际覆盖与未验项沿[验证导航](#验证导航)按固定源码认定，内部核清或签发测试不替代真实 OAuth 与实际读取验收。

### 授权工作区与记录读取

- 数据项详情的“业务授权”使用同排互斥的“批准数据使用、办理访问授权、批准与办理记录”，引擎授权跳转置于最右侧。批准区只填写业务批准，办理区只选择有效批准并显式提交；记录区区分业务批准与本人办理，列表和只读详情分开，返回列表不写入。批准成功后在进度说明旁提供显式继续办理入口；当前确认仅在正式候选本页出现时自动选中并只读观察，不自动提交或初始化、不从确认详情补造候选。复用原确认、办理和历史 API，不新增授权路线。

- 业务批准表单使用本条目专用接收方候选，期限显式选择、不默认永久。界面不提供 System 直接授权或直接许可管理；具有引擎及 Grant 读取权限且来源引擎引用已解析时，才可经 Console 前往 System 引擎授权。

- 业务授权工作区的布局、进度与同参恢复沿[正式界面契约](../docs/spec/addp企业资源目录实现规范.md#业务授权工作区)。新办理未绑定本次请求编号时，只展示批准条件与当前授权核查；旧办理进度留在记录详情，正式提交或打开精确请求后才在办理区展示，结果须匹配所选确认与请求。实际覆盖见[验证导航](#验证导航)，不以进度展示代替真实签发或读取验收。

- 候选去重与授权观察以[正式契约](../docs/spec/addp企业资源目录实现规范.md#接收方选择与办理候选)为准。先核验当前来源、原责任／成员关系，再按精确 DataItem／边界、直接接收方类型／编号、动作、有效期模式和精确截止时间分组，之后计数分页；历史不去重、不删除。接收方批准摘要须核对 Meta 当前基础边界，仅复用当前成员关系下的原编号；新编号重复批准在条目锁内拒绝，原编号同参恢复不变。

- `GET /entries/:id/sharing_decision_candidates` 的条目可见性、`catalog.entry.read`、独立 `system.engine_access_fulfillment.create` 和有效引擎管理委派守卫按正式契约执行，使用本次 User Bearer。跨模块核验不持 Catalog 锁，返回后在只读快照复查可见性、来源、原责任及期限。共享 Tab 只消费分页摘要，不含用途正文、不创建待核清保护、受理或 Grant；摘要不是正式准备／新受理依据，也不授予共享历史读取资格。

- “已有授权”只由当前 User 的独立 `system.engine_access_grant.read` 及 System 当前管理资格读取：按精确目标、直接接收方和 `state=active` 查询 Grant 历史，核对 DataItem 指纹与 read 动作，不用组织来源展开列表或 Catalog 运行身份代查。观察到相同关系时只阻止新建本次办理，不自动撤销、覆盖或续期；最终防重归 System 写入路径，已固定请求编号的同参恢复不受新预查阻断。无权、失败或分页不完整不代表无授权，提示也不证明当前边界匹配或实际可读；验收范围见[验证导航](#验证导航)。

- 共享历史与[本人办理记录](../docs/spec/addp企业资源目录实现规范.md#本人办理历史)按[正式读取契约](../docs/spec/addp企业资源目录实现规范.md#共享历史与签发结果)执行，均要求当前 Tenant User、`catalog.entry.read` 与条目可见。共享历史另需 `catalog.sharing_decision.create`，原确认人读本人历史、当前业务负责人复核本条目全部历史，无需办理权限或引擎委派；本人记录另需 `system.engine_access_fulfillment.create` 和原请求 Engine 的当前管理范围，只限本人原请求。编目权限不能替代。

- 两类记录复用结果说明，优先显示结果记录时间、办理结果和签发情况，确认／请求编号按需展开并可复制。接收方名称展示及其失败语义见[接收方名称解析](../docs/spec/addp企业资源目录实现规范.md#接收方名称解析)。复核者沿用批准结果读取资格，不调用仅办理人可用的请求详情。`granted_at` 仅表示历史签发，不表达当前访问 Allow。完整绑定、事务外查询及返回前复核按上述契约执行；只读查询不保存结果副本、不核清、不签发。验收范围见[验证导航](#验证导航)。

- 新批准与办理发送前分别冻结原决定／请求编号及参数，并先同步公开 URL。通信不确定时锁定工作区和记录切换，保留原编号、原参数；失败或 pending 保留同参显式重试，刷新和页面重载只 GET 原记录，不恢复写命令。正常换选确认须清除旧请求 URL 和结果。查询 accepted／closed 后结束不确定重试，错误不伪装为 pending，accepted 不表达 Grant 已生效。

### 目录浏览与联邦读取

- 目录集合只使用当前 Tenant 功能 Permission 与有效 Project Group 成员关系的交集，不使用 Department / Project Group Role Assignment Scope。集合条目仍逐项核验目录可见性；项目组名称动态读取 System，组织与成员事实、源数据 Grant 和业务批准均保留。列表与详情共用 `/me/project-groups` 的可维护状态，前端不重新解释组织范围角色。

- `/entries` 的业务域上下文用 `catalog.entry.read` 动态发现 Standard Domain 的名称、编码、定义和层级，仅投影本次响应；独立业务域总览复用同一目录计数，Standard / Model 专业详情仍由 owner 对当前 User Token 判权，不能以 Catalog 运行身份代查。

- Meta 数据血缘只在 Catalog Frontend 中以当前 User Access Token 动态查询 Meta 唯一图接口，并复用 `common-frontend/graph`；Catalog Backend 不代理、不复制血缘边，Meta 不可达不影响 Catalog 详情和 Ready。

- [数据字典](../docs/spec/addp企业资源目录实现规范.md#查询与修订解释)是 Catalog 联邦读模型：只对 active Meta DataItem 组合 Meta 当前物理字段、Catalog 权威且已审核的 StandardMapping 与其中冻结的 Standard ElementRevision，不落表、不使用已观察摘要或动态“当前版本”伪造标准事实。

- [数据字典导出](../docs/spec/addp企业资源目录实现规范.md#同步导出与不可变快照)是上述联邦读模型的一次同步 JSON 捕获：服务端重新解析并返回带生成时点和 SHA-256 ETag 的附件，不保存导出任务、文件或第二份事实；批量发布与长期托管不属于该接口。

## 业务域总览与五类标准接入

[业务域总览与资源浏览](../docs/spec/addp企业资源目录实现规范.md#44-业务域总览与资源浏览)规定独立总览入口与展示口径。Catalog、Model、Standard 的域级引用各读本地事实，以当前 User 权限判定；精确域、一跳、按目标去重并保留证据。“本域使用的标准”保留本域、其他业务域、公共及无法确认归属的目标，不在分页后隐藏本域目标。

[五类标准接入与动态引用](../docs/spec/addp企业资源目录实现规范.md#55-standard-五类专业资源变化与动态引用)规定稳定身份及专业事实归属。定义和修订唯一归 Standard，Catalog 不维护可编辑名称／定义副本，不复制专业关系或改写冻结修订。

方法与固定版本结论见[验证导航](#验证导航)，Hosted API、本机浏览器及未覆盖范围分别认定，不以本节能力摘要代替当前版本验收。

## 验证导航

确定性门禁沿[测试与验收标准入口](../docs/spec/addp测试与验收规范.md#三标准入口)执行；Catalog owner 使用 `make test-module MODULE=catalog`。下表仅定位已有方法和记录，不表示当前 HEAD 已运行通过；失败、未执行和未覆盖项按各记录保留，部分页面或接口通过不能替代整轮结果。

| 验证目的 | 方法与证据正文 |
| --- | --- |
| 数据字典查询与同步导出 | [现有测试断言、标准入口与未覆盖范围](../docs/spec/addp企业资源目录实现规范.md#数据字典验证导航)：区分受控依赖、附件编码与真实联邦查询／下载验收。 |
| 企业目录发布、生命周期与共享受理 | [场景方法](../docs/spec/testing/scenarios/授权与共享.md#发布生命周期与共享受理)、[OAuth 自动签发的只读观察](../docs/spec/testing/scenarios/授权与共享.md#oauth-自动签发的只读观察)：专用 macOS profile，区分受理、唯一签发与源数据实际读取。 |
| PG 业务批准与实际读取 | [`pg-source-authorization` 的业务批准闭环](../docs/spec/testing/scenarios/授权与共享.md#catalog-业务批准闭环)、[Catalog 离线与恢复](../docs/spec/testing/scenarios/授权与共享.md#catalog-可选依赖离线与恢复)；[旧协议与新成果证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#pg-dataitem-源授权与-transfer-新成果初始授权--hosted-t4)、[直接许可模型切换后的固定版本证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#直接授权许可模型替换--pg-hosted-t4-与开发库迁移)。 |
| 后续 PG 浏览器扩展的局部结果 | [许可提交与冲突恢复记录](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#直接许可浏览器提交与冲突恢复--pg-hosted-t4-局部通过)：页面局部通过、后续在途到期检查导致整轮失败；保留未执行项与清理证据。 |
| 业务域总览 Hosted API | [三类身份、跨域引用与清理方法](../docs/spec/testing/scenarios/授权与共享.md#catalog-domain-overview)、[固定源码成功记录](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#catalog-业务域总览--hosted-api-t4)、[首次失败与修复](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#catalog-业务域总览--首次失败与修复)；不含浏览器 UI T4 或全量跨域关系。 |
| 本地 Opera 普通用户复验 | [账号、跨域样本与页面断言](../docs/spec/testing/scenarios/授权与共享.md#catalog-本地普通用户浏览器复验)、[隔离入口、租约及结束判定](../docs/spec/testing/T4-Online验收规范.md#catalog-本地隔离浏览器验收)：本机技术证据，与 Hosted API 分别认定，页面证据接收后仍须源标准复核及清理。 |
