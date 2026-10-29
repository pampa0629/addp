# Catalog 模块开发说明

Catalog 是企业资源目录的唯一事实源，负责稳定目录身份、来源绑定、业务语义关联、字段/组件标准映射、责任关系、目录可见性和治理状态。

## 必读文档

- [企业资源目录体系图](../docs/concepts/addp企业资源目录体系图.md)
- [企业资源目录实现规范](../docs/spec/addp企业资源目录实现规范.md)
- [术语表](../docs/concepts/addp术语表.md)
- [API 设计规范](../docs/spec/addp-API设计规范.md)
- [新模块开发指南](../docs/spec/addp新模块开发指南.md)

## 边界

- 目录集合只使用当前 Tenant 功能 Permission 与有效 Project Group 成员关系的交集，不使用 Department / Project Group Role Assignment Scope。集合条目仍逐项核验目录可见性；项目组名称动态读取 System，组织与成员事实、源数据 Grant 和业务批准均保留。列表与详情共用 `/me/project-groups` 的可维护状态，前端不重新解释组织范围角色。

- 新办理尚未绑定本次请求编号时，不显示所选批准过去的办理进度；只展示批准条件与当前授权核查。旧办理及签发进度保留在记录详情，正式提交或打开精确请求后才在办理区显示该请求进度。

- 业务批准的办理候选先按当前来源、原责任／成员关系、DataItem 边界、直接接收方和精确期限去重，再计数分页；历史不去重、不删除。接收方候选通过 Meta 当前基础边界返回当前成员关系下的批准摘要，批准入口可以只读复用原编号。新编号重复批准在条目锁内拒绝，原编号同参恢复不变。办理下拉的“已有授权”只消费当前 User 的独立 System 授权读取能力，不从业务批准或历史签发猜测当前状态。

