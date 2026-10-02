# Catalog 模块开发说明

Catalog 是企业资源目录的唯一事实源，负责稳定目录身份、来源绑定、业务语义关联、字段/组件标准映射、责任关系、目录可见性和治理状态。

## 必读文档

- `docs/concepts/addp企业资源目录体系图.md`
- `docs/spec/addp企业资源目录实现规范.md`
- `docs/concepts/addp术语表.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp新模块开发指南.md`

## 边界

- 不复制 Meta、Model、Standard、Service、Develop、System 的完整专业事实；只保存目录身份、失效解释、列表和搜索所需的最小已观察投影，完整专业详情动态读取 owner。
- 不向 Meta 或 Standard 回写 Catalog ID 或关联投影。
- 责任关系不自动授予 IAM Role、数据访问或授权办理资格。已确认的目标规则以 `docs/spec/addp授权上下文规范.md` 5.5 为准：Catalog 提供当前责任资格及业务入口，System 引擎访问控制领域唯一维护源数据规则；不在 Catalog 新建 Grant 副本，不以 Catalog 可达性阻塞已有合法数据访问或其他模块 Ready。普通只读共享的同人确认／办理及责任移交边界已确认，表级贯通尚未实现。
- 业务共享确认和源读取授权办理使用独立功能 Permission，首版仅通过角色显式分配，不默认授予内置管理员，不从编目权限、责任身份或引擎管理委派推导。Permission 与真实消费入口同时发布，不能先启用占位权限。
- `POST /entries/:id/sharing_decisions` 已提供普通只读业务确认生产入口，须同时具备条目读取与 `catalog.sharing_decision.create`，并是当前业务负责人；本人或所在项目组受益可以显式确认。首版只支持 Meta 当前扫描的数据库 table，以实时祖先链和 System 引擎层级能力核对完整目标，不拆 `full_name`、不读取源数据。每次必须显式选择 `expiry_mode=at_time|until_revoked` 并填写用途：前者填写未来绝对到期时间（微秒精度，无统一天数上限），后者无到期日期；不默认永久、不使用日期哨兵。`sharing_decisions` 是不可修改的决定历史，不是 Grant 或受理回执；同参重试必须匹配模式及日期，恢复原记录。该 API 尚无前端入口，System 正式消费、受理与实际授权仍待贯通。
- `lockCurrentSharingDecisionBasis` 只在 Catalog 本地事务中读取持久决定、按条目→当前来源→原责任关系锁定并核验当前依据；业务确认创建在提交前复用该核验。历史读取不调用它，普通名称／说明修改不使决定仅因聚合版本增加而失效；换源、原责任失效或同账号重新接任不能复活旧决定。它不核验 System 当前身份／功能权限／委派，不调用远端、不建立待核清、不构成受理依据或跨模块可信接口；后续办理消费者须补齐这些门禁后使用。
- `GET /entries/:id/sharing_decision_candidates` 提供另一名人类办理人的分页摘要：当前条目可见、`catalog.entry.read` 与独立 `system.engine_access_fulfillment.create`，再由 System 使用本次 User Bearer 实时核验有效引擎管理委派。远端调用不持有 Catalog 锁，返回后在新的只读快照复查可见性、当前来源、原责任关系和期限。摘要不包含业务用途正文，不是受理依据，不创建待核清保护、受理或 Grant；原确认人本人历史接口不放宽。该接口尚无前端消费者，正式准备／新受理仍未接通。
- `prepareSharingFulfillment` 是内部事务登记原语：首次登记复用当前决定依据核验，从持久决定派生业务参数，并不可变绑定本次调用主体、原操作账号来源、批准要求版本和完整目标。同参重试返回原待核清记录；已核清记录不重新打开，不建立 System 结果副本。调用方须在事务提交后发送。当前没有生产办理消费者，该原语不证明输入可信或办理资格，不作为新的公开 API、Permission 或自动后台发送入口。
- `reconcileSharingFulfillment` 仅消费已提交的原待核清记录，事务外先查询，明确未找到才关闭原请求；关闭可能返回已受理结果。完整绑定一致且结果为 accepted／closed 后，条目→待核清记录锁内填写一次本地数据库核清时间。错误或未知结果保留保护，历史恢复不要求原责任／操作人／期限仍有效，不恢复新办理资格。生产恢复已接通 `common/client.SystemFulfillmentClient` 与 `SharingFulfillmentReconciliationRunner`：仅 `addp-catalog` 的当前 Tenant Service 身份及独立 `system.engine_access_fulfillment.execute` 可调用 System Runtime 核清／关闭。至少等待一分钟后逐项核清，沿用来源同步周期，每批 100 条游标推进、单项远端调用限时 30 秒；失败保留保护且不门控 Ready。普通 404、鉴权、通信及解析失败不得当作未找到。此链路不创建新受理或 Grant，首次准备／受理仍未发布。
- 待核清目标的有效叶子、正 int64 Engine ID、64 个 segment 和完整 JSON 16 KiB 边界只由 `common/authorization.EncodeSharingTarget` 校验，与 System 仲裁复用；首次登记前拒绝不满足该边界的目标，不先冻结条目。节点身份完整保留，无效 UTF-8 不允许被 JSON 替换字符静默修正。该纯值校验不代替真实来源、认证和办理资格，也不改变现有业务确认 API。
- 跨模块只走公开 API 和 Tenant Service Access Token，不跨 Schema 查询。
- 业务决定的确认人授权版本是当时审计事实，不因无关角色调整自动作废；首次受理由 System 实时核验原账号／Membership 和当前确认权限，Catalog 保证原业务负责人责任关系及来源依据仍有效。同账号重新接任不能复活旧决定；原办理请求仍严格绑定办理人授权版本。不得让普通历史读取重新要求确认人资格，也不得把内部资格测试当成已接通可信业务依据接口。
- `/entries` 的业务域上下文用 `catalog.entry.read` 动态发现 Standard Domain 的名称、编码、定义和层级，仅投影本次响应；独立前端业务域页已删除，Standard / Model 专业详情仍由 owner 对当前 User Token 判权，不能以 Catalog 运行身份代查。
- 除 System 注册和本模块必需基础设施外，任何业务模块不可达都不能阻止进程启动；Meta / Model / Standard / Service / Develop 同步失败只产生滞后并后台重试，各 owner 使用独立 checkpoint。
- `CatalogEntry` UUID 是企业稳定身份；Meta fingerprint 只是来源身份。
- 首次保存有效 `business_owner` 即建立过业务责任，不等待完成编目。内部 nullable `business_responsibility_established` 只表达历史依据：新建为 false，责任写入同事务置 true，旧历史不足为 NULL；不得因撤销编目、移交、失效或来源变化重置，也不接受请求方编辑。不把它单独当作精确源资源的授权依据。
- Catalog 最终核验业务责任及精确业务决定，System 持久提交正式受理回执并唯一写入 Grant；Catalog 不另存第二份可编辑接受状态，也不复制业务决定到 System。接受前移交须重新确认，接受后只允许同一次、同参数办理继续。自动办理窗口使用 System 原受理时间，为 5 分钟；指定到期时间时另受该日期截断，长期有效仍只有 5 分钟；不限制人员操作，重试不得刷新。到期未生效须重新核验并接受，已生效同次重试只返回原结果。仅持本地锁调用 System 无法证明进程故障后的先后；`fulfillment_checks` 是发送前持久待核清事实，数据库保护受影响条目的依据写入，不保存 System 受理时间／截止时间。核清恢复消费者已接通；可信请求的正式准备、首次受理及实际 Grant 仍待贯通，不能用编目更新或只读查询替代接受动作。
- 已弃用条目禁止新的共享确认与办理接受；弃用前已接受的同次办理仍按原窗口继续，已有访问规则单独撤销。来源同步先按 UUID 稳定顺序锁定本批既有条目，再锁 current 来源绑定，与人工维护保持同一锁顺序，不以来源重新出现撤销弃用或清除责任历史。
- `StandardMapping` 是 CatalogComponent 到确定 `Standard.ElementRevision` 的可审核、可追溯关系事实，拥有独立 UUID、并发版本、来源、置信度和审核状态。企业落标关系只归 Catalog；Quality 方案的物理目标与冻结标准来源仅表达检查意图，不构成第二套企业映射，也不以 Catalog 为前置。
- 旧 `component_element_associations` 只在数据库迁移事务中读取：保留历史观察证据并转为未固定修订的 `legacy/proposed` 候选，随后删除旧表。业务 API 和 CatalogEntry 聚合更新只使用独立 StandardMapping；历史候选经人工固定发布修订并审核前，不得视为已落标。
- 不提供 DataItem CatalogEntry 的手工创建和删除 API。
- Model Entity / LogicalTable 的专业内生 Domain、Element、Metric 和建模关系归 Model，Catalog 不建立可编辑副本。
- Standard Metric 的定义、公式、状态、Domain、分类、单位、数据元映射和依赖关系归 Standard，Catalog 不建立可编辑副本。
- Service QueryService 的 SQL、发布快照、协议、输出契约、Consumer Descriptor、运行状态和端点归 Service，Catalog 不建立可编辑副本；QueryService 没有 owner Domain，primary Domain 仍归 Catalog。
- Develop 只为已持久化的 `query|workflow` DevTask 建立 `development_artifact`；`script` / Notebook、即时查询、execution、运行结果和 ToolApproval 不进入企业目录。DevTask 内容、DAG、参数、Engine 绑定与执行契约归 Develop，Catalog 只保存最小可重建观察摘要并动态解析当前详情。
- Quality 评分、Issue 和 execution 历史不是 CatalogEntry，不进入 Catalog 存储或搜索投影。只对 Meta PostgreSQL table DataItem 使用 owner 提供的 `engine_id + schema_name + table_name` 按需动态解析，不拆分 `full_name` 猜测定位。
- Meta 数据血缘只在 Catalog Frontend 中以当前 User Access Token 动态查询 Meta 唯一图接口，并复用 `common-frontend/graph`；Catalog Backend 不代理、不复制血缘边，Meta 不可达不影响 Catalog 详情和 Ready。
- 数据字典是 Catalog 联邦读模型：只对 active Meta DataItem 组合 Meta 当前物理字段、Catalog 权威且已审核的 StandardMapping 与其中冻结的 Standard ElementRevision，不落表、不使用已观察摘要或动态“当前版本”伪造标准事实。
- 数据字典导出是上述联邦读模型的一次同步 JSON 捕获：服务端重新解析并返回带生成时点和 SHA-256 ETag 的附件，不保存导出任务、文件或第二份事实；批量发布与长期托管不属于该接口。
- Catalog 不提供泛化 `CatalogRelation` 或可配置关系类型；当前唯一自有跨条目关系是弃用条目的可选推荐继任项，只通过治理子资源 `PUT /entries/:id/governance` 维护，使用 CatalogEntry 聚合版本；普通编目更新不得读写它。推荐继任保持两个独立企业身份，不等同 `merged`。
