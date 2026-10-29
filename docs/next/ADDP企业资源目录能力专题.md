# ADDP 企业资源目录能力专题

更新时间：2026-10-29

本文只保留企业资源目录及相关源数据授权主线的当前能力边界、未决设计和剩余验收。稳定概念、实现契约和操作步骤以正式文档为准；不继续追加逐轮过程记录、临时日志、运行实例清单或已完成阶段的重复清单。

## 一、正式文档入口

| 内容 | 唯一维护位置 |
| --- | --- |
| Enterprise Catalog、CatalogEntry、Engine Catalog、责任与授权术语 | [术语表](../concepts/addp术语表.md) |
| 企业身份、专业事实所有权、组织协作、目录与资产边界 | [企业资源目录体系图](../concepts/addp企业资源目录体系图.md) |
| 自动建档、来源绑定、生命周期、StandardMapping、业务域浏览、联邦读模型与 Catalog API | [企业资源目录实现规范](../spec/addp企业资源目录实现规范.md) |
| 直接授权许可、业务确认、受理、Grant/Deny、委托及当前访问检查 | [授权上下文规范](../spec/addp授权上下文规范.md#55-目录责任业务授权决定与源数据授权) |
| Owner 保护投影、在途屏障、外部索引持久投递与恢复 | [数据安全与隐私保护实现规范](../spec/addp数据安全与隐私保护实现规范.md)第九节 |
| Meta 技术投影、与正文并存、字段检索范围及历史集合补索引 | [元数据扫描机制规范](../spec/addp元数据扫描机制规范.md#dataitem-技术元数据搜索投影) |
| Manager 后台采样、完整来源、执行授权、结果提交与读取 | [数据剖析规范](../../manager/docs/数据剖析规范.md) |
| 搜索备份、独立导入、冻结、切卷及回滚 | [Infra 说明](../../scripts/infra/docs/备份恢复与搜索迁移.md#旧-meilisearch-卷迁移与回滚) |
| 分层验证、标准入口、隔离环境和 CI 责任 | [测试与验收规范](../spec/addp测试与验收规范.md) |

专业模块的 `CLAUDE.md` 提供代码入口和 owner 特有范围，不能替代上述规范。业务样本、指标口径及语义查询推进继续由 [Outdoor 业务数据治理推进方案](Outdoor业务数据治理推进方案.md) 维护，不在本专题复制。

## 二、当前能力边界

目录主线保持：Meta 持久化 DataItem → Catalog 自动建立企业身份并维护业务关联与责任 → Asset 组合、发布 → Portal 消费。来源变化按 owner 日志与独立 checkpoint 恢复；Manager 提供技术资源浏览、预览、剖析与内容检索。旧 discoverable、Meta AssetRecord、Asset 自动发现及通用 source_reference 路线已删除，不恢复双轨来源。

| 能力 | 当前边界 |
| --- | --- |
| [企业目录基础](../spec/addp企业资源目录实现规范.md) | 企业身份与专业来源独立，治理状态、目录可见性和搜索投影不混同。 |
| [组织与治理](../spec/addp企业资源目录实现规范.md#32-责任关系) | 责任治理、个人关系、项目组集合与覆盖率各有独立边界；批量分配仍只针对当前页显式成员，不新增统一 Workspace。 |
| [专业资源与关系](../spec/addp企业资源目录实现规范.md#53-其他-owner-读取契约) | 接入类型与稳定身份以 owner 契约为准，当前事实联邦读取，不复制专业定义或关系边。 |
| [标准映射](../spec/addp企业资源目录实现规范.md#31-standard-语义关联)与[字典](../spec/addp企业资源目录实现规范.md#514-数据字典联邦读模型) | 人工候选、独立审核和冻结修订按正式契约执行；人工接口可用不表示 Copilot 已接入，真实业务闭环仍见剩余工作。 |
| [源数据授权](../spec/addp授权上下文规范.md#555-直接授权许可业务批准与明确交接) | System 唯一维护精确 DataItem 的直接许可、Grant 与 Deny；业务批准不依赖直接许可，两种来源共用唯一签发与读取裁决。责任、功能 Permission、管理资格和内容读取权分别核验；许可不是 Grant，不恢复旧初始化或切换模式。 |
| [Manager 读取与剖析](../../manager/docs/数据剖析规范.md#当前授权切换边界) | 剖析执行及结果归 Manager，使用独立 execution 源授权；普通单表首版范围及排除项见规范，预览通过不替代后台剖析验收。 |
| [搜索与结构命中](../spec/addp元数据扫描机制规范.md#dataitem-技术元数据搜索投影) | 技术与正文分别幂等更新、相互保留；原专题记录 MongoDB 漏投递及真实命中字段展示已完成功能验收，不据此外推当前版本或全部引擎。所有 Catalog 扫描与刷新策略接入技术投影，不等于历史全部补录或全部非表结构可搜索。 |
| [搜索基础设施](../../scripts/infra/docs/备份恢复与搜索迁移.md#旧-meilisearch-卷迁移与回滚) | 新版本独立导入和正式新卷切换已完成；旧卷、旧镜像与备份仍保留。迁移成功不替代 Owner 权限、保护或外部服务故障验收。 |

以上是能力定位，不是当前 HEAD 或全平台通过声明。PG 授权按[许可模型替换后的固定版本记录](../spec/testing/evidence/数据保护与执行血缘历史证据.md#直接授权许可模型替换--pg-hosted-t4-与开发库迁移)及[后续浏览器局部通过与整轮失败](../spec/testing/evidence/数据保护与执行血缘历史证据.md#直接许可浏览器提交与冲突恢复--pg-hosted-t4-局部通过)分别认定；旧协议不替代新协议，局部成功不抵销失败。

业务域的 Hosted API、本机 Opera、后续布局与 Quality 摘要修改的 T3／固定提交 CI 各有独立证据，统一沿[Catalog 验证导航](../../catalog/CLAUDE.md#验证导航)及[业务域历史证据](../spec/testing/evidence/数据保护与执行血缘历史证据.md#catalog-业务域总览--hosted-api-t4)查阅，不合并为当前版本完整 T4。

## 三、剩余工作

跨模块缺口保留在本清单，Owner 的实现约束、恢复协议和专项验收沿各条链接维护，不在本专题复制。

### 3.1 企业目录与标准映射

[域级覆盖率与缺口下钻](../spec/addp企业资源目录实现规范.md#44-业务域总览与资源浏览)已纳入现行契约，不再列为未实现；仅在获准的盘点视图按当前用户权限和精确域计算，不将全局计数改名为域统计。

- [ ] 在专用环境完成 StandardMapping“人工候选 → 审核 → 已审核字典 → 认证时映射版本证据”的真实业务闭环，不在共享开发库为填满页面制造业务事实。
- [ ] **Standard／Catalog：**确认[指标一跳关系契约](../spec/addp企业资源目录实现规范.md#510-model--standard-专业关系查询契约)的边类型：规范为 `standard.metric.base_metric/dependency`，[当前生成器](../../standard/backend/internal/service/professional_relation_service.go)输出 `standard.metric.base/component`，[Catalog 详情](../../catalog/frontend/src/views/EntryDetail.vue)仍按旧名匹配，未匹配时原样显示机器类型。确认后先统一一跳图规范，再同步显示与回归，不保留兼容别名。域级标准引用使用 `standard.metric.dependency` 与 `position=base|component` 的独立契约，不跟随一跳图全局替换。现有 SQLite 用例只直接断言 `base` 边名，不能计为两类边显示、契约对齐或真实页面验收。
- [ ] Copilot 提交候选尚未接入；先确定真实 Service Principal、候选权限与证据契约，AI 只能提交候选，不能直接审核为权威映射。
- [ ] 使用普通读者、治理者及有 Project Group membership 的真实身份，核对默认入口、盘点权限、我的目录、项目集合名称与菜单优先级；不以管理员账号替代全部角色验收。
- [ ] 补验至少两个业务域、零资源域、切域后的迟到响应，以及 Standard/Quality 权限不足或不可达时的独立区域状态；核对 Model/Quality 专业导航，不把局部只读点击记为完整验收。
- [ ] 域级反向关系汇总和关联已发布资产暂不实施。需要这些能力时先确认 owner 查询、权限、适用对象、子域和跨域计数口径，不按同域共现推断关系或把全局数字改名。

### 3.2 源数据授权与正常使用闭环

PG 普通单表的直接签发、接收方读取、Deny／收权和原命令恢复已有上述固定版本证据，不再重复列为全新待办；不外推其他引擎、multi／whole、列／行授权或真实持久部署默认切换。实际功能权限和引擎管理资格仍由有权主体显式安排，不从角色名推导。

- [ ] **Manager：**按[剖析授权与结果读取契约](../../manager/docs/数据剖析规范.md#当前授权切换边界)，使用同一接收账号验证已授权表的剖析创建、Worker 执行及结果查询，再核对未授权表、到期、撤销与 Deny 拒绝。已有预览允许／拒绝证据不代替后台剖析验收。
- [ ] 在既有 `enterprise-catalog-publishing` 中完成 Catalog 业务确认 → System 首次受理 → 自动签发 → 原参数恢复的真实 OAuth 验收，分别报告决定、受理、签发及实际读取，不将历史签发显示为当前 Allow；PG suite 已有 OAuth 单表闭环不代替此专用发布场景。
- [ ] **各内容读取 Owner／System：**Develop 完整 QueryReadSet、扫描执行、Quality、Transfer、其他引擎及其他内容出口按[源授权契约](../spec/addp授权上下文规范.md#55-目录责任业务授权决定与源数据授权)逐项贯通；没有完整范围与运行凭据证明前，不借整库连接或 Service 身份放行。
- [ ] **Common Provider／Manager：**PostgreSQL 准备后源端并发 DDL 的增强沿[剖析首版排除范围](../../manager/docs/数据剖析规范.md#当前授权切换边界)推进，不作为普通单表体验前置，不宣称已证明原生事务内依赖冻结。

### 3.3 搜索覆盖与故障恢复

- [ ] **Meta／Manager：**按[技术投影与历史补录](../spec/addp元数据扫描机制规范.md#dataitem-技术元数据搜索投影)盘点其他引擎历史 DataItem 的正式投递覆盖。缺少投递记录不直接证明索引无文档；补录仍须明确范围、成本和正式入口，不自动扩大扫描或重建索引。
- [ ] **Meta：**Neo4j 图标签、关系和属性尚未进入[结构检索投影](../spec/addp元数据扫描机制规范.md#技术投影的检索消费与覆盖边界)。有真实需求后先定义非表事实白名单与命中解释，不套用关系表字段假设。
- [ ] **Meta：**原专题记录 NFS 手动扫描失败 `meta.execution.lease_expired_recovery_required`，精确恢复证据仍待补。按[扫描 Worker 与租约边界](../../meta/CLAUDE.md#服务身份与-system-调用)核对 execution、Worker 租约、部分元数据及投递，再确定重试范围；不把失败视为成功，不直接全量重扫。
- [ ] **Manager／Security：**仍需真实“已接收但响应丢失 + 进程退出”的隔离故障验收，恢复协议唯一见[投影变化流与 Owner 确认](../spec/addp数据安全与隐私保护实现规范.md#九投影变化流与-owner-确认)。须认回同一原任务、提交次数不增加，历史不足或冲突继续隔离；HTTP 夹具和 PostgreSQL T2 不替代此 T4。
- [ ] **Manager／Catalog／Asset：**各自补齐当前租户、可见性、上架及保护负例；不互相代验。[Asset 搜索读取约束](../../asset/CLAUDE.md#api-与数据)保留索引估计总数及权威复核后不足一页的限制，可靠普通投递与精确分页仍由 Asset 独立推进，不扩大返回范围。
- [ ] **各数据面 Owner／Security：**多 Backend／HA 回执与恢复认证沿[Owner 确认契约](../spec/addp数据安全与隐私保护实现规范.md#九投影变化流与-owner-确认)独立安排，不从单实例运行成功外推。

### 3.4 相关消费链路

- [ ] **Agent／Ontology：**搜索零召回已解除，“先建任务、不要运行”的完整 Transfer 创建仍待验收。原专题的 `platform_condition_unsatisfied:platform_context` 保留为历史失败；后续[原参数创建记录](../../agent/CLAUDE.md#原参数创建修订-21)已转入确认恢复的重复复核问题，仍未实际创建。当前 Runtime 职责及验收由该 Owner 主线维护，不扩大 Meta／Manager 修复；未来语义边界见[Ontology 后续设计](ADDP%20Ontology最小设计契约.md)。
- [ ] **Outdoor 主线与执行 Owner：**语义查询、计划编译和指标执行的完整用例统一见[Outdoor 剩余闭环](Outdoor业务数据治理推进方案.md#尚未闭合的工作)。Catalog 仍只承担企业身份、语义关联、治理及发现，不复制指标定义、代替语义执行或提前新增 Semantic Runtime。

## 四、验收与维护方式

目录发布与生命周期沿[既有专用场景](../spec/testing/scenarios/授权与共享.md#发布生命周期与共享受理)取得证据；PG 源授权和业务域总览各用[Catalog 验证导航](../../catalog/CLAUDE.md#验证导航)中的原标准入口，不混用 suite、身份或结果。停机追赶、来源 missing、显式重绑、System 恢复、owner 不可达及外部索引故障只在明确的隔离环境和操作窗口验证，不接管共享开发服务。

- 工作区总门禁使用 `make test-changed`；明确 owner 时使用 `make test-module MODULE=<模块>`，局部门禁通过不等于总门禁通过。
- PostgreSQL 测试先运行 `bash scripts/infra/status.sh` 核对实际端口，标准入口只使用 `addp_test` 或 `addp_iam_test`；专用 T4 使用已登记的 User、Tenant 和受控夹具，不擅自改变真实账号或业务资源。
- 按“通过、失败、未运行”分别交付结果。总门禁失败或测试条件不齐时不得宣称工程收口；以当前工作区复验，历史日志和某轮已通过结果不作为新改动的统一证据。
- 实际扫描、补索引、源授权配置、业务编目写入及故障操作先明确对象和影响范围。服务重启由用户操作；检索修复不包含 Transfer 执行或 Outdoor 业务数据写入。

后续只更新能力定位与未完成项：稳定规则进入正式文档，完成项移出待办；固定源码、失败、清理与未覆盖结果保存在现有[历史证据专题](../spec/testing/evidence/数据保护与执行血缘历史证据.md)，交付和 CI 只报告各自实际范围，不再按日期追加过程长文。