- 共享历史与本人办理记录按[正式读取契约](../docs/spec/addp企业资源目录实现规范.md#321-责任与实际数据授权)执行，均要求当前 Tenant User、`catalog.entry.read` 与条目可见。共享历史另需 `catalog.sharing_decision.create`，原确认人读本人历史、当前业务负责人复核本条目全部历史，无需办理权限或引擎委派；本人记录另需 `system.engine_access_fulfillment.create` 和原请求 Engine 的当前管理范围，只限本人原请求。编目权限不能替代。

- 两类记录复用结果说明，优先显示结果记录时间、办理结果和签发情况，确认／请求编号按需展开并可复制。复核者沿用批准结果读取资格，不调用仅办理人可用的请求详情。`granted_at` 仅表示历史签发，不表达当前访问 Allow。完整绑定、事务外查询及返回前复核按上述契约执行；只读查询不保存结果副本、不核清、不签发。验收范围见[验证导航](#验证导航)。

- 数据项详情的产品入口为“业务授权”，使用同排互斥的“批准数据使用、办理访问授权、批准与办理记录”工作区，引擎授权跳转置于最右侧。批准区只填写业务批准；办理区只选择有效批准并显式提交；记录区区分业务批准记录与本人办理记录，列表和只读详情分开，返回列表不发出写命令。批准成功后在进度说明旁提供显式继续办理入口。进度只解释选定确认或精确请求的历史事实。当前确认仅在正式办理候选本页出现时自动选中并只读观察；不自动提交或初始化、不从确认详情补造候选。切换确认清除旧请求 URL 和结果，通信不确定时锁定工作区并保留原参恢复。复用原确认、办理和历史 API，不新增授权路线。

- 自动签发续办复用现有办理记录，`grant_reconciled_at` 仅表示自动处理已核清，不是 Grant 或签发成功状态；受理核清 `resolved_at` 仍独立解除责任／来源保护。前台与后台共用一个续办命令，原参先查 System 签发历史、明确未找到才签发，丢响应保留恢复；仅 closed、已有签发历史或 System 明确的原窗口到期／关闭可终结。旧记录不回填已处理；历史 GET 不触发续办，不改变受理响应契约，不以该标记裁决数据访问。

- 详情的“业务授权”是人类操作入口：业务批准需要独立确认 Permission 及当前业务负责人资格；正式准备和只读原办理请求需要独立办理 Permission。接收方使用本条目专用候选下拉；期限显式选择，不默认永久。新办理选择有效业务批准，Catalog 业务流程始终保留，不受 System 直接授权许可开启或关闭影响；不查询、初始化或改写直接许可，也不携带旧批准要求版本。提交前分别冻结原决定／请求编号及参数，并先同步公开 URL；失败或 pending 保留同参显式重试，刷新只读原记录。查询 accepted／closed 后结束不确定重试，错误不伪装为 pending，accepted 不表达 Grant 已生效。当前界面不提供 System 直接授权或直接许可管理；具有引擎及 Grant 读取权限且来源引擎引用已解析时，可经 Console 统一导航前往 System 引擎授权。

- 不复制 Meta、Model、Standard、Service、Develop、System 的完整专业事实；只保存目录身份、失效解释、列表和搜索所需的最小已观察投影，完整专业详情动态读取 owner。
- 不向 Meta 或 Standard 回写 Catalog ID 或关联投影。
- 责任关系不自动授予 IAM Role、数据访问或授权办理资格。已确认的目标规则以 `docs/spec/addp授权上下文规范.md` 5.5 为准：Catalog 提供当前责任资格及业务入口，System 引擎访问控制领域唯一维护源数据规则；不在 Catalog 新建 Grant 副本，不以 Catalog 可达性阻塞已有合法数据访问或其他模块 Ready。schema 普通表 single 的业务确认、办理及自动签发绑定 DataItem 身份与摘要，Manager 预览／剖析／结果读取消费同一源规则；不代表文件组合及全部模块已受控。
- 业务共享确认和源读取授权办理使用独立功能 Permission，首版仅通过角色显式分配，不默认授予内置管理员，不从编目权限、责任身份或引擎管理委派推导。Permission 与真实消费入口同时发布，不能先启用占位权限。
- `POST /entries/:id/sharing_decisions` 已提供普通只读业务确认生产入口，须同时具备条目读取与 `catalog.sharing_decision.create`，并是当前业务负责人；本人或所在项目组受益可以显式确认。首版只支持 Meta 当前扫描的数据库 table，经共享 MetaClient 读取 Meta 当前 complete 的授权资源事实并核验 System 引擎状态，不用祖先链重建边界、不拆 `full_name`、不读取源数据。每次必须显式选择 `expiry_mode=at_time|until_revoked` 并填写用途：前者填写未来绝对到期时间（微秒精度，无统一天数上限），后者无到期日期；不默认永久、不使用日期哨兵。`sharing_decisions` 是不可修改的决定历史，不是 Grant 或受理回执；同参重试必须匹配模式、日期及边界摘要，恢复原记录。该 API 已接详情确认表单；确认前通过 `GET /entries/:id/sharing_boundary` 只读核验当前业务责任及 Meta 边界，POST 强制绑定 `expected_boundary_hash`；System 可信反查、首次受理和签发均携带该不可变绑定，不把历史恢复变成新批准。
- `lockCurrentSharingDecisionBasis` 只在 Catalog 本地事务中读取持久决定、按条目→当前来源→原责任关系锁定并核验当前依据；业务确认创建在提交前复用该核验。历史读取不调用它，普通名称／说明修改不使决定仅因聚合版本增加而失效；换源、原责任失效或同账号重新接任不能复活旧决定。它不核验 System 当前身份／功能权限／委派，不调用远端、不建立待核清、不构成受理依据或跨模块可信接口；正式准备和专用 Runtime 反查消费者使用它核验 Catalog 持久依据；System 独立核验当前 IAM、办理权限及委派。
- `GET /entries/:id/sharing_decision_candidates` 提供另一名人类办理人的分页摘要：当前条目可见、`catalog.entry.read` 与独立 `system.engine_access_fulfillment.create`，再由 System 使用本次 User Bearer 实时核验有效引擎管理委派。远端调用不持有 Catalog 锁，返回后在新的只读快照复查可见性、当前来源、原责任关系和期限。摘要不包含业务用途正文，不是受理依据，不创建待核清保护、受理或 Grant；确认历史复核使用独立确认权限及本人／当前业务负责人范围，不因候选办理资格而获权。共享 Tab 已消费该分页候选；正式准备／新受理仍通过专用接口核验，不把候选摘要当作依据。
- `prepareSharingFulfillment` 是内部事务登记原语：首次登记复用当前决定依据核验，从持久决定派生业务参数，并不可变绑定本次调用主体、原操作账号来源、业务决定和完整目标。同参重试返回原待核清记录；已核清记录不重新打开，不建立 System 结果副本。正式消费者 `PrepareSharingFulfillment` 从可信 User AuthContext 派生原操作身份，通过 System 核验当前独立办理权限和引擎管理委派，并读取当前 Catalog 服务身份；提交本地事务后才发送，通信不确定时保留待核清保护。内部原语本身不证明输入可信或办理资格。
- `POST /entries/:id/sharing_fulfillments` 只接收请求编号和决定编号，不接受操作者、目标或接收方覆盖。`POST /runtime/sharing-fulfillments/:request_id/basis` 只允许 `addp-system` 当前 Tenant Service 身份及独立 `catalog.sharing_fulfillment.read`；精确匹配已提交待核清绑定，重新核验当前来源及原业务负责人责任，返回最小依据和原确认身份，不提供用途正文或通用决定枚举。System 在本地受理锁外反查，不跨 Schema。正式准备、首次受理及前端显式办理入口已接通，受理核清后调用唯一自动签发续办；POST 响应仍只表达受理结果，不声明数据访问已生效。
- `reconcileSharingFulfillment` 是唯一续办命令的受理核清阶段：只消费已提交的原待核清记录，事务外先查询，明确未找到才关闭原请求；关闭可能返回已受理结果。完整绑定一致且结果为 accepted／closed 后，条目→待核清记录锁内填写一次本地数据库核清时间。错误或未知结果保留保护，历史恢复不要求原责任／操作人／期限仍有效，不恢复新办理资格。生产恢复通过 `SharingFulfillmentReconciliationRunner` 调用 `continueSharingFulfillment`，受理核清后继续原参签发阶段；仅 `addp-catalog` 当前 Tenant Service 身份及独立 `system.engine_access_fulfillment.execute` 可消费 System Runtime 核清／关闭／签发。至少等待一分钟后扫描自动处理尚未核清的记录，沿用来源同步周期，每批 100 条游标推进、单项续办限时 30 秒，不门控 Ready。普通 404、鉴权、通信及解析失败不得当作未找到；恢复不创建准备记录或新受理，正式准备仍由人类入口显式发起。
- 待核清目标的有效叶子、正 int64 Engine ID、64 个 segment 和完整 JSON 16 KiB 边界只由 `common/authorization.EncodeSharingTarget` 校验，与 System 仲裁复用；首次登记前拒绝不满足该边界的目标，不先冻结条目。节点身份完整保留，无效 UTF-8 不允许被 JSON 替换字符静默修正。该纯值校验不代替真实来源、认证和办理资格，也不改变现有业务确认 API。
- 跨模块只走公开 API 和 Tenant Service Access Token，不跨 Schema 查询。
- 共享目标只通过 `common/client.MetaClient.ReadAuthorizationFacts` 按稳定指纹读取 Meta 最小授权事实；Catalog 使用自身 `addp-catalog` Tenant Service 凭据，不冒用 System 身份。不再用祖先链、Meta 行号或完整详情重建授权边界；非 complete、畸形或身份不一致的事实拒绝办理，不回退旧路线。当前仍保留 System 引擎状态核验；业务决定与办理绑定持久保存数据项指纹及批准边界摘要，System 的 Grant 独立核验并引用同一绑定。旧 native 测试确认及办理通过 owner 事务迁移清理，其他目录及通用审计保留。
- 业务决定的确认人授权版本是当时审计事实，不因无关角色调整自动作废；首次受理由 System 实时核验原账号／Membership 和当前确认权限，Catalog 保证原业务负责人责任关系及来源依据仍有效。同账号重新接任不能复活旧决定；原办理请求仍严格绑定办理人授权版本。不得让普通历史读取重新要求确认人资格，也不得把内部资格测试当成真实 OAuth 双服务 Online T4 已通过。
- `/entries` 的业务域上下文用 `catalog.entry.read` 动态发现 Standard Domain 的名称、编码、定义和层级，仅投影本次响应；独立业务域总览复用同一目录计数，Standard / Model 专业详情仍由 owner 对当前 User Token 判权，不能以 Catalog 运行身份代查。
- 除 System 注册和本模块必需基础设施外，任何业务模块不可达都不能阻止进程启动；Meta / Model / Standard / Service / Develop 同步失败只产生滞后并后台重试，各 owner 使用独立 checkpoint。
- `CatalogEntry` UUID 是企业稳定身份；Meta fingerprint 只是来源身份。
- 首次保存有效 `business_owner` 即建立过业务责任，不等待完成编目。内部 nullable `business_responsibility_established` 只表达历史依据：新建为 false，责任写入同事务置 true，旧历史不足为 NULL；不得因撤销编目、移交、失效或来源变化重置，也不接受请求方编辑。保存责任不改变 System 直接授权许可，不把历史依据单独当作精确源资源的授权依据。业务批准不依赖直接许可；System 直接签发按显式许可优先、持久 Catalog 部署声明兜底的规则判断，未登记或读取失败均拒绝，进程失联不推导允许。
- Catalog 最终核验业务责任及精确业务决定，System 持久提交正式受理回执并唯一写入 Grant；Catalog 不另存第二份可编辑接受状态，也不复制业务决定到 System。接受前移交须重新确认，接受后只允许同一次、同参数办理继续。自动办理窗口使用 System 原受理时间，为 5 分钟；指定到期时间时另受该日期截断，长期有效仍只有 5 分钟；不限制人员操作，重试不得刷新。到期未生效须重新核验并接受，已生效同次重试只返回原结果。仅持本地锁调用 System 无法证明进程故障后的先后；`fulfillment_checks` 是发送前持久待核清事实，数据库保护受影响条目的依据写入，不保存 System 受理时间／截止时间。正式准备、首次受理、受理核清及自动签发续办已接通；同参恢复不重复签发或复活撤销。PG 普通表 single 的业务批准、Manager 真实预览、Deny、解除及撤销，按下文[验证导航](#验证导航)定位场景方法和新旧协议的固定版本证据，不合并为当前 HEAD 或完整链路通过。此范围不代表其他引擎或消费入口已贯通；编目更新、受理及签发历史都不能替代当前实际访问裁决。
- 已弃用条目禁止新的共享确认与办理接受；弃用前已接受的同次办理仍按原窗口继续，已有访问规则单独撤销。来源同步先按 UUID 稳定顺序锁定本批既有条目，再锁 current 来源绑定，与人工维护保持同一锁顺序，不以来源重新出现撤销弃用或清除责任历史。
- `StandardMapping` 是 CatalogComponent 到确定 `Standard.ElementRevision` 的可审核、可追溯关系事实，拥有独立 UUID、并发版本、来源、置信度和审核状态。企业落标关系只归 Catalog；Quality 方案的物理目标与冻结标准来源仅表达检查意图，不构成第二套企业映射，也不以 Catalog 为前置。
- 旧 `component_element_associations` 只在数据库迁移事务中读取：保留历史观察证据并转为未固定修订的 `legacy/proposed` 候选，随后删除旧表。业务 API 和 CatalogEntry 聚合更新只使用独立 StandardMapping；历史候选经人工固定发布修订并审核前，不得视为已落标。
- 不提供 DataItem CatalogEntry 的手工创建和删除 API。
- Model Entity / LogicalTable 的专业内生 Domain、Element、Metric 和建模关系归 Model，Catalog 不建立可编辑副本。
- Standard 五类标准的名称、定义、修订、状态、Domain、分类、单位和语义依赖归 Standard，Catalog 不建立可编辑副本。
- Service QueryService 的 SQL、发布快照、协议、输出契约、Consumer Descriptor、运行状态和端点归 Service，Catalog 不建立可编辑副本；QueryService 没有 owner Domain，primary Domain 仍归 Catalog。
- Develop 只为已持久化的 `query|workflow` DevTask 建立 `development_artifact`；`script` / Notebook、即时查询、execution、运行结果和 ToolApproval 不进入企业目录。DevTask 内容、DAG、参数、Engine 绑定与执行契约归 Develop，Catalog 只保存最小可重建观察摘要并动态解析当前详情。
- Quality 评分、Issue 和 execution 历史不是 CatalogEntry，不进入 Catalog 存储或搜索投影。只对 Meta PostgreSQL table DataItem 使用 owner 提供的 `engine_id + schema_name + table_name` 按需动态解析，不拆分 `full_name` 猜测定位。
- Meta 数据血缘只在 Catalog Frontend 中以当前 User Access Token 动态查询 Meta 唯一图接口，并复用 `common-frontend/graph`；Catalog Backend 不代理、不复制血缘边，Meta 不可达不影响 Catalog 详情和 Ready。
- 数据字典是 Catalog 联邦读模型：只对 active Meta DataItem 组合 Meta 当前物理字段、Catalog 权威且已审核的 StandardMapping 与其中冻结的 Standard ElementRevision，不落表、不使用已观察摘要或动态“当前版本”伪造标准事实。
- 数据字典导出是上述联邦读模型的一次同步 JSON 捕获：服务端重新解析并返回带生成时点和 SHA-256 ETag 的附件，不保存导出任务、文件或第二份事实；批量发布与长期托管不属于该接口。
- Catalog 不提供泛化 `CatalogRelation` 或可配置关系类型；当前唯一自有跨条目关系是弃用条目的可选推荐继任项，只通过治理子资源 `PUT /entries/:id/governance` 维护，使用 CatalogEntry 聚合版本；普通编目更新不得读写它。推荐继任保持两个独立企业身份，不等同 `merged`。

## 业务域总览与五类标准接入

2026-10-22 确认：Standard 五类标准统一按稳定身份进入企业目录，专业定义和修订唯一归 Standard，Catalog 不维护可编辑名称/定义副本。Catalog 业务域总览独立于资源浏览；Catalog、Model、Standard 的域级标准引用只读接口使用各自本地事实，精确域、一跳、按目标去重并保留证据；当前 User 权限判定，不复制专业关系或改写冻结修订。2026-10-28 展示调整为“本域使用的标准”，保留本域、其他业务域、公共及无法确认归属的目标，不在分页后隐藏本域目标。完整契约以[业务域总览与资源浏览](../docs/spec/addp企业资源目录实现规范.md#44-业务域总览与资源浏览)、[Standard 五类专业资源变化与动态引用](../docs/spec/addp企业资源目录实现规范.md#55-standard-五类专业资源变化与动态引用)为准。

## 验证导航

确定性门禁沿[测试与验收标准入口](../docs/spec/addp测试与验收规范.md#三标准入口)执行；Catalog owner 使用 `make test-module MODULE=catalog`。下表仅定位已有方法和记录，不表示当前 HEAD 已运行通过；失败、未执行和未覆盖项按各记录保留，部分页面或接口通过不能替代整轮结果。

| 验证目的 | 方法与证据正文 |
| --- | --- |
| 企业目录发布、生命周期与共享受理 | [场景方法](../docs/spec/testing/scenarios/授权与共享.md#发布生命周期与共享受理)、[OAuth 自动签发的只读观察](../docs/spec/testing/scenarios/授权与共享.md#oauth-自动签发的只读观察)：专用 macOS profile，区分受理、唯一签发与源数据实际读取。 |
| PG 业务批准与实际读取 | [`pg-source-authorization` 的业务批准闭环](../docs/spec/testing/scenarios/授权与共享.md#catalog-业务批准闭环)、[Catalog 离线与恢复](../docs/spec/testing/scenarios/授权与共享.md#catalog-可选依赖离线与恢复)；[旧协议与新成果证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#pg-dataitem-源授权与-transfer-新成果初始授权--hosted-t4)、[直接许可模型切换后的固定版本证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#直接授权许可模型替换--pg-hosted-t4-与开发库迁移)。 |
| 后续 PG 浏览器扩展的局部结果 | [许可提交与冲突恢复记录](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#直接许可浏览器提交与冲突恢复--pg-hosted-t4-局部通过)：页面局部通过、后续在途到期检查导致整轮失败；保留未执行项与清理证据。 |
| 业务域总览 Hosted API | [三类身份、跨域引用与清理方法](../docs/spec/testing/scenarios/授权与共享.md#catalog-domain-overview)、[固定源码成功记录](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#catalog-业务域总览--hosted-api-t4)、[首次失败与修复](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#catalog-业务域总览--首次失败与修复)；不含浏览器 UI T4 或全量跨域关系。 |
| 本地 Opera 普通用户复验 | [账号、跨域样本与页面断言](../docs/spec/testing/scenarios/授权与共享.md#catalog-本地普通用户浏览器复验)、[隔离入口、租约及结束判定](../docs/spec/testing/T4-Online验收规范.md#catalog-本地隔离浏览器验收)：本机技术证据，与 Hosted API 分别认定，页面证据接收后仍须源标准复核及清理。 |
