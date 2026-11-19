Service SQL 角色方式（2026-11-03）：已按[查询发布与执行规范](../service/docs/architecture/查询发布与执行.md)接入非联邦、Provider 可证明唯一普通表的直接字段及别名；候选统一为 POST，保存仅准备元数据并复用保护编译器，实际执行与交付核验当前本人角色。Common 保留资格失效与方式失效的具体原因，不改变授权规则。默认保护及已有原生表方式保持原契约。

SQL资格生命周期Hosted（2026-11-04）：唯一Run41796322974／Job126461181063／attempt1、源码2b752ee0ed69c4fcfe3a5875735ad3385ac0947b已失败，官方原件及冻结源码已独立核验。失败在初始化consumer请求PG整引擎扫描，System管理范围核验403；领域／Online／浏览器报告未生成，SQL资格及其余业务未执行，不记断言通过或失败。生命周期清理与Infra零残留通过，业务域清理未进入。用户已确认，扫描夹具适配已按v42实现：独立准备User仅补两项扫描功能完成basic初识别，业务User只刷新原七表内的精确item_id，不增加消费权限或改产品规则。System Go、所属202项回归及登记CLI／针对性方法复验通过。完整入口的早期失败保留记录；随后以已提交main基线3ff120a0de3d40002b0a0ef7b9a6e4961ae37552加本轮精确差异的临时冻结副本执行make test-security-online-runner，41／201／18／142／34／80／242项及登记CLI全部通过，退出0，不覆盖其他会话WIP。未重派Hosted，不需本地重启。v42独立报告审计器8组离线测试不代替实际产物。事实与修复范围统一见[Service资格生命周期证据](../service/docs/testing/查询与要素协议.md#sql-角色资格生命周期隔离断言2026-11-04)。

字符串默认值修订已接入v41（2026-11-04），限定本地#33／MySQL真实HTTP及独立审计通过：10项／42条受控响应／11组JSON及CSV，int默认保持，string默认MM→GG后的脱敏输出、非法更新不变性、旧游标拒绝和两方向续页通过；临时#14带version2删除404，#7不变、已注销。Service Go、PostgreSQL11顶层、所属Runner180项、临时夹具5组及完整make test-security-online-runner（41／80／198／188／388项及登记CLI）验证仍有效。Hosted未运行，活动角色方式／资格生命周期及其他引擎不由本轮覆盖。事实统一见[Service测试正文](../service/docs/testing/查询与要素协议.md#字符串默认值修订的验收接入v412026-11-04)，索引`/tmp/p145-string-default-20261104/verification.json`。

允许值列表int默认修订的限定本地#33／MySQL真实HTTP及独立审计通过：10项／43条受控响应／11组JSON及CSV；临时#13带version2删除404、#7不变、已注销。该历史证据不代替string默认修订的真实HTTP，详细范围统一见[Service测试正文](../service/docs/testing/查询与要素协议.md#允许值列表默认修订的验收接入v402026-11-04)。

新默认续页Runner v39已接入（2026-11-04）：同一五行合成来源以2＋2／4行参考，固定string默认仅用于普通谓词，int默认1→2；两方向参数省略／显式续页、CSV分页头、改int/string拒绝均沿单一服务／Policy及精确清理，领域v39／浏览器v9，资源数不变。所属174项通过；完整总入口在未修改TiDB部署夹具处20秒超时、退出2，未记通过；根Makefile原定Security组380项独立退出0，登记CLI退出0；不替代完整总入口通过，未执行的Graph/Cleanup等阶段不计通过。固定摘要及日志见`/tmp/p145-hosted-pagination-20261104/verification.json`。规范与证据统一见[Service测试正文](../service/docs/testing/查询与要素协议.md#新默认续页的隔离runner接入v392026-11-04)；实际Hosted未运行，CI专门会话跟进，无需重启。

新默认续页本地HTTP独立审计通过（2026-11-04）：#33/addp-web/MySQL #3，9项检查／36条受控响应／9组JSON与CSV；两种省略／显式参数续页与更新前同参数第二页CSV一致，两页逐行等于10行参考、稳定排序不重叠，逐页版本／has-more头一致；旧游标4次、新游标改参数4次均400/no-store，脱敏和CSV空单元格保持。#12按version2删除404、#7不变、已注销。事实与边界统一见[Service测试正文](../service/docs/testing/查询与要素协议.md#已发布sql默认值修改2026-11-04)；索引verification.json的new_default_pagination_passed。仍是限定本地默认保护，不是活动角色方式或Hosted证据；已按上段v39接入既有隔离Runner，实际Hosted仍由专门会话跟进。

SQL默认值本地HTTP独立审计通过（2026-11-04）：#33/addp-web/MySQL #3，正式可选int默认1→2更新，7项检查／24条受控响应／5组第一页JSON及CSV，旧游标两格式四次400、非法更新三次400、版本冲突409、双别名默认掩码及CSV空单元格均核对。#11按version=2删除404，#9/#10前后清理证据闭合，#7不变、已注销。前两轮临时夹具失败及修正保留在[唯一测试正文](../service/docs/testing/查询与要素协议.md#已发布sql默认值修改2026-11-04)，不再代表当前状态；索引`/tmp/p145-published-default-edit-20261104/verification.json`。该轮只验默认保护／第一页；上段已补续页结果，仍不扩展为角色选择、string默认修改或Hosted已验。

Service SQL发布默认值修改（2026-11-04，v38）：用户已批准仅修改可选参数默认值。正式版本PUT及真实编辑页已接入，SQL和其他声明只读，拒绝／事务／保护边界沿[Service规范](../service/docs/architecture/查询发布与执行.md#已发布-sql-参数默认值修改)。真实PG存储与源读取门禁、Go T1、Service前端单元／构建及中英文受控浏览器已通过；隔离Runner新增默认1→2的旧游标拒绝／新默认输出，沿[v38合同](../scripts/docs/Online数据保护.md#v38发布默认值修订与旧游标拒绝)。完整`make test-security-online-runner`退出0（41／80／198／185／376项及登记CLI），所属171项通过；Service前端120单元／构建通过，12项受控浏览器通过（4项中英文角色／默认编辑、8项SQL创建／草稿回归）。固定文件摘要与验证日志见`/tmp/p145-published-default-edit-20261104/verification.json`；限定本地正式HTTP结果已在上段补录，新增Hosted仍未验，CI由专门会话跟进。

本轮 Go T1、Service PostgreSQL 11 项及 Common 投影存储 PostgreSQL 子门禁 8 项、Service 119 单元／构建和相关 10 项受控浏览器回归通过；不是 Common 全部 PostgreSQL 门禁或个人环境／Hosted T4。全 Console 门禁仍有非本功能导航失败，未计通过。证据 `/tmp/p145-service-sql-role-20261103/verification.json`；用户已全量重启；本地 #33 的默认／全遮盖两页 JSON／CSV、方式撤销拒绝及主动默认恢复合并独立审计通过，共十二项和六份实际 CSV；恢复文件与最初默认文件逐字节一致。#7 最终私有、停用、空选择，临时策略保持 revoked，会话注销，原失败报告保留；不外推新 SQL Hosted/T4。详见 [SQL 单表角色方式本地验收](../service/docs/testing/查询与要素协议.md#sql-单表角色方式本地验收2026-11-03)。CI 仍由专门会话跟进。当前状态与剩余边界统一维护于[后续工作专题](../docs/sessions/数据脱敏与保护后续工作专题.md)。

Service SQL 隔离套件（2026-11-03，历史 v34）：原生表和新增 SQL 双别名复用同一角色方式生命周期；默认两页 JSON／CSV → 保存源字段选择 → 全遮盖 → 方式撤销后新请求／旧游标精确拒绝 → 保留选择保存／重新启用拒绝 → 显式移除后逐字节恢复初始 CSV。只使用既有隔离合成表、普通 User 与正式 Assessment，不新增权限或部署；本人 Membership／授权版本及 Baseline 不变，所有成功、失败和中断精确清理。CI 登记同时约束版本、SQL 派发、报告字段及新增 Service 的创建／删除计数；浏览器 v9 不扩展。

v34 当时通过本地 `make test-security-online-runner`（41／80／197／180／347 项）、所属 Runner 147 项（末次报告字段去重后补跑）及登记校验，证据 `/tmp/p145-service-sql-hosted-runner-20261103/verification.json`；这只是确定性 Runner 门禁，新 v34 Hosted/T4 未运行，不自行派发、查询、重跑或定时。历史断言见 [SQL 双别名隔离断言](../scripts/docs/Online数据保护.md#v34sql-双别名撤销与默认恢复)。

Service SQL 命名参数（2026-11-04，历史 v35）：该阶段领域报告为 v35，保留原生表和无参数 SQL 场景，新增一个必填、无默认值的整数参数 SQL 场景，共用既有生命周期和精确清理。三阶段都验证参数值改变后的真实过滤结果、JSON／CSV 两页、双别名、Unicode、NULL；旧游标改参数、缺参、多余参数、非法标量必须为 400/`invalid_structured_query`，合法参数下方式撤销仍为 409/`query_protection_choice_invalid`，不可混算。保存不持久化运行取值、不构造 Meta Source；登记约束派发、报告及新增服务计数，浏览器仍为 v9。标准 `make test-security-online-runner` 的 41／80／197／182／357 项及登记检查通过，所属 Runner 155 项通过；本轮仅脚本及文档，不需重启。证据 `/tmp/p145-service-sql-parameters-20261104/verification.json`，断言见 [命名参数隔离合同](../scripts/docs/Online数据保护.md#v35sql-输入命名参数与游标绑定)。这些确定性结果不能替代尚未运行的 v35 Hosted/T4。

同轮复用当前浏览器本人 #31（管理员，Tenant1），在既有 MySQL Persons 上新建私有参数服务 #8，限定必填 string 参数 `minimum_id`：三阶段各两页及另一参数值的 JSON／CSV，独立审计 46 次实际查询响应、9 份五行 CSV、24 次参数错误 400、4 次方式撤销 409，全部 `no-store`；参数实际过滤、旧游标绑定和默认恢复逐字节相等均通过。#8 最终 inactive/version14、私有、空选择；临时策略 revoked/version10，原服务 #6/#7、角色及授权版本不变。两次临时审计失败均保留，分别修正抑制字段可省略／观测时间不能视作资格变更，以及 JSON 对象键序不能视作配置改变；不修改产品契约。最终响应头仅对两次角色观察间完整的46次请求认定，较早网络事件已被浏览器缓冲淘汰，不追认失败轮完整头证据。报告和已目视核对的停用页面位于 `/tmp/p145-service-sql-parameters-local-20261104/`，独立命令 `python3 /tmp/p145-service-sql-parameters-local-audit.py` 通过。不导出令牌，不将管理员 string 参数结果替代普通 User／整数参数的 Hosted，未变更本地用户角色、原值／源授权、源数据或本地进程。

Service SQL 资格生命周期（2026-11-04，历史v36）：该阶段领域报告v36。在既有资格撤销、自然到期场景各新增一个私有必填int参数SQL双别名服务；保留原生表服务，共享同一次IAM变更／90秒真实等待、同一普通User及有效Policy，不新增角色、权限、凭据或第二次资格分配。失效前分别完成4/1两页JSON／CSV；失效后SQL候选中无可用角色方式，各自旧游标及无游标JSON／CSV必须精确403/`query_protection_roles_unavailable`、无结果且单个no-store。主动撤销继续单独证明旧令牌401及正式同用户重登；自然到期继续要求同一会话、授权版本及功能权限不变、Assignment仍active/expired，不重登／续期／撤销或改时钟。显式移除选择后分别恢复默认五行。

SQL创建立即捕获ID，定义校验严格核对原SQL、参数声明和私有已选方式，不伪造Meta Source。报告仅存输出结构、CSV／游标摘要及拒绝事实，SQL独立结果嵌入两个既有资格报告，不保存凭据、源值或完整游标。失败、中断及SQL单独清理失败的反例证明仍继续清理原生表与Policy；根资源计数13+task_ids，浏览器v9保持原边界。现有 `make test-security-online-runner` 和CI登记自动覆盖，无新入口、工作流、产品API或后端变更，无需重启。本轮确定性证据见 `/tmp/p145-sql-eligibility-20261104/verification.json`；最终命令及数量以该记录为准。SQL新增403断言已先失败后修复，所属163项通过。完整 `make test-security-online-runner` 退出2：部署41项及Graph80项通过，后续Cleanup198项中 `test_delegated_image_log_visible_before_process_returns_and_next_phase_waits` 未观察到日志标记而失败，父门禁因此未继续执行Security组；这是本轮未修改的依赖测试，未调查、修复或重跑，不将完整入口记为通过。随后单独执行根Makefile中原定的Security测试组，366项退出0；登记CLI退出0。该组覆盖当前Runner和登记反例，未重跑失败的Cleanup依赖，不把分组通过改写成完整总入口通过。

当前实际运行缺口仍为v36普通User／MySQL SQL资格撤销和自然到期的Hosted/T4；原#31/string参数或原生表资格证据不可替代。本轮未启动CI、操作个人浏览器／服务、修改个人角色／源或原值授权。可选默认参数的后续断言见下一段；Hosted仍由专门CI会话跟进。

Service SQL 可选默认参数（2026-11-04，v37）：新增一个发布默认值为1的可选int参数私有SQL服务，保留必填及资格生命周期场景，复用同一角色方式选择／撤销／显式默认恢复和清理路径。默认保护、角色全遮盖、恢复默认三个阶段各核对空映射、省略parameters成员及显式默认值；两个方向交替省略／显式传值续读同一解析参数游标，行、列、CSV字节和服务版本一致。改值真实筛选，改值续读旧游标精确400；显式null、布尔、对象、空字符串、多余参数按参数错误拒绝，不套用默认值。方式撤销后，省略和显式默认值的新请求／旧游标JSON／CSV均精确409，显式移除选择才恢复初始CSV；运行值不覆盖发布默认值。

候选接口没有服务声明，按已有API显式绑定发布默认值，再在正式执行请求验证省略；没有修改API或保护规则。当前领域报告唯一v37，浏览器v9不扩展；新增一个Service的计数，根资源14+task_ids，完整游标、凭据、运行输入和源值不归档。标准 `make test-security-online-runner` 退出0，41／80／198／184／375项及登记CLI通过；所属171项通过，固定输入摘要及结果见 `/tmp/p145-optional-default-20261104/verification.json`。原测试先RED，新反例覆盖忽略默认、CSV丢默认、按原始输入错误绑定游标、运行值覆盖默认、显式默认绕过撤销、错误拒绝及失败／中断清理。CI登记同时约束新派发、报告及等价事实，无新工作流或产品后端变更，无需重启。本轮没有个人账号、浏览器、角色、源／原值授权或本地服务操作；新增v37 Hosted/T4未运行，仍由专门会话跟进。下一步优先验证发布默认值修订后旧游标的明确拒绝及新查询按新默认脱敏，沿已有服务版本合同。

Graph 保护消费者（2026-10-17）：固定 `addp-graph` 仅消费 `graph` 投影及确认 cursor，纳入安装／解除屏障；启动事务补齐存量未 released 资源及 releasing 的解除变化。Graph 领域读取复用 Common 显式 graph leaf 门禁，命中纳管拒绝，不提供属性脱敏、Policy 或原值豁免；System 源数据授权本轮不调整。当前消费者集合以 `protection_owner_contract.go` 为准，下文四 Owner 的历史验收不作为 Graph 已通过的证据。验证沿 Graph/Security/System Go、Security PostgreSQL、System migration 和 Common Neo4j/FalkorDB 标准门禁，既有 CI 自动发现。

# Security 模块说明


2026-10-27，用户重启后完成普通预览／查询响应禁缓存的限定本地真实Gateway HTTP验收，Manager／Service实际构建均为`d8aad1735df11220472892cac6b9d5fe72650efd`，已核验包含代码修复`9e9de5a5361adc8874363ef414fa453b0f313750`。正式第一方`addp-web`用户为outdoor开发者#33、Tenant#1；隐藏输入的密码和令牌没有保存，测试结束正常注销。Manager五行预览的列仍为`_id,nick_name,gender,phone`，首行phone默认掩码，另四行按现有抑制规则省略phone键；逐个单元格与前次完整CSV对照一致。空locator继续400且不含数据。Service同一Persons来源实际首五行JSON、首五行CSV和一页五行JSON续读通过：首五行一条掩码／四个空单元格，续页五条掩码且无重复行；JSON与CSV逐值一致，实际CSV的SHA-256仍为`0567ee31dc09da22a743e0f7eeb1dc3e9534cf2cb4700aa7cc8f9f8c6a7ef73f`。成功响应及真实400／404／503拒绝共七项均精确单一`Cache-Control: no-store`，按当前Backend实例request ID、精确路由、方法与状态逐项关联。Service同一不存在测试名的404/service_not_found还与修复前缺少头的响应作了对照，原因不变。

首轮临时脚本错误要求每行保留phone键，在200预览后报Known fields missing；这是验收器没有体现既有抑制语义，该轮未计通过且会话已注销，原报告保留为`attempt-1-acceptance-report.json`。结合Manager/preview投影revision13的mask及invalid=suppress和共享执行器省略键实现，修正为严格核对列结构、所有非保护字段、受保护非空值及逐行CSV oracle；缺失应有掩码、原值、缺失普通字段和错误ID四个反例均拒绝，`temporary-helper-regression.json`保留证据，没有修改或放宽产品保护规则。

Service既有测试服务#6「Persons 手机号保护验收」原为私有停用、version27、无角色专用选择。用户明确同意临时启用并恢复停用后，仅经普通User的正式版本化API写入status=active/version28，验收结束在finally中恢复inactive/version29；完整业务配置逐项比较保持一致，仍public_access=false、角色选择空、原引擎3及表来源不变。未调整角色、Policy、原值或源授权，也没有借用管理身份。独立只读核验当前持久状态确为停用29，Manager/preview与Service/service_execute投影仍为revision13／14和原默认前3后4掩码／invalid=suppress。

实际执行`bash /tmp/p145-run-data-response-acceptance.sh --temporary-service-cycle`及`python3 /tmp/p145-data-response-independent-audit.py`通过，证据位于`/tmp/p145-data-response-http-20261027/`：`acceptance-report.json`、`independent-runtime-audit.json`、`manager-five-rows.json`、`service-five-rows.csv`、匿名404与恢复停用后503事实及初次失败／辅助反例报告。仅补已有记录，产品输入未变，复用已通过的Manager/Service Go T1和Swagger，不重复门禁。本次只证明该MySQL非空间测试表、五行预览与首五行CSV／JSON及一次续页、400／404／503拒绝；GeoJSON、全量续页、角色资格到期、浏览器和隔离Hosted仍未在本轮验证。CI继续由专门会话跟进，不自行定时或轮询；功能清单实际行155／156保持部分实现。

2026-10-26，补齐普通数据响应的 HTTP 缓存边界：Manager `GET /preview` 的预览 JSON、Service `POST /api/query/{serviceName}/query` 的 JSON／CSV／GeoJSON（含游标续页），均在 Handler 首次写出前复用现有 `common/api.NoStore`。成功及 Handler 内参数、资源、身份、租户、停用／协议未启用、保护等拒绝响应统一声明 `Cache-Control: no-store`；不改变数据、游标、当前角色或原值授权判断，不调整专门的瓦片／文件／派生缓存。上游认证／Gateway 在进入 Handler 前的响应仍不在此项范围。

先由 Manager HTTP Owner＋Resolver＋原生 Provider 既有用例确认成功预览缺少头、空 locator 的400也缺少头；新增 Service 回归的首版 SQLite 夹具把JSONB协议存成TEXT而非驱动要求的字节，纠正夹具后，404／401／403／503／501／两项400均按原状态返回且真实复现缺少头（保留两个red日志，不作为产品访问失败）。修复后 `make test-module MODULE=manager GO_ONLY=1`、`MODULE=service GO_ONLY=1` 均通过，仅Go T1。Manager原用例同时继续核对默认掩码、短期原值、四种实时结构漂移拒绝和恢复；Service七种真实Handler早退的状态不变。Swagger标准生成与覆盖通过，Manager94／Service63个公开路由；新头覆盖文档中的8／9类响应，Service补全实际已有501／503说明。生成器同步既有`dataitem.ItemDescriptor.sizeBytes`的`*int64`格式说明，代码数据模型未改；独立语义差异审计确认无其他变化。相关证据为 `/tmp/p145-data-response-cache-{manager,service}-green.log`、同前缀red及service-red-fixture-corrected日志、swagger-gen／swagger-coverage日志、`/tmp/p145-data-response-cache-swagger-audit.json`。既有Go和Swagger门禁自动发现覆盖，无新增入口／依赖／CI编排，Common实现未变而不重复有效门禁。重启前另经真实Gateway canonical `POST /api/query/{serviceName}/query` 向不存在的测试名只读请求，精确404/service_not_found且无Cache-Control，保存 `/tmp/p145-data-response-cache-runtime-before.json`；首次误用控制面前缀的路由404另存为noncanonical-route记录，不作为Handler证据。该代码提交时尚未加载新进程验收修复后的真实HTTP；下一段所述2026-10-27结果另行记录。浏览器／隔离Hosted没有在此次本地对照中执行，AI不启停服务，CI仍由专门会话跟进。

2026-10-26，现有 Transfer 角色资格自然到期隔离场景升级为领域 v32，补齐导出缓存证据，不新增部署、账号、角色、权限或产品路线。共享 Gateway 传输只额外保留 `Cache-Control` 的全部实际值，JSON、CSV 流和 HTTPError 均覆盖，Cookie 等凭据头不进入 Response。到期前所选方式与主动清空选择后的默认 CSV，创建、每次状态读取、下载均要求单一 `no-store`，并在两份 CSV 的 `cache_control` 中记录三阶段值；自然到期后旧状态、旧文件、原查询再次导出及默认恢复后旧文件复读，继续精确 `409/query_protection_roles_unavailable`、无结果，并分别写入 `export_refusal_cache_control`。同一会话、六个实际执行 UUID、读写五行、Policy／投影／目标不变、默认恢复及原清理边界全部保留。

先注入十个交付阶段的缺失、公开缓存、重复矛盾头共30个反例，旧路径实际全部未拒绝，保存 `/tmp/p145-expiry-cache-owner-red.log`；补强后这些反例及空值／no-cache／private／重复no-store均拒绝，且测试核对原任务、Policy和其他账号审计的清理边界。传输回归额外覆盖正常JSON、实际字节和HTTPError保留重复头、不收集Cookie。完整 `make test-security-online-runner` 通过：System fixture Go、38／58／31／282项Python及最终Online CI登记一致性均成功，日志 `/tmp/p145-expiry-cache-runner-20261026.log`；输入摘要保存 `/tmp/p145-expiry-cache-inputs-20261026.json`，交付前复核本会话七个输入不变。版本与新增必需证据的登记／破坏反例、既有文档同次更新，继续沿原门禁自动发现，不新增入口或CI编排。这里仅是T0/T1与隔离套件就绪，本轮没有执行真实自然到期HTTP／浏览器／Hosted T4，不用上次默认导出19项本地对照替代。产品代码及本地角色、策略、授权保持原状；真实v32运行仍由专门CI会话跟进，不自行定时、派发或跟踪。

2026-10-26，补齐 Manager／Develop 用户导出会话的 HTTP 缓存边界：创建、状态和文件 Handler 在写出任何响应前复用 `common/api.NoStore` 设置 `Cache-Control: no-store`，覆盖成功、参数错误和业务拒绝；Develop 原有实时结果接口改用同一 helper。状态码、精确拒绝原因、文件正文与当前用户复核不变，不改变角色、选项或原值授权，也不清理既有日志或下载副本。Gateway／认证中间件在进入 Handler 前的响应不在此次范围。先由新增 Develop 回归实际复现三个早退响应缺少头，再修复唯一主路径；Common 回归覆盖 JSON／CSV 流首次写出时的头、状态和字节不变，Manager／Develop 各覆盖三种早退。`make test-module MODULE=common GO_ONLY=1`、`MODULE=manager GO_ONLY=1`、`MODULE=develop GO_ONLY=1` 均通过（仅 Go T1）；既有 Go 自动发现已覆盖，无新增依赖或 CI 入口。六个导出接口的双语 Swagger 头声明及生成产物同步，标准生成与 `bash scripts/swagger/check-route-coverage.sh manager develop` 通过；工具安装模块版本为 v1.16.6，以 `go version -m` 核验。证据 `/tmp/p145-export-cache-{common,manager,develop}-green.log`、`/tmp/p145-export-cache-develop-red.log`、`/tmp/p145-export-cache-swagger-{gen-v1166,coverage}.log`。该代码提交时尚未进行重启后真实 HTTP；下段记录新版实际结果。浏览器或隔离 Hosted T4 未在本轮执行，不将先前缺少响应头的 CSV 验收当作新功能通过；CI 仍由专门会话跟进。


2026-10-26，用户重启后完成上述缓存边界的限定本地真实 Gateway HTTP 验收，未依赖浏览器控制。Security／Manager／Develop／Transfer 实际构建均为 `fad6a9bd68d20ad82cf202c6bc05b99e1ac040dc`，已核验包含修复 `76b87598bc224b393d1a9653f8b38e51bab7fc25`。用户在自己终端隐藏输入密码，经正式登录得到 outdoor开发者 #33、Tenant #1 的第一方 `addp-web` 会话，结束正常注销；未保存密码／令牌，也未修改角色、Policy、原值或源授权。19 项实际 HTTP 对照均通过：5 项 200、2 项 202、6 项 400、4 项 404、2 项 409，每一响应都精确带单一 `Cache-Control: no-store`，并通过当前实例日志的 request ID／路由／状态关联。中英文方式失效继续精确返回 `409/query_protection_choice_invalid` 和导出专用建议，未混同角色资格失效。

Develop 新查询 `99f3c0db-53bc-420e-a4e7-5caba8551939`、导出会话 #15／Transfer `1f7c46b5-78d1-4982-98ae-7903e4ee423e` 实际读写五行；CSV 为一行手机号掩码、四个空单元格，中文正确，SHA-256 仍为既有 `0567ee31dc09da22a743e0f7eeb1dc3e9534cf2cb4700aa7cc8f9f8c6a7ef73f`，表明新增响应头未改变这些文件字节。Manager 原生整表导出会话 #9／Transfer `1a4b7cce-5aa2-4c6d-8b4d-9e69f7a0d627` 实际读写 2,188 行，全部 ID 唯一；863 个非空手机号逐值符合默认前后掩码，1,325 个空值保留空单元格，五条共同记录与 Develop 逐列一致，SHA-256 `0c4c3adf2af6cbb682ddc748e09e328984e43d501f0bef4103da7ea05a720c7b`。两份下载均为正确 CSV Content-Type／附件文件名。独立只读审计同时核对两会话归属、本次 User 查询、两个服务代发 Transfer 的人类观测来源（triggered_by=33、Actor／Membership／授权版本／EA 均空，不提升为 User 执行权）、准确 UUID／成功状态／读写计数，以及既有 Policy 仍 revoked/revision 6。

实际命令 `python3 /tmp/p145-export-cache-http-runtime-audit.py` 与两次 `python3 /tmp/p145-audit-export-csv.py <CSV> --rows <5|2188> --mode default --report <报告>` 均通过；首次临时审计解析非请求日志时把 JSON 布尔值当成对象，修正为只接收对象型 `http.request.completed` 后复验通过，未放宽任何请求、文件或权限断言。保留失败分类于 `audit-attempts.json`。证据统一位于 `/tmp/p145-export-cache-http-20261026/` 的 `acceptance-report.json`、`independent-runtime-audit.json`、两份 CSV 及两份 `*-csv-independent-audit.json`。本轮仅补已有验收记录，复用产品输入未变的 Go T1／Swagger 门禁；不启停服务、不设置定时、不派发或跟进 CI。不将本地 HTTP／物理文件对照记为浏览器或隔离 Hosted T4；真实角色资格到期、新版旧特殊会话状态／文件拒绝及上游认证前响应仍未在本轮验证，清单实际行155／156保持部分实现。

2026-10-26，补强既有角色资格自然到期套件的 CSV 执行来源核验，领域报告仍为 v31、结构不变。到期前的角色全遮盖 CSV 和显式空选择后的默认 CSV，都在下载前通过同一普通 User 调用已有 Transfer 执行详情接口，核对会话冻结的 UUID、成功状态、`task_id=0`、手动触发、`trigger_by` 精确等于当前已验证用户、实际读写五行；不把人类观测来源当作私有 User Actor／EA，也不新增接口或权限。两种方式分别注入八类反例（外部／缺失发起人、错误 UUID、绑定任务、失败状态、非手动触发、读／写计数错误），全部要求拒绝该对照、尚未下载文件且清理保持精确。先用新反例确认旧路径缺口，再修正唯一验收主路径；专项四项及所属脚本 134 项 Python 回归通过，日志 `/tmp/p145-role-csv-provenance-{red,owner-20261026}.log`。既有 `make test-security-online-runner` 与原 CI 登记已覆盖这些文件，无新增入口、依赖或编排修改。

此前 v31 执行来源核验补强时，完整 `make test-security-online-runner` 在共享工作区及仅应用本会话三个文件的临时干净 main 副本都未通过：共同失败为既有栅格登记用例 `test_raster_requires_manual_disposable_profile_and_registered_regressions`，不是本轮保护断言。干净基线 `a5b89d7375a889bf7126ffd627421e0d3fc3887f` 的其余阶段实际通过（System fixture Go、38／58／31 项 Python，最后 277 项中仅该项错误）；最终独立登记检查因前置失败未执行，整体不计通过，也不修改或等待栅格工作。证据 `/tmp/p145-role-csv-provenance-{runner,clean-runner}-20261026.log` 与 `/tmp/p145-role-csv-clean-scope.json`（记录精确输入散列）。本轮只有验收脚本和说明变化，不启停服务、不改业务账号角色／Policy／授权，不跟进 CI；新增真实 HTTP／Hosted 角色到期证据仍未运行，旧本地查询／CSV 通过结论不据此扩大。

2026-10-26，Develop 导出提示修正补充限定本地真实 HTTP 验收。沿正式 `/system/login` 取得 outdoor开发者 #33、Tenant #1 的第一方 `addp-web` 会话，经 Gateway 复用既有 MySQL Persons 五行 SQL 与已撤销的导出方式，不修改角色、Policy、原值或源授权。针对原查询 `64b2d239-2280-4d7f-ac4b-f5fc4bbb245a` 再次创建导出，中英文均精确返回 `409/query_protection_choice_invalid`，正文只有安全错误和对应的导出专用建议。新默认查询 `40eb7144-2cf5-4ab9-98e6-35eaa44929e6` 成功，导出会话 #14／Transfer `529987ad-95fa-4781-9624-9e6ba61a9e3b` 实际读写五行；创建 202、状态和文件 200。保存的 CSV 四列、五个唯一 ID、一个默认掩码 phone 与四个空单元格通过独立字节审计，中文值保留，SHA256 `0567ee31dc09da22a743e0f7eeb1dc3e9534cf2cb4700aa7cc8f9f8c6a7ef73f`；响应 `Content-Type: text/csv`，附件文件名正确。六个 HTTP 响应均按同一 request_id 对照 Develop 本实例日志，临时第一方会话在脚本结束时正式注销。

首次独立审计误把默认 Transfer 导出的 `actor_principal_id` 也要求为 #33，导致脚本 AssertionError。按[第一方导出来源契约](../docs/spec/addp登录认证的统一要求.md)与既有 `TestExportExecutionUsesOwnerVerifiedInitiatorAndOwnAdHocIsolation`，该服务代发执行只以 owner 核验的 `triggered_by=33` 表示人类来源，不得提升成 User Execution Authorization。修正临时审计后额外严格要求 Transfer 的 Actor、Membership、授权版本与 EA 均为空，来源为 Develop、无任务定义、bounded/sync，并核对查询本身的 User Actor／EA、会话用户和两条成功 UUID。`python3 /tmp/p145-export-web-http-runtime-audit.py` 与 `python3 /tmp/p145-audit-export-csv.py ... --rows 5 --mode default --report ...` 实际通过；原失败保留为临时审计假设错误，不计产品故障，不修改产品身份记录。证据 `/tmp/p145-export-web-http-20261026/{acceptance-report,independent-runtime-audit,csv-independent-audit}.json` 及同目录实际 CSV。

本轮运行的 Security／Transfer 为 `1ccbf4316bb6244a72d188d8cc78bd2699bf141f`，Develop 为 `cb2a25b5b24f117417fa7dd2b55d04e314c5fd6d`，均包含提示修复 `f237501b4`；只修正临时审计并补既有记录，复用输入未变的已通过门禁。此前 CLI Device Flow 首次批准和身份核验实际成功，重复提交已消费设备码才产生 `invalid_grant`；CLI OAuth Token 被现有第一方角色选择边界拦截，不能充作角色到期或方式撤销验收。浏览器控制不稳定，故本轮不宣称页面点击或新 Hosted T4 通过。角色资格真实失效、旧特殊会话状态／文件的新提示仍未在本轮运行；原特殊文件会话已按保留周期清理，不能以默认文件替代。CI 继续由专门会话跟进，不自行调度、跟踪或启停服务，清单行 155／156／157 保持部分实现。

2026-10-25，Develop导出原因细分已落地：Common Transfer客户端只按严格有界JSON和稳定错误码保留409/transfer_protection_roles_unavailable为角色资格不可用，409/transfer_protection_choice_invalid为方式失效；401/403仍独立，未知码/异常协议不猜测原因或转发上游正文。Develop导出创建、状态和文件错误统一复用类型化写回，分别输出query_protection_roles_unavailable与query_protection_choice_invalid及导出专用双语建议，保留查询实时缓存既有文案；Swagger同步三处409声明。客户端、实际BFF Service Create/Get/Open及HTTP中文/英文回归通过，前端新增12个受控浏览器用例直接消费后端完整译文，三入口×两原因×双语均无下载；目视核对完整中文提示。Common/Develop/Transfer的 `make test-module MODULE=<owner> GO_ONLY=1` 通过（仅Go T1）；共享前端277项通过；`make test-develop-frontend` 最终106项浏览器及workflow测试、构建通过（T1/T3，首轮两个既有交互超时，未改实现而复核通过）；Swagger生成及60个公开路由覆盖通过。现有隔离领域报告v31的Transfer资格到期断言同步要求四处导出拒绝保留角色原因，`make test-security-online-runner` 38/55/30/274项Python、相关Go及登记通过（T0/T1），日志 `/tmp/p145-export-reason-runner-20261025.log`。上述不代替新版实际HTTP/Hosted T4，CI仍归专门会话，本地Common运行验收需用户重启全套；不改任何角色资格、原值/源授权或保护裁决。

2026-10-25，隔离 suite 领域 v30 追加 `transfer_role_membership_expiry`：使用独立普通用户与固定主功能角色，复用既有资格角色定义，正式 API 新建90秒 Assignment；先同步 transfer/export Policy，再真实时间等待。期限内保存独立任务、真实写出既有合成目标五行全遮盖，并通过 Develop 成功查询冻结独立 export 选择，生成和下载实际五行 CSV；查询预览仍默认保护。到期后同一会话/主体/成员/授权版本/功能权限保持，仅资格消失，Assignment active/expired且无撤销。任务启动、名称更新、本人直接选择检查精确409/transfer_protection_roles_unavailable；任务仍只有原成功执行且目标逐值不变。旧导出状态/文件和从原快照再次导出按现有 BFF 合同409/query_protection_choice_invalid，无结果；Common BFF合并409的提示缺口单列，未伪称已经细分资格原因。显式清空任务/新查询export选择后，默认任务五行和新默认CSV恢复，旧特殊文件继续拒绝。失败/中断先删本轮任务，再撤销仍有效资格，避免会话失效阻断清理；成功不 revoke/重登/续期。导出会话/文件审计保留由外层一次性Infra销毁，不在领域阶段预报清理通过。`make test-security-online-runner` 的38/55/30/274项Python、相关Go夹具与登记检查通过（T0/T1），日志 `/tmp/p145-transfer-natural-expiry-runner-20261025.log`；10项三Owner到期专项通过。新真实HTTP/Hosted尚未执行，浏览器v9维持原范围。详见[Transfer资格到期](../scripts/docs/Online数据保护.md#transfer-资格自然到期与旧文件)。CI仍归专门会话，不自行跟踪或定时，不启停个人环境。

2026-10-25，隔离 suite 领域报告 v29 新增独立 `develop_role_membership_expiry`：System 夹具创建只具备查询/数据读取/执行授权/本人历史与 Meta 读取的普通 Develop 用户，资格角色只重复其已有服务定义读取权限；复用既有角色定义及隔离 IAM 管理员，不触碰 Service 用户的撤销/到期事实。同步 Develop/query Policy 后经正式 API 建立 90 秒 Assignment，以真实时间等待自然到期；同一会话、主体、成员、授权版本及功能 Permission 不变，Assignment active/expired 且无撤销。到期前实时全遮盖与同 UUID 默认脱敏历史分别验证；到期后旧实时结果精确 409/query_protection_roles_unavailable，保留原选择的新执行以 preparation/protection_role_choice_unavailable 失败、同一历史无结果，旧成功历史保持默认脱敏。显式空选择的新执行/实时结果恢复五行默认保护。精确清理仅触及本轮 Policy/Assignment，凭据不进入子进程或报告。`make test-security-online-runner` 的 38/50/28/268 项 Python、相关 Go 及登记检查通过（T0/T1），日志 `/tmp/p145-develop-natural-expiry-runner-20261025.log`；新增七项 Develop/Service 到期专项另通过。[场景与未验范围](../scripts/docs/Online数据保护.md#develop-角色资格自然到期)明确 v29 真实 HTTP/Hosted 尚未执行，既有浏览器 v9 范围保持。CI 仍由专门会话跟进，本会话不触发、轮询或定时。

2026-10-25，隔离 suite 领域报告 v28 独立追加 `service_role_membership_expiry`：沿既有 IAM API 为专用普通用户新建 90 秒资格 Assignment，保护同步后才开始期限，不改旧撤销记录或系统时钟。到期前两页五行 JSON/CSV 正常；真实到期后，要求同一 Access Token 仍可认证、主体/租户/成员/授权版本/功能权限不变，仅资格角色消失，Assignment 为持久 active、动态 expired 且无撤销事实。有效 Policy/投影/服务选择不变时，旧游标与无游标请求均严格为 `403/query_protection_roles_unavailable`、无结果；显式移除后同一会话恢复默认五行保护。成功路径不撤销或续期，失败/中断仅清理本轮仍有效资格，已到期事实保留随隔离 Infra 销毁。标准入口仍为 `make test-security-online-runner`，本轮 38/50/28/265 项 Python 回归、相关 Go 夹具与登记检查通过（T0/T1），日志 `/tmp/p145-role-natural-expiry-runner-20261025.log`；[期限与证据边界](../scripts/docs/Online数据保护.md#角色资格自然到期)区分已有自然到期协议 T2、本轮 Runner 与尚未运行的 v28 Hosted，不把它们合并为 T4 通过。CI 仍归专门会话，本会话不派发、跟踪或设置定时。

2026-10-25，既有隔离 suite 领域报告 v27 另加 `service_role_membership_revocation`，与方式撤销的 409 独立：精确撤销普通测试用户的额外资格角色分配，旧会话 401 单列；正式重登同一用户、功能 Permission 集合不变后，旧游标 JSON/CSV 与无游标对照必须为 `403/query_protection_roles_unavailable` 且无结果，Policy/投影/服务选择不变，显式移除后五行默认保护恢复。额外角色只重复已有服务定义读取权限；复用隔离初始化已有的 Tenant 管理员仅控制精确 IAM 分配，不用于数据消费，不新增角色管理 Permission 或个人授权。凭据不传浏览器及生命周期子进程；失败/中断精确清理。标准入口仍为 `make test-security-online-runner`，本轮 38/49/28/261 项 Python 回归、相关 Go 夹具与登记检查通过（T0/T1），日志 `/tmp/p145-service-membership-runner-verified-20261025.log`；[细节及未验范围](../scripts/docs/Online数据保护.md#用户角色资格撤销)明确新增 Hosted 未运行，CI 继续归专门会话，不设置定时。

2026-10-25，既有 `security-mysql-owner-protection` 隔离套件的领域报告 v26 新增 Service 角色方式撤销后的旧游标 JSON/CSV 精确拒绝、保留失效选择直接停用、显式移除后的五行默认恢复及精确清理。复用当轮 MySQL 合成表和普通 User，不撤销用户角色、不改变原值或源授权；[场景与证据边界](../scripts/docs/Online数据保护.md#service-角色方式撤销后的旧游标)区分已通过的本地限定验收与尚未运行的新增 Hosted T4。`make test-security-online-runner` 的 38/49/28/254 项 Python 回归、相关 Go 夹具及登记检查通过；最后清理计数修改由新增六项 Runner 回归复核通过。既有浏览器 v9 范围保持，CI 由专门会话跟进，本会话不派发、轮询或设置定时。

Security 是 ADDP 数据安全控制面，唯一拥有敏感数据类型、安全分类、安全等级、检测器、发现、资源安全评估、保护基线、策略、原值访问申请、临时原值授权和保护投影。DetectorCapability 是平台代码提供的只读可信能力，Detector 是 Tenant 把能力绑定到 SensitiveDataType 的版本化启用配置；发现不得通过敏感类型代码或名称猜测绑定。当前已经实现基础定义、显式保护纳管、Owner 投影变化流、acknowledgement 激活/释放屏障，以及手机号元数据/文档、邮箱、身份证件号码和银行卡号元数据检测、无原值 Finding、一次性 Finding review、不可变 Assessment/ProtectionPolicy revision、唯一投影编译、显式重新发现和资源级有效期设置。Manager、Develop、Service 已具备自身字段级动作投影，Transfer bounded snapshot 的独立 `export` 动作与 PostgreSQL、MongoDB 原始记录执行器已完成。当前用户从 Manager 或 Develop 复用共享交互发起 ProtectionAccessRequest，显式选择 Manager 预览、Develop 查询或两者；另一名有审批权限的用户一次审批，原子形成各精确出口的 ProtectionExemption。授权仍按用户、按字段、最长 30 天，已有 Manager 授权不扩大。Projection v5 始终保留默认保护决策并携带主体级限时 `allow`，Owner 到期、撤销、Assessment 修订变化或主体不匹配时本地自动回落。用途约束和其他主体类型属于后续阶段。

## 边界

- 不拥有用户、角色、登录认证或资源授权；这些事实属于 System/IAM 和资源 Owner。
- 当前所有 active Security Permission 只允许 `tenant` Scope；管理 API 只消费 Tenant Context，不把 Department 或 Project Group Assignment 当作资源过滤。租户治理使用 `tenant.security_manager`，原值申请使用 `tenant.protected_data_requester` 或等价的 Tenant-only 自定义 Role。未来开放组织级授权前必须先在规范中建立可信的资源归属、Scope Binding、显式 Deny 与 owner 最终策略校验契约。
- 不复制 Meta DataItem、CatalogEntry 或 CatalogComponent，不以 Catalog 建档作为安全事实成立的前置条件；Enrollment 只冻结 Engine ID、item type 与 full_name 的最小保护目标快照用于展示和审计，不保存 attributes 或字段事实。
- 不代理数据预览、查询、导出或服务流量；Owner 使用 `common/dataprotection` 在自身服务端执行保护投影。
- `common/secretcipher` 只负责静态敏感配置值加解密，不是 Security 业务模块的一部分。
- 未纳管资源不进入检测、投影或保护路径，不产生额外远程调用和安全审计负担。
- Manager、Develop、Service、Transfer 的必要 Owner 集合及各自字段级主动作只能在 `internal/service` 的 Owner 契约中定义一次；Enrollment 创建、全量重编译、变化流校验和历史投影升级均从该契约派生，不得重复写 Owner 切片或 action 分支。当前可申请原值访问的出口另行严格限定为 `manager/preview`、`develop/query`，由申请显式选择，不能从 Owner 主动作集合自动扩张。

保护投影自身到期与用户临时原值授权到期分别验证：投影到期必须拒绝相关输出，授权到期仅回落默认保护。Common 查询／原生表的实际输出保护器及 Manager 普通预览响应必须重新校验本次读取绑定投影的有效期，空结果也不能输出过期字段结构。四个 Owner 的指定时点回归由 Go T1 和 `make test-common-postgres` 覆盖，Transfer 真实目标零新增写入由既有 `make test-transfer-postgres` 覆盖；历史协议夹具保留 365 天有效期，沿正式变化流安装，不改变产品有效期或本地系统时钟。同一回归还覆盖更高修订替换到期投影：旧读取继续拒绝，其他存储实例刷新持久 cursor 后的新读取执行新保护决策，重新打开存储后保持修订、纳管状态与保护输出；不增加自动续期路径。这些证据属于 T1/T2，不代替长周期到期 T4。

## 运行角色

敏感数据发现的过程事件复用 `common.execution_events`：实际 claim 与 `started` 同事务，成功时与 Finding、Enrollment 当前来源和保护投影一起提交 `completed`，既有失败及租约重试耗尽分别提交唯一 `failed`。错配或过期 lease 不得追加，事件写入失败回滚对应事务。仍按现有 `max_attempts` 恢复：回到 pending 时不伪造 execution 终态事件，新 attempt 实际 claim 后记录自己的 started。心跳不产生事件；本切片不新增百分比或 Runtime 步骤，不保存字段、样本、Detector 配置、专业结果或授权信息。读取复用已有 Owner 读取裁决，30 天事件维护不改变专业证据、执行概览或审计生命周期。验证沿 `make test-module MODULE=security GO_ONLY=1` 和 `make test-security-postgres`；新增 `TestDiscoveryExecutionEventsAgainstPostgres` 纳入后者既有服务测试筛选，既有 Go／Security PostgreSQL CI 自动覆盖。确定性测试不能代替真实 Gateway／Worker／Monitor T4。

- Backend：端口 `8194`，API 前缀 `/api/v1/security`，维护控制面事实。
- Worker：独立运行角色；通过 `common.task_executions` 领取 `security/sensitive_data_discovery` 有界执行，使用 `addp-security` Tenant Service Access Token 精确读取 Meta 技术事实，并且只对已显式纳管的文档按 fingerprint 读取临时受控正文样本；通用租约过期后按 `max_attempts` 重试或失败收口，不运行定时调度或 TaskProvider。
- Frontend：端口 `5191`，通过 Console iframe 集成；产品入口固定收敛为“分类分级体系”“敏感数据定义”“受保护资源”。`/classification-grading` 以可恢复 `tab` 组织低频维护的 SecurityClassification 和 SecurityGrade；`/sensitive-data-definitions` 以 SensitiveDataType 为主视图，把 Detector 作为“识别方式”、把 ProtectionBaseline 作为“默认保护”收进对应敏感类型，不再保留 `/protection-baselines` 产品路由；“受保护资源”统一承载 Assessment 调整、按需读取的不可变修订历史与只能收紧的 ProtectionPolicy 编辑，并把 acknowledgement 表述为保护规则“待同步/已同步”，不使用“待安装/已安装”或“已生效”，也不能表述成某个具体请求或该 Owner 所有数据形态均已执行成功。旧的实体级分类、等级、敏感类型和独立保护基线页面路径不恢复，也不保留兼容入口。

独立前端根路径在 AuthContext 加载后，按 Console 菜单顺序 `/classification-grading`、`/sensitive-data-definitions`、`/protection-enrollments` 选择第一个符合页面准入条件的地址；没有可访问页面时进入 `/forbidden`。登录后的默认返回地址为根路径，显式业务地址仍按自身权限判断。

Manager 原值申请目标的 `projection_acknowledged` 复用现有 Owner 回执，表示当前资源投影已同步；不改变 `approved` 审批结论或当前主体授权语义。申请目标从同一只读一致性快照读取投影、审批、Assessment、授权和回执。页面在已批准但待同步时继续轮询，确认后刷新预览；数据面仍仅消费本地投影。

临时授权列表、详情和撤销响应直接返回由同租户绑定 Assessment 派生的 `component_key`。授权卡片仅消费该字段，不依赖可选 Assessment 列表；持有授权读取权限的审批用户无需取得 Assessment 读取权限即可识别授权字段。组件键只存在于响应 DTO，不新增持久事实或权限。

## 数据库

使用 PostgreSQL `security` schema。当前事实表为：

- `security.security_classifications`
- `security.security_grades`
- `security.sensitive_data_types`
- `security.protection_baselines`
- `security.protection_enrollments`
- `security.protection_projections`
- `security.protection_projection_changes`
- `security.protection_projection_acknowledgements`
- `security.sensitive_findings`
- `security.sensitive_finding_reviews`
- `security.resource_security_assessments`
- `security.resource_security_assessment_revisions`
- `security.protection_policies`
- `security.protection_policy_revisions`
- `security.protection_access_requests`
- `security.protection_exemptions`
- `security.protection_exemption_revisions`

四个必要 Owner 确认 enrolling 门禁后，Security 在同一事务创建一次 discovery execution。治理人员也可携带 Enrollment `version` 显式创建重新发现执行；同一纳管同时至多一个 pending/running execution。完成后保存最新结构或文本快照并重新编译投影，保留资源指定截止时间，不自动延期。结构化 DataItem 目前使用 `addp.detector.phone_metadata/v2`、`addp.detector.email_metadata/v1`、`addp.detector.identity_document_number_metadata/v1` 和 `addp.detector.bank_card_number_metadata/v1` 按字段路径、确定性扁平路径语义和通用类型独立识别；文档使用 `addp.detector.phone_document/v1` 在当次内存样本中识别精确 11 位 ASCII 数字串。身份证件号码元数据能力只识别精确字段语义，不读取或校验业务值；结构化字段通过只读算法目录选择 Unicode 前后缀掩码、同类型固定值替换或普通 SM3 哈希；字段策略在基线显式 `allowed_algorithms` 内独立配置算法与 `parameters`，前后保留数不得超过基线上限。基线参数和许可收紧后，不再满足约束的字段策略回退到当前基线。图层沿用带空间字段的数据表语义，属性字段可脱敏，geometry 不执行标量算法。算法执行复用 `common/dataprotection`；文档正文仍只支持既有手机号片段保护。检测能力都不持久化原始业务值。Finding 必须通过当前 Tenant 的 Detector 绑定取得 SensitiveDataType 和自动采用置信度；达到绑定阈值且存在有效 ProtectionBaseline 后，唯一编译器对结构化组件为 Manager 生成 `preview` 和系统派生的 `profile` 规则，为 Develop 生成独立 `query` 规则，为 Service 生成独立 `service_execute` 规则，并为 Transfer bounded snapshot 生成独立 `export` 规则；对文档虚拟组件 `$document.text` 只生成 Manager `search_index` 规则。没有当前动作执行器的 Owner 出口继续保持资源级 deny。

`released` Enrollment 永久保存退出审计且不可恢复状态。再次保护只能通过旧记录上的重新纳入命令创建新的 `activating` Enrollment，并重新走所有必要 Owner 激活屏障；同一目标不允许同时存在两条未退出生命周期。

成功发现后 Enrollment 查询批量返回当前快照 Finding 总数、待复核数和已复核数；Finding 查询使用 `enrollment_id + source_snapshot_hash` 精确限定当前候选，并在分页响应中携带可选不可变初审记录，以及由当前 Detector、Assessment、ProtectionBaseline 和已发布 Projection 批量组装的只读 `explanation`。当前组件结构仍匹配正式 Assessment 时，`explanation.assessment_id` 在 `sensitive` 与 `not_sensitive` 结论下都指向同一精确聚合，供“受保护资源”继续追加修订；组件结构冲突时不返回该关联，前端不得按字段名猜测。集中“待复核候选”是 `/protection-enrollments?tab=review-queue` 的子视图，只通过 `GET /findings?snapshot_scope=current&review_state=pending` 聚合未退出 Enrollment 当前 Finding，不新增队列实体或第二套复核状态。前端不自行推导保护结论，解释链不持久化且不含原值。历史快照只用于审计。零命中只表示当前检测能力未发现候选，不编译 `allow` 且继续资源级 deny。治理人员确认当前无需保护时，必须使用唯一 Release 路径并提交 `basis=no_supported_findings`、Enrollment `version` 和原因；服务端校验最近发现已完成、Finding 数为 0 且无在途发现执行，然后冻结退出依据、发起人、时间和依据快照。

识别质量摘要直接从 Finding、不可变 review 和 Assessment 当前修订即时聚合，不建立统计表或双写链。当前候选只取各未退出 Enrollment 最新成功发现；历史人工质量样本按 Enrollment、组件和检测能力版本折叠为最新 review。人工指定只作为可能漏检的线索，不归因于某个 Detector。

创建 Enrollment 的唯一用户输入是 Meta 资源树返回的 DataItem ResourceLocator；Security 自行计算 fingerprint，只纳管完整 DataItem。旧的 fingerprint 与字段路径自由输入路线不存在。字段组件通常由 Detector 发现；自动发现漏检时，治理人员只能从 Security Backend 实时读取并校验、且尚未形成任何正式 Assessment 的 Meta 当前字段清单中选择组件，直接形成来源为 `manual` 的正式 Assessment，不得自由填写字段路径。已存在 Assessment 的组件不再出现在人工指定候选中，其后续调整或撤销必须在既有聚合上追加修订。Finding 误报通过不可变 `reject` review 收口，既有正式 Assessment 错误则追加 `not_sensitive` 修订撤销，二者都由唯一投影编译器重新发布保护结果。

ProtectionPolicy 绑定正式 Assessment 和一个精确出口：`manager/preview`、`develop/query`、`service/service_execute` 或 `transfer/export`。字段算法及参数只能在当前 ProtectionBaseline 的算法许可、保留上限和异常值约束内独立收紧，效果为 `mask|suppress|deny`，不承载原值授权。创建、更新和撤销都追加不可变 revision，并在同一事务调用唯一投影编译器，只发布对应 Owner 的变化；撤销后回落到 Assessment + ProtectionBaseline，不解除纳管。

ProtectionAccessRequest 是原值访问的唯一入口，只能由当前可信 User AuthContext 从 Manager 预览或 Develop 查询出口针对正式敏感 Assessment 发起，显式选择一个或两个使用范围，正文不得指定主体；申请本身不改变保护效果。申请状态固定为 `pending|approved|rejected|expired`，由 Security 按服务端时间返回有效状态；超过截止时间的未决策申请不再属于待审批，且不阻止申请人重新提交。唯一审批工作区使用 `scope=pending|history` 分别展示未过期待审批申请与已批准、已驳回、已过期历史，必须服务端分页，并可按申请人、资源或字段、申请时间筛选；历史记录还可按处理结果以及当前 `authorization_state=active|expired|revoked|superseded` 筛选，当前授权筛选必须在分页和总数统计前按当前 Exemption、Assessment revision 与服务端时间完成。批准历史必须另外返回当前授权状态与原授权截止时间，不能把“已批准”误作当前仍可访问原值。审批响应返回权威 `enrollment_id`，前端据此复用既有受保护资源详情和授权撤销路径，不按资源名称猜测、不复制第二套授权管理交互。本人待审申请可见但不可自审。申请人和审批人的 Principal ID 是权威审计身份；Security 只在申请或审批动作发生时通过 System 受信租户用户引用接口校验身份并固化显示名快照，审批读取不实时跨模块查询，服务启动会为旧记录补齐一次快照。另一名具有审批权限的用户在截止时间前批准后，Security 才创建或重新激活绑定 `{assessment, manager, preview, user}` 的 ProtectionExemption 并追加不可变 revision；拒绝不产生授权，申请人与审批人相同或申请已过期均冲突。Assessment 后续产生新修订时旧授权立即失效，不得静默恢复。授权效果固定为限时 `allow`，最长 30 天且不得超过申请期限，业务依据和审批理由必填。Projection v2 的规则始终保留 Policy/Baseline 默认 `decision`，主体授权放在 `authorizations`；Owner 必须从服务端可信 AuthContext 匹配 User，不能接受浏览器提交主体。临时授权只能提前撤销，不能直接创建、续期或重新启用；需要继续访问时必须重新申请和审批。

`manager/profile` 不建立可编辑 Policy：唯一编译器把有效 `preview=mask|suppress` 派生为 `profile=suppress`，把 `preview=deny` 派生为 `profile=deny`。Manager 负责把 `profile=suppress` 执行为整个字段剖析对象的移除，Security 不复制 Manager 指标结构。

创建 SensitiveDataType 必须同时提交其自动发现初始等级的完整默认保护，并在同一事务创建有效 ProtectionBaseline；不允许创建“已定义但无默认保护”的半成品。SensitiveDataType 更换自动发现初始等级前，新组合必须已存在有效 ProtectionBaseline；当前初始等级对应的 ProtectionBaseline 不得停用、改绑或单独删除。其他等级的 ProtectionBaseline 创建、更新、启停、改绑和带 `version` 删除必须根据 Security 自有 Finding/Assessment 依赖精准重编译受影响 Enrollment，并与定义写入保持同一事务；SensitiveDataType 自动发现初始等级变化只重算未复核候选 Finding；Detector 自动采用置信度变化走 Detector 配置变更的有界重新发现路径。正式 Assessment revision 冻结当时的类型、分类和等级，名称或排序等展示变化不制造投影版本。影响解析不得扫描全租户 Enrollment，也不得调用 Meta、Catalog 或 Engine。人工确认或调整 Assessment 时，目标 SensitiveDataType + SecurityGrade 也必须已有有效 ProtectionBaseline；`baseline_missing` 仅作为存量异常或一致性故障的失效关闭观测状态，不是正常配置流程。

Owner 变化流是唯一投影交付路线。Manager、Transfer、Develop、Service、Graph 只能使用各自固定 Tenant Service Access Token 拉取自身变化并确认本地原子安装的 cursor；不能提交 consumer owner 或资源清单。

Enrollment 从 `enrolling` 进入 `active` 必须同时满足四个必要 Owner 的当前投影均为 `active`，且各自 acknowledgement 已覆盖当前 `published_sequence`。旧修订的确认不能激活新修订；重复确认不递增 Enrollment 版本，也不重复创建发现执行。激活与退出都沿同一 acknowledgement 状态推进路径，SQLite T1 和 `make test-security-postgres` 的生命周期回归共同验证该屏障。

Standard 的旧分类分级 ID 和数据不迁移、不映射，也不提供兼容 API。

## 前端验证与失败取证

限时原值访问 `security-plaintext-access` 已在提交 `2e9d37e8b` 的 [2026-10-12 Hosted T4 38191967691](https://github.com/pampa0629/addp/actions/runs/38191967691) 首次通过完整 API 与真实 Console 双用户验收：同一 Tenant 的申请人与审批人使用隔离浏览器 Context，两次批准均在 Owner 同步确认后自动刷新原值预览；真实到期和提前撤销后恢复脱敏，另一主体始终脱敏，三行数据中的 null 保留，审批历史的当前授权状态正确。v3 总报告与 v1 页面报告一致，浏览器告警和业务请求错误为零，三张安全截图已目视核对，Hosted 应用、业务容器、Infra 及凭据清理零残留。同提交 Platform CI 完整通过；套件仍只手工调度，不将此结果扩大为 Security 实际停机故障验收。

同一套件的 v4 扩展在完成原有 API 场景后，通过标准生命周期停止临时 Security、核对拒绝连接，验证未过期本地投影下有效原值授权与其他用户脱敏，以及真实授权到期后两人均回落脱敏。Manager 进程保持不变；所有停止尝试均恢复 Security 并核对构建身份、两用户和到期历史，再继续原有真实页面验收。凭据不传入恢复子进程；900 秒总预算、手工 Hosted 入口和零残留清理不变。提交 `96432fd38` 的 [2026-10-12 Hosted T4 38196037099](https://github.com/pampa0629/addp/actions/runs/38196037099) 已首次真实通过 v4：90 秒授权、八个拒绝连接时间点、两用户有效期内和到期后的输出、Manager 实例保持、Security 恢复身份及到期历史均核验通过。原有完整 API 和 v1 页面验收同时通过，三张安全截图已目视核对，浏览器零告警、零业务请求错误，清理零残留。该场景不扩张为长周期投影到期或高可用验收；Platform CI 结果另行记录。

Transfer 派生查询拒绝验收核对 `query_output_lineage_unresolved`、failed 终态、本次执行身份及零写入。类型化错误、完成事务和专业响应的敏感信息保护统一见[Transfer bounded 失败诊断](../transfer/CLAUDE.md#bounded-失败诊断)。

四出口 v9 的停机与恢复步骤、拒绝连接探针、Backend 构建身份、Source Grant、原值授权边界和清理要求统一见[Security 控制面实际不可达场景](../docs/spec/testing/scenarios/数据保护.md#security-控制面实际不可达)。固定版本运行结果按以下记录分别认定。

本扩展已在提交 `c322167b8` 的 [2026-10-12 Hosted T4 38219829612](https://github.com/pampa0629/addp/actions/runs/38219829612) 首次真实通过 v9：两个 Security 停机窗口、14 个前后拒绝连接时间点、四 Owner Backend 实例和构建身份保持、Security 新实例恢复及普通 User/Tenant 核验均通过。MySQL 五行的 Develop 查询、Service REST Query 与从空表开始的 Transfer bounded snapshot 均移除邮箱，目标客户标识保持；MongoDB 七行在 SM3 下完成 Develop 嵌套字段、空值与相邻属性校验，计数仍为七、派生查询不返回结果，Transfer 可证明查询落表与不可证明查询零条／零字节写入均通过，拒绝使用安全诊断 code 而非原始错误文本。MongoDB 十个任务及执行逐一核对、全部删除，Baseline 恢复、临时 Policy 撤销；既有三算法、重启恢复与 Console 检索／定位／预览／CSV／Monitor 链路同时通过，浏览器零告警及零业务请求错误，四张截图已目视核对。独立 v9 报告核对与 Hosted 应用、业务容器、Infra、凭据清理均通过，零残留；[同提交 Platform CI 38219801688](https://github.com/pampa0629/addp/actions/runs/38219801688) 全部 33 项通过。此证据只覆盖上述有效投影下的实际出口，不扩张为其他出口原值授权、投影自身到期或全平台高可用验收。

四出口 Hosted v10 进一步验证显式重新发现后的投影同步与实际输出：普通用户沿既有纳管 version 命令创建 discovery execution；四 Owner 的稳定投影 ID、递增 revision、新 published cursor 和本次发现完成事实须同时成立；时间按 API 规范接受带时区 ISO 8601 值，统一换算为 UTC 比较，拒绝无时区值及过旧回执。当前回执齐备后重新读取四个出口并核对 Transfer 新执行身份与实际目标。有效期延长及完整 365 天区间由 Security 编译器 T1 单独验证；不扩展治理响应、产品角色或自动续期，不将此证据表述为自然到期 T4；该 Hosted 套件的临时普通账号需具备既有 `security.enrollment.update` 命令资格，其他套件与源数据 Grant 不变。该扩展仍走现有 Hosted suite 和资源清理。

提交 `fabac4bcb8` 的 [2026-10-13 Hosted T4 38335798164](https://github.com/pampa0629/addp/actions/runs/38335798164) 已真实通过 v10，并完成下载报告的独立核对：正式重新发现保持源结构和四个投影 ID 不变，四 Owner 均安装更高 revision 与新 cursor，当前回执覆盖本次发现完成时间；Manager、Develop、Service 及 Transfer 新执行均输出五行，邮箱字段移除、客户标识保持，Transfer 实际读写均为五行且目标预览一致。既有两个 Security 停机窗口、14 个拒绝连接时间点、MongoDB 三算法及十个任务清理同时通过；Console 检索、定位、预览、CSV 和 Monitor 页面报告通过，零浏览器告警、零业务请求错误，四张截图已目视核对。领域资源、Hosted 应用、业务容器、Infra 和凭据清理均通过，零残留。该结果不覆盖后续提交 `b8ce75038` 新增的治理有效期展示、资源筛选和银行卡号识别，也不计为自然到期或全平台高可用验收。

Hosted T4 `security-mysql-owner-protection` 使用独立的准备身份，通过 System 正式 `access_grants` 命令原子初始化独立批准要求并给普通消费账号签发精确表的只读 Grant。范围仅包括 MySQL `customers`、`bank_card_accounts`、MongoDB `Outdoor.Persons` 集合，以及 PostgreSQL `spatial_algorithm_source`、`spatial_algorithm_transfer`、`mysql_email_transfer`、`mongodb_query_transfer`、`mongodb_query_control`、`mongodb_query_denied`、`bank_card_transfer` 七张表，共十个精确读取 Grant；不授予目录、未来表或消费者的授权管理权限。准备令牌在进入四 Owner 场景前移除，普通账号继续同时受功能权限、源数据授权和 Security 规则约束；整套夹具随 Hosted Job 清理。夹具身份、精确授权请求和准备令牌隔离回归由既有 `make test-online-runner` 自动发现，真实链路由 `online-t4-gates.yml` 的同名套件验证。

MongoDB 查询搬运验收复用上述 suite、普通 User 和标准 `bounded + snapshot` 链路：未纳管时先验证派生查询能落表，纳管后三算法分别验证透明 aggregate 的嵌套字段别名、`$ifNull`、SQL NULL 和相邻字段；不可证明输出血缘的派生查询必须在保护准备阶段拒绝，写入条数、字节数为零且独立拒绝目标前后为空。Manager 独立预览 Policy 不改变 Transfer 默认 Baseline。提交 `41bd12252` 的 [Hosted T4 38133692022](https://github.com/pampa0629/addp/actions/runs/38133692022) 已真实通过 v8：8 个任务及执行逐一核对、任务全部删除并确认 404，Baseline／Policy 恢复及 Hosted 清理零残留；不把该 suite 结果扩大为全仓 CI 通过。

空间标量算法验收按当前 Owner 输出协议解析 Manager 表格预览的 WKT，以及其他 Owner 查询输出的 GeoJSON 或原生十六进制 EWKB，并逐行比较二维 Point 坐标；非法类型、维度及改变后的坐标均失败。源表与结果表的 geometry 类型、主几何列及 SRID 4326 另由 Meta 空间事实核验；空间解析错误须标明 Owner 和行 ID，不能被算法校验或清理日志掩盖。

重新纳入保护的 PostgreSQL 生命周期回归使用带纳秒尾数的固定时间夹具，先校验落库后的微秒精度，再以操作前读回的审计时间作为精确比较基线；不得把数据库读回值直接与 `time.Now()` 的纳秒输入比较，也不能通过放宽时间误差来掩盖退出审计被修改。该回归继续由 `make test-security-postgres` 和现有 `security-postgres` CI Job 执行。

`make test-security-postgres` 除迁移与服务事务外，还运行默认保护 HTTP 契约集成回归：使用真实 Handler、Service 和 PostgreSQL，验证敏感定义与初始规则原子创建、非法规则无残留、完整写响应与递增版本、更新和删除冲突的稳定错误码、显式重读后再次写入、初始规则删除限制及租户隔离。测试仅在标准测试库事务中重建 `security` schema，结束回滚；身份上下文是可信测试夹具，不代表真实登录、权限中间件或 Gateway 已完成联调。CI 继续由 `release-and-t2-gates.yml` 的 `security-postgres` Job 调用同一入口；真实跨模块验收仍归独立的 Online T4。

表单弹窗只在开始新会话（`open`）或用户显式加载最新基线后清除旧校验；`opened` 和焦点恢复只负责聚焦，不得清除动画期间已经产生的必填提示。浏览器回归通过延长打开动画、在动画结束前提交空表单来验证提示持续可见。

候选复核的会话边界由 `useProtectionFindingReview.prepare` 统一拥有：首次打开、重新打开和成功后连续复核下一个候选都在准备新候选时清理旧校验；关闭或销毁后忽略待执行的界面初始化。`FindingReviewForm` 的 `clearValidate` 与 `focusRationale` 分离，挂载和弹窗 `opened` 只聚焦。不得让聚焦方法隐含表单重置，也不得通过延迟用户提交或增加测试重试规避动画竞态。现有 `make test-security-frontend` 与 Platform CI 同时覆盖动画前空提交、动画后提示保留、重开清理和连续复核会话，不新增测试入口或环境依赖。

标准入口为 `make test-security-frontend`，依次运行确定性测试、Playwright 浏览器回归和构建。浏览器测试保持零重试，失败时保留截图与 `trace.zip`，成功时不保留轨迹。结果存放于系统临时目录（CI 设置 `TMPDIR=RUNNER_TEMP`）下当轮独立的 `addp-security-playwright-results-*`；成功退出时删除当轮目录，重跑不覆盖此前失败产物。

默认保护编辑由独立浏览器用例覆盖识别方式到默认保护的抽屉切换、必填空值提交，以及取消按钮、Esc、遮罩点击三种退出路径。退出后必须恢复原值、清除旧校验并保留父抽屉；Esc 和遮罩关闭还验证焦点回到编辑按钮。鼠标遮罩关闭不能依赖 Element Plus 默认恢复焦点，默认保护弹窗在 `closed` 后显式聚焦本次触发按钮，不改写组件库的焦点陷阱。需要单独诊断时，在 `security/frontend` 运行 `npm run test:e2e -- --grep 'default protection editor'`；这些用例仍由标准前端门禁自动运行。

`platform-ci.yml` 的 Security 前端门禁失败后上传 `security-browser-failure-<run_id>-<run_attempt>`，保留 14 天。下载并解压产物后，可在 `security/frontend` 运行 `npx playwright show-trace <trace.zip绝对路径>`，检查操作时序、DOM 快照、网络与浏览器控制台；一次重跑通过不能证明偶发问题已经修复。

默认保护提交从校验开始到保存及列表刷新结束使用同一 `saving` 状态：禁止重复提交、修改表单，以及取消、关闭按钮、Esc 和遮罩退出，避免旧响应关闭新编辑会话。保存失败时解除限制并保留输入供重试；成功刷新后再关闭。延迟响应浏览器用例同时覆盖失败重试、保存成功和刷新尚未完成的阶段。

默认保护保存失败提示只读取 API 标准错误体中的非空字符串 `error`，以普通文本展示；不展示诊断 `detail` 或 Axios 通用 HTTP 错误。断网、缺少原因、空白或非字符串原因使用现有 `security.common.failed` 本地化词条。浏览器回归同时验证服务端原因和这些异常响应，保持输入可编辑且不自动重试。

默认保护写入结果与列表刷新结果分开处理：创建、更新成功后先采用响应中的完整规则和新 `version`；删除成功后移除对应本地规则。刷新失败显示持久的“变更已保存”提示并提供只读重试，不误报保存失败、不重复写入，也不允许基于未刷新列表继续编辑。初次加载失败不能显示成空列表。版本冲突按 `resource_version_conflict` 判断，保留草稿并阻止旧版本再次提交；只有用户显式选择放弃输入并加载最新规则后，才替换编辑基线和恢复提交。加载失败保留原草稿，组件卸载后忽略迟到响应。

默认保护工作区向敏感类型父页面发布按类型限定的最新规则快照，用于更新默认保护摘要；父页面不得因此再次全量读取定义。写入响应、显式冲突重载和列表读取成功均更新这份展示快照，不另存业务保护事实。

## 必读规范

- `docs/concepts/addp数据安全与隐私保护体系图.md`
- `docs/spec/addp数据安全与隐私保护实现规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp-Swagger集成指南.md`

普通账号资源组合筛选 T4 复用现有 Console 登录与 `security-mysql-owner-protection` 浏览器流程：使用同一普通 User 的正式 AuthContext，逐项核对名称、引擎、生命周期状态及列表范围的请求参数、返回成员、分页总数和实际表格。覆盖正确组合、错误引擎、保护中与已停止保护状态的空交集、百分号字面匹配、范围切换及重置恢复；当前四出口回执、有效期和页面截止时间同时核对。筛选阶段仅允许 Security GET，不创建或修改纳管、策略、授权和角色。跨模块整页导航后，验收 API 必须从本次浏览器成功业务响应的 Authorization 绑定当前会话，并重新核对同一 User、Tenant 和完整权限集合；禁止继续使用导航前捕获的旧令牌，也不回退准备身份。新增三张筛选截图，浏览器报告升级为 v4、场景报告升级为 v12，严格校验新增字段；既有客户检索、预览、CSV、Monitor、银行卡和停机回归保持完整；沿现有前端、Online runner 及 Hosted 入口发现，1200 秒总预算不变。普通账号组合筛选 T4 结论须以完整报告及截图的独立核对为依据，真实结果见下文记录。

## 数据库启动所有权

本模块 schema 迁移仅由 Backend 执行，Worker 只读校验成功提交的 schema 版本。Backend 多实例通过模块级数据库锁协调。开发脚本和 Compose 在所属 Backend 就绪后启动 Worker。结构或启动迁移变化须递增模块 `SchemaVersion`。共享规则见[开发服务生命周期与构建身份规范](../docs/spec/addp开发服务生命周期与构建身份规范.md)。

结构化投影启动补齐只处理 `enrolling` 和 `active`，先锁定并核验 Enrollment 生命周期，再解析 Owner 投影。已解除的投影保留作审计，升级时不得为已解除资源补建 Graph 投影或恢复保护。既有期限回填取原 Owner 最早截止时间，保留各投影原日期、修订与 feed 位置，重复运行不延期、不改变资源版本。旧协议转换、重放和初始化以[现行 Projection v5 契约](../docs/spec/addp数据安全与隐私保护实现规范.md#八protection-projection-v5)为准。期限回填由 SQLite 与 `make test-security-postgres` 覆盖，启动补齐另有 Security Go 与 PostgreSQL 生命周期回归。

## 早期保护验收结论与缺口

以下结果按固定源码与实际执行范围认定。后续的 [v44 四出口与清理](#v44-完整-hosted-验收与清理闭环2026-11-08)和 [v46 分组与角色保护](#v46v10-分组与角色保护-hosted-闭环2026-11-13)分别记录了新版覆盖。旧运行仍可逐轮核对。[本节原始记录（固定提交）](https://github.com/pampa0629/addp/blob/911024dd7f1e77d1c24d0e6d964d59ccbe2e09a4/security/CLAUDE.md#L216-L500)

### 资源筛选、银行卡识别与有效期

- **资源筛选。** 普通 User 的九组只读组合筛选已核对字面子串、百分号字面匹配、引擎、生命周期、范围交集、计数／分页及重置恢复。同步状态和当前期限独立展示。前一轮页面整页导航后沿用旧 API 客户端，Meta 返回 401，未完成筛选验收。两轮均记录隔离清理零残留。[v12 通过，源码 `7a6476b3d`](https://github.com/pampa0629/addp/actions/runs/38477006785) · [v12 首败，源码 `dc8d4520f`](https://github.com/pampa0629/addp/actions/runs/38471195140) · [现行资源治理规则](../docs/spec/addp数据安全与隐私保护实现规范.md#受保护资源治理与银行卡号元数据识别)
- **银行卡识别。** v11 验证元数据检测仅命中明确命名的字符串字段，Finding 与 Assessment 指向同一敏感类型。四个数据输出 Owner 核对五行掩码、Unicode、短值、NULL、空串和相邻反例，Transfer 实际写入五行并重扫回读，临时资源和 Infra 零残留。该轮银行卡结果来自 API 与 Worker，截图覆盖既有客户数据。检测器只依赖字段元数据，卡种、号码有效性、校验位和银行卡页面另有验证边界。首轮在旧 900 秒总预算内等待邮箱索引解除隔离时耗尽，缺完整报告。[v11 通过，源码 `ea1379586`](https://github.com/pampa0629/addp/actions/runs/38433500090) · [v11 首败，源码 `333cc6ae5`](https://github.com/pampa0629/addp/actions/runs/38425834669) · [敏感发现规范](../docs/spec/addp数据安全与隐私保护实现规范.md#六敏感发现执行)
- **有效期和来源准备。** v13 验证同修订、同进程下的资源自然到期，四出口拒绝，Transfer 零行／零字节且目标不变。显式恢复长期并同步新修订后，恢复五行脱敏结果，旧版本写入被拒绝。初版实际验证 System 210 前向迁移；后继通过的精简夹具只为七张 PostgreSQL 表准备精确源许可，MySQL 和 MongoDB 仍按当时的路径完成扫描与保护。该授权观察仅适用于 v13，后续 v44 使用的三目标六项许可另有证据。早期前置失败分别是未配置独立 System 服务身份导致读取检查 503，以及 Meta 将业务分支误当无父节点入口导致 500。两轮都未进入相应保护输出，退出清理零残留。[v13 源码 `05ea0c789`](https://github.com/pampa0629/addp/actions/runs/38835960352) · [v13 精简夹具 `dd3d39890`](https://github.com/pampa0629/addp/actions/runs/38840651279) · [503 首败 `061e8420f`](https://github.com/pampa0629/addp/actions/runs/38704771989) · [Meta 500 `9fed427f4`](https://github.com/pampa0629/addp/actions/runs/38830258272) · [现行有效期规则](../docs/spec/addp数据安全与隐私保护实现规范.md#资源保护有效期) · [现行隔离夹具](../scripts/docs/Online数据保护.md#hosted-精确许可准备与清理)

### 原值访问与 Develop 历史

- **现行结果。** 原值授权按用户与获批出口独立生效。Develop 获批的当次结果有界交付，历史始终保留默认脱敏预览，需重新查询才能按当前授权查看原值。v6 验证授权有效时当次可见原值、同一历史默认脱敏，以及自然到期、撤销、另一用户隔离和 Security 不可达。Console 浏览器证据覆盖申请审批与 Manager 保护状态，Develop 当次／历史分工来自领域 API 和工作台门禁。本机迁移核实旧私有历史键为零、既有脱敏预览可读，Hosted 清理通过且 Infra 零残留。[v6 源码 `293fdafe2290955c15272056b627c8fad721be2a`／Run 39041158723／Job 117420768709](https://github.com/pampa0629/addp/actions/runs/39041158723/job/117420768709) · [Develop 现行契约](../docs/spec/addp数据安全与隐私保护实现规范.md#102-develop-当次原值结果与脱敏历史)
- **旧规则及日志缺陷。** v5 曾实际采用私有历史实时重判，获批后的原值历史曾进入 GORM 参数展开日志。修复通过本机实际写入与 Hosted 获批写入核对，SQL 保留占位符、类别和过程记录，原值及私有载荷不进入日志。v5 当时核对了双用户、独立出口、到期／撤销回落及退出零残留。更早两次 Hosted 分别卡在 Graph 前端默认端口和撤销投影尚未在 Manager 生效的浏览器交接，两轮清理通过。旧日志、截图与备份副本处置仍按已确认决定搁置。[v5 通过，源码 `188da46aed6e39b83cb540fcc66d0ca5c4d915e2`](https://github.com/pampa0629/addp/actions/runs/39028729141) · [端口前置失败](https://github.com/pampa0629/addp/actions/runs/39018432898) · [撤销同步失败](https://github.com/pampa0629/addp/actions/runs/39024702645)

### 字段策略、拒绝阶段与页面证据

| 固定源码及实际运行 | 当轮新增覆盖 |
| --- | --- |
| `3d4176f168633f92187de49f0be4427bff50936c` · [v14／Job 116879250398](https://github.com/pampa0629/addp/actions/runs/38872372087/job/116879250398) | MySQL 别名的默认保护保持。派生查询在准备阶段拒绝，Develop／Monitor 可按同一 UUID 查到安全诊断及无结果。首轮将 MongoDB 血缘拒绝误期望为结果阶段，页面未执行，清理通过。[v14 首败 `8d91f3b14`](https://github.com/pampa0629/addp/actions/runs/38868796504) |
| `6e31b58175d0c022f575480873e3197ff6622428` · [v15／Job 116924746894](https://github.com/pampa0629/addp/actions/runs/38886293712/job/116924746894) | Service 到期返回 403／`data_protection_required`，Transfer 到期或派生拒绝采用准备阶段安全诊断、零新增写入、目标不变。Provider 类型化缺表或 SQLSTATE 仍归查询错误。缺少反例的候选在排队时取消。[未执行的候选](https://github.com/pampa0629/addp/actions/runs/38885678273) |
| `68d7cbd0311f51e00fc97c821c737f9c36d65f43` · [v16／Job 117250246594](https://github.com/pampa0629/addp/actions/runs/38983726735/job/117250246594) | 首次真实浏览器核对 Service 与 Transfer 的到期拒绝、清空旧预览、恢复安全输出和同 UUID Monitor。Service 按发布字段抑制 email，Transfer 原生整表保留合规的十一列。前两轮分别误用 Monitor 的 `triggered_by` 校验 Transfer 的 `trigger_by`，以及误用 Service 字段清单校验原生整表。两轮均未进入到期页面，清理通过。[发起人断言失败 `f93c7928`](https://github.com/pampa0629/addp/actions/runs/38919249803/job/117034601396) · [列断言失败 `7a8bce58`](https://github.com/pampa0629/addp/actions/runs/38923900689/job/117050436707) |
| `8025781f5c825b00f3b9046441c236d9d4d7e768` · [v17／Job 117263317878](https://github.com/pampa0629/addp/actions/runs/38987833025/job/117263317878) | MongoDB 短值触发 Transfer **结果**阶段保护拒绝，专业详情和 Monitor 记录同一 UUID，零新增写入、目标不变。原基线恢复后重新执行成功。 |
| `7207a59f714e2d27ca633d67b1c4c50680239b67` · [v18／Job 117703147439](https://github.com/pampa0629/addp/actions/runs/39136902282/job/117703147439) | 普通用户验证三个空间表普通属性字段、四出口独立策略、重启保持与撤销回落。该轮空间表范围限属性保护。五个必要 Owner 按完整集合核验同步，四个输出出口各自核验输出。 |

上述成功轮均有 Hosted 生命周期清理与 Infra 零残留。v18 之前的四次 Hosted 失败保留原结论：初次搜索在 Manager checkpoint 初始化前 403，下一轮首批技术索引未投递导致空命中并耗尽预算，随后领域脚本等待的 Owner 少了 Graph，最后浏览器仍使用旧四项清单。四轮均已清理，不计作新增字段策略通过。[初始 403 `645ebd416`](https://github.com/pampa0629/addp/actions/runs/39087749235) · [总超时 `cd2803e94`／Job 117661462679](https://github.com/pampa0629/addp/actions/runs/39122735286/job/117661462679) · [领域屏障 `fb13955d8`](https://github.com/pampa0629/addp/actions/runs/39127891317) · [浏览器清单 `441e4e5ca`](https://github.com/pampa0629/addp/actions/runs/39132512437)

- **分层门禁。** v15 同源码的 Service 与 Transfer PostgreSQL 实际通过。候选的 Go workspace 曾因 pgx 直接依赖声明漂移失败，修正后已实际复验通过。v16 Hosted 成功时，对应 Platform consistency 仍在 PostgreSQL 调用字符串断言处失败。[v15 Release/T2](https://github.com/pampa0629/addp/actions/runs/38886250264) · [Go workspace 修复 Job 116936933536](https://github.com/pampa0629/addp/actions/runs/38890009152/job/116936933536)
- **本机与受控验证。** 2026-10-15 至 16 日的本机 MySQL 副本核对了默认保护、CSV、Develop 和 Service，以及普通用户中文页面的到期拒绝与恢复。首次 MySQL 目标因 TEXT 主键限制未写入，后续 PostgreSQL 目标实际读写 2,188 行且到期时零新增写入。恢复长期后 Service 退回原停用状态，任务及脱敏目标留作复查。本机故障中出现的 `refresh_token_reuse_detected` 未由该次验收修复。英文真实页面和 Transfer 结果阶段另以上表 Hosted 证据认定。
- **治理界面。** 已有权限的用户通过评估历史查看各出口不可变策略修订，包括已撤销策略。缺 Policy 读取权限时不请求，读取错误明确展示，关闭或切换后丢弃旧响应。弹窗复用共享 `preserveDialogFocus` 保留打开过程中的用户焦点。受控前端门禁和本机只读复核通过，修订弹窗无独立 Hosted T4。策略适用动作、算法上限、撤销回落和原值授权限制引用[保护策略规范](../docs/spec/addp数据安全与隐私保护实现规范.md#71-protectionpolicy-首期语义)。

### SQL 输出保护与派生血缘

| 固定源码及实际运行 | 当轮新增覆盖 |
| --- | --- |
| `4145b1b92293c43210a4e0168a8ce57ecbb30ead` · [v19](https://github.com/pampa0629/addp/actions/runs/39275743465) | 单普通基表 `SELECT *` 与带别名通配符，逐值核对五行掩码、NULL／空值与非敏感字段。 |
| `3397631bfb828923a0a5de25e1b8d4854d190d55` · [v20／Job 118420568188](https://github.com/pampa0629/addp/actions/runs/39359032226/job/118420568188) | 直接字段／别名副本可与同表通配符混投，敏感副本分别保护。 |
| `726b2fc5ed2e67410d3c9ae151b05ff91f21b932` · [v21／Job 118524506515](https://github.com/pampa0629/addp/actions/runs/39391692620/job/118524506515) | 单层分页包装保持行选择，外层通配符与直接字段分别核对。SQL Service 三页 2／2／1，无重漏，末页停止。 |
| `fd4cd5f513b943086e24cae94da3b238ad1731ac` · [v22／Job 118614370212](https://github.com/pampa0629/addp/actions/runs/39418814791/job/118614370212) | Develop 六类敏感计算在准备阶段拒绝，独立 UUID 的同一历史无结果。SQL Service 敏感派生输出返回 403；`count(*)` 与非敏感计算对照通过。 |

上述 Hosted 成功轮均有独立报告核对与退出零残留记录。新版混合投影、单层分页、重复输出名拒绝和六类计算也另有本机真实 Develop 及历史复核。源元数据快照未变与源业务值核验分别认定。原生 PostgreSQL／MySQL 的结构锁和编译器按对应 T2 认定，其他 MySQL 兼容实例、复杂 SQL 与静态加密仍需各自证据。

早期 MySQL 原生 T2 曾重现表达式字段值来源漏记，合成结果出现原值，隔离夹具已清理。修复后明确区分直接、派生、可证明无字段依赖与不透明输出。受保护派生即使具备直接字段原值授权仍拒绝。复杂值来源需完整证明，产品规则与支持边界以[Owner 本地执行契约](../docs/spec/addp数据安全与隐私保护实现规范.md#十owner-本地执行契约)为准。

### 四出口逐原因矩阵与结构漂移

- **最终结果。** 固定 `f0f949b5123cb5ad2ff8778d015577d4a5bd0e42` 的 [Run 39727724444／Job 119667747241](https://github.com/pampa0629/addp/actions/runs/39727724444/job/119667747241) attempt 2 实际通过 v25／浏览器 v9。七原因四出口共 20 个真实运行格、8 个仅共享协议格。Manager／Service 核对 403 和稳定码，Develop／Transfer 核对同 UUID 历史及准备阶段原因，Transfer 另核对零写入与目标不变。保留 21 张截图，临时策略和基线已恢复，各类资源和 Infra 分别核对零残留。[attempt 1 的 Job 119655405669](https://github.com/pampa0629/addp/actions/runs/39727724444/job/119655405669) 在 Manager 前端安装时遇 npm ECONNRESET，未运行业务。[实际覆盖矩阵与 T2 边界](../scripts/docs/Online数据保护.md#四出口逐原因矩阵)
- **诊断范围。** v23 证明敏感计算、血缘无法验证、到期的具体原因，v24 补入未就绪、不支持操作、结构不匹配的 Develop／Monitor 同 UUID 和真实页面。尚未生效仍仅有真实 PostgreSQL T2，没有产品开始时间入口。v25 的空间表证据覆盖普通属性字段且保留坐标与 NULL；MongoDB 覆盖嵌套、稀疏、NULL、三算法、可证查询传输和 Canonical Extended JSONL，其他 MongoDB 公开输出及全类型另验。[v23 `8da847b503ff8ef2c6cc183d577db0d1cb5e7250`／Job 118720172659](https://github.com/pampa0629/addp/actions/runs/39449485758/job/118720172659) · [v24 `31bcd85f08b6ad305d85078002748c700284307a`／Job 118973549956](https://github.com/pampa0629/addp/actions/runs/39523768953/job/118973549956)
- **运行前结构复核。** Manager 读取原生表行前用实时完整字段核对保护投影。Transfer 在打开原生读会话及任何目标操作前核验精确来源的实时结构，结构不符或缺失沿既有准备阶段拒绝。真实 PG 回归中旧实现已触发目标重建与写入，修后为零读写且目标 OID、行数和内容不变。两者均未认证描述与读取之间的并发 DDL 原子性。

| v25 前序实际失败 | 保留的判断依据 |
| --- | --- |
| `dc36584fdebf87879d62b5f116600d2bd1116989` · [119303398341](https://github.com/pampa0629/addp/actions/runs/39623669811/job/119303398341) | 当时 MySQL Provider 缺 Transfer 查询读会话，任务创建 400。原响应正文缺失，独立复现定位配置校验，未运行保护准备拒绝。 |
| `b100eda7b803d180e0db53297331ba618de11b67` · [119341995199](https://github.com/pampa0629/addp/actions/runs/39634714694/job/119341995199) | 高置信 Finding 自动采用默认保护，未就绪夹具的人工复核假设不成立。原状态正文缺失，修订后的探针使用完成发现且零 Finding 的精确 PG 源。 |
| `4f394dd95d08e5c18dd051a5e7dd566545c4d2f4` · [119403048973](https://github.com/pampa0629/addp/actions/runs/39652172676/job/119403048973) | PG 字符串过滤合法，旧探针实际重建目标并 1 读／1 写；该轮未保存行值。后续 PG 对照确认合法过滤只写掩码值，拒绝探针按实际方言选择。 |
| `8caece4d70b1adc6b5edf6ece56e1d8eabd8a653` · [119505065215](https://github.com/pampa0629/addp/actions/runs/39681568226/job/119505065215) | Manager 结构漂移预览返回 200，原响应正文缺失，未直接证实原值。 |
| `b703950106e174bff02ec236f31e5c3689546670` · [119570484821](https://github.com/pampa0629/addp/actions/runs/39701169370/job/119570484821) | Transfer 原生源结构漂移仍成功，目标重建且 5 读／5 写，原目标行值未归档。 |
| `c0153c89a23b988bc5788e6476cd61c9ab73558e` · [119606039075](https://github.com/pampa0629/addp/actions/runs/39712245598/job/119606039075) | 汇总先于 MongoDB 证据完成，产生 KeyError，领域报告与浏览器未完成。 |

这些失败轮的退出清理与业务结论单列，当时未完成的矩阵保留失败。v24 的 Platform 登记也曾因负例没有变异实际版本标记而失败，后继实际门禁已验证修正。[首败 `f5449d860`／119984762601](https://github.com/pampa0629/addp/actions/runs/39527270182/job/118984762601) · [修复 `a9309a84`／119147047035](https://github.com/pampa0629/addp/actions/runs/39577642649/job/119147047035)

## 执行身份约束的角色事实接口（2026-10-22）

用户确认默认保护不随角色叠加改变，角色仅增加可选方式资格后，已补齐异步执行所需的 System/Common 前置接口：`POST /api/v1/system/execution-authorizations/{id}/subject-roles`。仅同 Tenant 的 Develop/Transfer Owner 可按真实 running execution、授权 ID、attempt 和 lease 核验真实 User 发起人的当前角色；Develop→DuckDB 仍由 Develop 读取，Runtime、其他 Owner、错误 Tenant/主体/成员/版本、失效授权或租约拒绝。Orchestrator 子执行核对父来源；响应只含绑定引用、有效角色键并集、观察时间和上界期限，不返回权限清单、连接或登录 Token，不写业务配置或历史。无功能 Permission 的角色仍参与资格集合；新角色触发 IAM 版本变化时旧执行授权仍拒绝，重新建立当前授权后才读取新集合。原始数据读取与原值审批规则不变，不以本接口授予新的数据效果。

规范见 `docs/spec/addp授权上下文规范.md`“执行发起人的当前角色核验”和数据安全实现规范 7.1.1。实现为 System `execution_subject_roles.go`、API adapter 与 Common `system_execution_subject_roles.go`；沿用现有执行授权权限，无新增迁移、角色权限分配或通用角色枚举 API。角色选项控制面、投影编译、保存查询/服务/传输的选择引用与四出口执行/界面尚未接入；本项不能记为角色脱敏功能完成。

验证：`make test-module MODULE=system GO_ONLY=1`、`make test-module MODULE=common GO_ONLY=1` 通过（仅 Go T1）；`make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package iam --test execution-subject-roles"` 在已核实的 25432/addp_iam_test 通过真实 PostgreSQL T2，无跳过，复用完整 ExecutionAuthorizationService 用例，新增当前角色、无权限角色、自然到期、Owner/租户/身份/租约/父执行边界。前两轮测试夹具分别因变更角色后沿用旧授权、非法成员期限，以及修改不可变角色分配期限而失败；已改为正式新会话/新授权与创建时确定期限后的自然到期，不关闭数据库约束或放宽断言。`make test-system-iam-runner` 27项通过；System Swagger 生成与覆盖检查通过（241个公开路由方法）。标准 IAM T2 默认发现仍覆盖本用例，新增 filter 只用于最小充分本地回归，未新增 CI 路线。日志 `/tmp/p145-role-{system-t1,common-t1,system-t2,runner,swagger}.log`。尚未进行本接口的本地 HTTP/浏览器或 Hosted T4，未重启用户服务、不定时、不跟进 CI；四出口原有逐原因 T4 不作为本接口证据。


## 2026-10-22 角色可选方式控制面与 Projection v5

按已确认的“默认自动保护、角色只增加选择资格、多角色取并集”原则，在原 ProtectionPolicy 不可变修订增加 `role_options`，不新增角色策略聚合或优先级。Security 为新选项生成 UUID，更新只接受当前修订已有身份；完整集合随默认效果同事务校验、保存、编译，撤销保留审计配置但不发布。每项仅可采用当前基线许可且类型匹配的内置 mask 决策，不能穿透字段默认 suppress/deny；基线撤回算法后移除不再许可的选项，普通默认保护继续有效。投影选项绑定 policy_id、policy_revision、assessment_revision；Common 显式解析器区分不可用/已变更与当前角色不具资格，角色取并集，不自动切换默认算法，也不把显式选择替换为原值豁免。调用者仍须先核验主体、Tenant、来源字段和当前角色事实；传入字符串集合本身不是认证证明。

唯一运行协议为 `addp.protection_projection/v5`，旧版本只用于一次性启动迁移，不保留运行兼容分支。Security 从可信 Enrollment 前像原位迁移中央记录和历史变化，保持序号；共享投影存储新增 `005_projection_role_options_v5`，清除旧缓存与所有 checkpoint 后完整重放。新增回归实际发现“只清 cursor 而保留 initialized=true”会提前认为就绪，已改为一并清除 checkpoint 并通过 SQLite/真实 PG 验证。Security 与 Transfer 外层启动 schema 均递增至 6，避免 Transfer 已应用旧 schema 时跳过共享迁移。其他 Owner/Provider 夹具只切换当前协议常量；运行时默认保护、原值授权、SQL 语义和源数据均未新增宽松路径。

既有字段策略表单复用 ProtectionAlgorithmEditor 配置角色选项；角色候选只读取现有 System 租户角色目录，沿用 `iam.tenant_role.read`，只选择可分配给 User 的 Tenant 角色，不增加权限或自由输入角色键。目录无权读取或失败时已有选项保持只读、可保留或显式移除，不能因加载失败清空。修订历史显示选项身份对应的名称、角色键、算法参数；版本冲突仍由原显式重新加载流程处理。内置角色译名及名称/说明解析迁入 common-frontend 唯一维护，System 三个 IAM 组件直接复用，原重复函数和转发入口已删除；自定义名称保持原输入。Security 创建/更新/详情 Swagger 同步，上一轮执行主体角色接口的已生成 System Swagger 同次补入提交。

验证：Common Go T1 最终标准入口 `make test-module MODULE=common GO_ONLY=1` 通过；Security Go T1 标准入口已通过，新增 HTTP 及字段策略用例继续由共享 Go/既有 PG 入口发现。`make test-security-postgres` 在已核实 25432/addp_test 完整通过 8 个顶层用例，无失败或跳过，含四出口选项编译、默认不变、修订身份、基线收紧、撤销审计与 HTTP 未知选项身份拒绝、显式移除和跨租户隔离。`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test projection-store"` 真实通过 7 个顶层用例，无失败或跳过，覆盖多 Owner 同构、v4 缓存重放、初始化、并发版本以及既有 28 格逐原因协议；这是投影存储组，不计完整 Common PostgreSQL 门禁。该标准脚本只抽取原完整门禁中的同一函数，默认全门禁保持原组序；新过滤及拒绝跳过/零测试由 `scripts/test/module-gate_test.py` 34 项验证。最初全仓 Go 在新增初始化断言处红，修后 `GOMAXPROCS=2 GOFLAGS=-p=1 make test-go` 完整退出 0（依赖 tidy、全部已跟踪 Go 模块及 Common 无 CGO 过程指标）；补充的 Common 最终入口覆盖其后的评估修订断言。全仓日志 `/tmp/p145-options-go-final.log`；两次完整 Common PG 尝试因临时 guard 发现并发而终止，不计通过；后来精确投影组经过同一 guard 完成，不宣称共享主机独占。

Security 237 个单元、原 33 个完整浏览器回归及构建通过；随后新增角色目录无权读取/失败保留场景，与多角色配置合计 3 个最终专项再次通过并构建（现有 35 个不同浏览器场景未合并伪称一次全跑）。System 155 单元、203 完整浏览器及构建通过；删除旧转发入口后，155 单元、8 个角色管理/角色分配/账号专项与构建再次通过。Common Frontend 257 项通过；投影存储所有权 9 项/4577 Go-SQL 文件、T2 登记检查以及 Security/System Swagger 63/241 路由覆盖通过。日志 `/tmp/p145-options-{common-t1-final,security-t1,security-postgres-final,projection-postgres-final,module-runner-final,store-ownership,t2-registration,security-frontend-final,security-role-browser-final,security-build-final,system-frontend-final,system-role-browser-final,system-build-direct,common-frontend-final,swagger-coverage-final}.log`。

边界：本轮完成控制面、v5 发布/重放和共享显式选项解析，不将 Develop/Service/Transfer 的业务保存选择、运行时选择与同次默认历史预览计为完成；Manager 普通预览继续默认方式。尚无本轮本地实际产品或 Hosted T4，不复用旧四出口 T4 为角色能力背书。后续保存及同步消费还需完整的当前登录用户角色事实：现有 AuthContext 是带 Permission 的角色投影，会漏掉零功能权限业务角色；不能据此暗中缩小已确认的角色并集。该窄接口的调用边界已向用户提出，未擅自扩大角色枚举或改变认证协议。未启停用户服务、不设置定时、不跟进其他 CI、不改变源授权、原值授权或栅格。清单大项仍部分实现，由 p0 维护表格。

2026-10-22 本人角色自查与 Develop 业务选择：补齐仅当前第一方 Tenant User 的 `GET /system/auth/subject-roles`，共享现行有效 Assignment 查询并包含没有功能权限的业务角色，不扩张 AuthContext v1、数据权限或原值授权。Develop 候选只读取显式数据项，选择以 `content.protection_choices` 保存精确字段与选项修订；保存／仅改名称更新、自身实时结果读取与绑定 EA／attempt／lease 的后台执行均复核当前资格。多个角色提供选项并集，不改默认方式；过期、撤销、修订变化、未消费来源与不支持的输出明确拒绝。角色结果与默认历史由同次 PreparedQuery 形成，SQL 原生变换只延后所选字段，其余沿共享 SQL／Go 路径；暂存严格有界、逐字段剥离到期原值，CAS 与独立计时修订防止旧观察恢复原值或延长保留。其他出口的默认保护不变，特殊方式尚未接入其他 Owner；全量导出暂拒绝特殊方式查询，不静默替换选择。

验证：`make test-go` 全 22 个 Go 模块及 go.mod/go.sum 通过；之后保护规划／暂存细化分别由最终 `make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=develop GO_ONLY=1` 补验通过，System Go T1 通过。真实数据库入口 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package iam --test execution-subject-roles"`、`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test query-read-set"`、`make test-common-mysql-data-protection COMMON_MYSQL_TEST_ARGS=--local-disposable`、`make test-develop-postgres` 通过；PG／MySQL 分别 14／5 个新增原生角色方式用例，覆盖直接、别名、wildcard、混合、分页、选择优先于原值豁免、同次独立默认预览、当前角色／修订拒绝及输出元数据保真。IAM 覆盖纯业务角色、旧版本、自然到期及登出；Develop 真实 PG 覆盖默认历史无原值、最终复核后的快照和过期租约不进入交付缓存。PG 只用标准入口及 `addp_iam_test`／`addp_test`（实际端口 25432），无跳过；Common PG 仅 query-read-set 组，不计全门禁。一次前置 PG 验证因并发守卫主动终止，未计通过，随后独占复验通过。

前端：`make test-common-frontend` 259 项；`make test-develop-frontend` 确定性检查、92 项浏览器及构建；`make test-monitor-frontend` 确定性检查、289 项浏览器及构建；另按既有 `npm run test:e2e -- e2e/execution-diagnostics.spec.js --grep protection_role_choice_unavailable` 补验中英文 2 项新原因，均通过。Develop 新用例覆盖选择保存、重载执行不重复询问、修订变化不自动换方式、主动恢复默认与 390 px 窄屏；两张图目视核对中文，临时证据 `/tmp/p145-role-choice-visual-h4kqk_vt`。初轮发现的组件声明时序、夹具返回类型及浏览器亚像素／动画等待问题已修正并复验；MySQL 断言已按实际原生 VARCHAR→LONGTEXT 事实校正为保护值／列一致、语义类型与精度一致，并精确保留同次 Provider 返回元数据，未放宽保护断言。共享投影唯一所有权 9 项及 System／Develop Swagger 覆盖 247／60 路由通过，Swagger 检查单测 18 项通过。T1／T2／隔离浏览器 T3 不等于本地真实账号或 Hosted T4；本轮未重启用户服务、未手动派发／跟踪 CI、未创建定时，实际业务 T4 待加载新版后验收。记录与日志 `/tmp/p145-choices-*`。

2026-10-22 23:50，本人角色与 Develop 选择生命周期的本地 MySQL T4 通过。用户重启后，System／Develop／Security 均 ready，Develop／Security registered，三个构建提交均为 `673cf8eef7cb776de26a0b04c8dbdc86384becbf`，包含实现 `50446c027`；相关保护源码无变化，验收前后三个 build ID 未变。无痕 Chrome 的「outdoor开发者」在 `business.security_persons_acceptance_20261015` 执行五行直接字段查询：默认保护 UUID `b259fc2c-aaeb-40a1-a835-6914a4f95f2d`（46 ms）返回前 3 后 4 掩码及四个 NULL。只读核对该账号已有 10 个有效角色，包含数据工程师；未调整角色成员或数据授权。

本轮临时增加仅数据工程师可显式选择的 `p145验收-全遮盖`（前后均保留 0 字符），字段默认算法、其他出口与原值许可均不变。保存查询定义 #26 `p145_role_choice_acceptance_20261022` 后，实时 UUID `7cc501ec-5d46-483a-8033-0e73d5579d87`（74 ms）返回全遮盖与四个 NULL；同一 UUID 的 Owner 历史显示默认掩码，未出现原值。新页面重新打开定义，SQL、来源与一项已保存选择均保留，直接执行再次返回全遮盖（Monitor #768，64 ms），无需再次选择。

撤销本次临时策略并同步后，旧选择执行 UUID `497c0e1e-515d-43dc-90fe-4854f860393e`（Monitor #769，46 ms）在准备阶段明确拒绝、零行；Owner 历史无结果，Monitor 同 UUID 原因及建议一致，明确提示并非数据读取权限不足。刷新方式后失效引用仍保留并要求主动移除，没有暗中改为默认。通过主动移除选择并更新定义后，五行默认保护再次成功（Monitor #770，86 ms）。临时角色选项已撤销，查询出口默认保护同步完成，其他出口不变；保留已恢复默认的无调度验收定义 #26 和不可变审计修订，没有写入业务源或调整源授权／原值授权。

临时证据 `/tmp/p145-role-choice-local-20261022/readiness.json` 与截图已校验完整性并目视核对关键实时／历史／拒绝图。本地 T4 仅证明第一方现有多角色账号的一项 MySQL 角色选项生命周期；纯业务角色、选项并集、成员角色撤销、原值豁免冲突仍沿用前述 T1／T2／T3 证据，不扩大为新增 T4。MQL、其他 Owner 的角色业务选择及 Hosted 角色 T4 本轮未验。部分旧标签页导航出现停滞，通过同一登录会话的新标签页继续，未修改产品绕过认证。此次仅补验收记录，未重复有效代码门禁、未重启服务、未跟进 CI 或设置定时，清单大项继续部分实现。

2026-10-23 08:10，补核专门 CI 会话交付的四项固定范围归档，无新增派发、重试、取消、轮询或本地服务操作。Online [39872011655](https://github.com/pampa0629/addp/actions/runs/39872011655)，精确提交 `6ca313c0628ada381fb3e441c32e064a2cbd9900`、attempt 1：Compose Job `120148447106`、Metric TiDB `120148447390`、Metric PostgreSQL `120148447393`、openGauss `120148447472` 均 completed/success；各自 Run Hosted 门禁步骤实际执行并成功，非仅 readiness 或条件跳过。正式 artifacts `12467438628`／`12467681512`／`12466809401`／`12468255564` 的运行与 SHA 元数据、Job 上传记录、online-report 和 summary 逐项一致，四份 summary 均 cleanup passed、Infra zero_residuals。Compose 浏览器归档证明 Cookie 轮换、iframe 收敛／刷新保持和退出传播，未持久保存 JavaScript token；两种 Metric 的 count_distinct 与 sum_decimal_by_group 均实际完成初始查询、不自动重绑定、撤回拒绝和显式重绑恢复，临时资源残留 0。openGauss 正式报告与 online-gate.log 中实际报告一致：Manager／Develop／Service 初始 5 行、最终 6 行，阶段内 checksum 一致；Transfer 初始 5、增量 2、空恢复 0，创建 2、删除 2、残留 0。

独立只读校验命令 `python3 /tmp/p145-fixed-scope-39872011655-audit.py` 通过，固定四项及 18 个证据文件散列见 `/tmp/p145-fixed-scope-39872011655-audit.json`；原始正式归档位于 `/tmp/addp-ci-20261023-000013/scheduled-{compose,metric-tidb,metric-pg,opengauss}`。另核实本会话文件尾空白修正 `d8ced917a0c8293548b94e4c944bfbae664ab86e` 的 Platform [39811301682 / Job 119937956271](https://github.com/pampa0629/addp/actions/runs/39811301682/job/119937956271)，consistency 与其 Check changed commits for whitespace errors 步骤均实际成功。本轮新增的是这些既定合成集成场景的通过证据，不单独解释旧 data_protection_required 的历史根因，不证明所有引擎／字段算法、角色消费、MQL 或静态加密，也不新增 openGauss 脱敏 SQL 下推覆盖。已只读核对清单实际行 155／156／158，现有摘要未保留这四项旧 CI 阻断；父项继续部分实现，不因集成通过提升功能程度或重复追加流水。当前开发中的过程事件等其他会话工作不纳入本轮提交。

### Transfer 角色选择接入前的原生字段保护基础（2026-10-23）

用户已授权继续接入 Transfer 角色脱敏选择和全量导出。本轮先落地不依赖新增身份边界的共享读取基础：`projectionstore.PrepareTableProtection` 显式接受同一请求作用域的角色字段选择，复用查询既有的选项解析、来源消费校验和算法，不制造查询或复制算法。空选择继续默认保护；有选择时来源／字段／动作未命中、当前角色不具资格、观察到期或选项修订变化均拒绝，不静默默认。每批输出（含空批）在修改结果前复核当前投影选项；独立 `ApplyDefault` 保持默认预览。Transfer 两处现有调用显式传空选择，接口旧签名删除，当前产品默认行为保持。规范和 Common 边界说明同次更新。

验证实际通过：`make test-module MODULE=common GO_ONLY=1`（Common Go T1），`make test-transfer-backend`（Transfer 全后端 T1），最终输入上的 `cd common && go test ./dataprotection/projectionstore -count=1`，以及 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test projection-store"`（真实 25432/addp_test，8 个顶层用例、0 失败、0 跳过，不计完整 Common PG 门禁）。新增 `TestProjectionStoreNativeRoleChoicesAgainstPostgres` 自动由现有门禁发现，复用同一断言覆盖默认／SM3、NULL、多角色增加、Tenant／出口／字段／修订隔离、无受管目标拒绝、资格失效及准备后修订变化拒绝、默认保护恢复。`make test-projection-store-ownership` 9 项及所有权扫描通过；本轮相关文件 `git diff --check` 通过。日志在 `/tmp/p145-native-roles-{common-go,transfer-go,projection-go,postgres,ownership}.log`。没有新 HTTP／浏览器 T4，不新增 CI 入口，不派发或跟进 CI，不启停本地服务。

Transfer 任务选择／保存／执行与 Develop 全量导出尚未接通，不记为完成。只读核对发现普通 Transfer 执行及模块导出目前未绑定可供现有 subject-roles 接口核验的用户执行授权；导出只保存 User ID 不等于当前异步角色资格。已向用户提出：仅角色专用操作按当前登录账号签发本次 Transfer 执行授权，再按 execution/attempt/lease 读取角色，不保存登录令牌、不借用后台或发布人身份；这样选择专用方式需要现有 system.execution_authorization.create、transfer.task.execute 和对应引擎效果资格。该权限／调用边界待用户确认，未擅自补权限或放宽 System。查询与导出仍为独立动作，查询选项不能直接当作导出选项；已提出分别保存一次、以后复用的方案。默认导出、源授权、原值授权与栅格均未调整。后续优先确定上述执行身份边界，然后接入任务及导出定义、共享选择组件与实际输出验收。

### Transfer 角色选择与 Develop 全量导出接入（2026-10-23）

用户已确认执行身份边界。本轮接入当前第一方登录用户手动发起的 bounded/snapshot/native table 及其 PreparedQuery 来源：Transfer 的 `config.source.protection_choices` 与 Develop 的 `content.export_protection_choices` 均只保存精确源字段／选项身份及修订，独立于查询的 `content.protection_choices`。未选择采用本出口默认保护，不跨出口复用查询选项。Transfer 向导复用共享 FieldMaskingChoices，默认折叠且不查询角色候选，已有选择重新打开时显示并复核；Develop 同一抽屉分别展示查询与导出方式，保存、重载及新 execution 快照保留两套引用。只改名称也必须复核，旧引用、角色失效、错误来源／出口、切换不支持形态不能静默丢弃或改回默认。新查询／导出方式不属于原值授权。

手动任务启动／重试、角色专用全量导出在同步用户请求内签发本次 Transfer EA，校验真实 Principal、Tenant、Membership／Version 和确切 execution UUID；要求现有 system.execution_authorization.create、transfer.task.execute 及实际引擎读写效果资格，未增加任何权限或 Role Assignment。Worker 使用用户 EA 获取源／目标引擎访问，按 execution/attempt/lease 逐批读取 System 当前角色；保护门禁统一只读 Runtime Descriptor，不再为判断保护领取连接凭据。源字段在映射和 Writer 前由 Common 保护；无选择的默认路径不增加角色或用户 EA 依赖。定时、委托、编排、continuous、增量、encoded/record 来源不消费角色专用方式。原有默认 MongoDB Extended JSONL 不因此扩张为角色导出。

共享导出会话新增可空保护引用（只有源 locator／字段选择），由现有 EnsureStore 单一路径安装，旧默认会话不补造资格；EA 事实纳入完整请求摘要，仍由精确 Owner 会话核对发起人，User ID 本身不成为执行权限。Get/Open 对特殊会话必须提供当前用户核验回调；文件生成成功不替代下载资格，选择或角色失效拒绝。特殊文件经现有同一路由的 User Bearer 与共享认证下载工具交付，拒绝外部／带票据 URL，不把令牌放入 URL；纯 Resource Ticket 不代替完整角色核验。默认下载保持原行为。权限不足按 401/403 返回，选项或角色资格冲突按 409 返回，不把 IAM 拒绝伪称敏感字段保护失败。Swagger 同步，文件路由仍遵守原 resource_ticket 入口声明，特殊交付条件写明于 Description，未放宽覆盖检查。

验证：Common 完整 Go T1 `make test-module MODULE=common GO_ONLY=1`、Common 新传输／会话／选择回归 `cd common && go test ./client ./exportartifact ./dataprotection`、Transfer 完整后端 `make test-transfer-backend`、Develop 完整 Go T1 及最终 `go test ./internal/api ./internal/service`、Manager 完整后端 `make test-manager-backend` 均通过。新增回归覆盖保存和仅改名称更新、精确权限／角色冲突区分、Owner 会话与独立 EA／真实发起人绑定、逐批新观察、失效后交付拒绝、未知字段／角色断言／空选择／不支持形态，以及两套业务引用独立快照。

真实 `25432/addp_test` 上 `make test-common-postgres` 完整门禁、`make test-transfer-postgres`、`make test-develop-postgres` 均通过，不以 T1 中跳过的外部服务用例冒充 T2。Common 的旧会话加列／新选择持久化由既有 TestExportExecutionSourceAgainstPostgres 覆盖；Transfer 已登记的 TestIntegrationPostgresBoundedExportMasksBeforeTargetWrite 新增原生／查询两种真实读写分支，目标实际收到所选 SM3，模拟角色失效时零读写且原目标不变。这是 T2 合成当前角色事实，不是实际 IAM 任免／撤销 T4。Develop 首次调用标准入口缺少 DSN，补齐同一已核实测试库参数后完整通过，不将缺参前置失败记为产品问题。

前端 `make test-common-frontend` 266 项、`make test-develop-frontend` 单元／92 项浏览器／构建、`make test-transfer-frontend` 单元及构建通过；Develop 双语角色场景在同一已登记用例中实际保存、重载、执行两套选择，查询选择失效后明确改回默认不清除导出选择。共享下载新增当前账号调用、外部／票据 URL 拒绝和受控本地化错误回归；最后下载提示微调另经共享门禁及 Develop 构建通过。`make test-authorization test-projection-store-ownership`、Swagger 生成与路由覆盖通过（Transfer 38／Develop 60）；所有权门禁 9 项及 4659 个 Go/SQL 文件通过。初次授权覆盖指出 resource_ticket 不允许 conditional Permission 声明，已依现有单一路由协议修正注解并完整重跑通过，没有放宽断言或增加认证旁路。新增测试均由现有 Go、T2／前端标准入口发现，无需新 Makefile／CI 登记或新外部依赖。

日志 `/tmp/p145-transfer-role-{common-go,common-targeted,transfer-go,develop-go,develop-final,manager-go,postgres,transfer-postgres,develop-postgres,common-front,develop-front,develop-build-final,transfer-front,service-test,export-tests,contract-gates,swagger,swagger-coverage}.log`。当前代码／T1／T2／T3 接入已实现，但本地新版真实账号及 Hosted 角色导出 T4 尚未执行，尤其 MySQL／MongoDB 角色专用导出不借用旧默认保护 T4 作证。后续优先加载本轮 Common／Owner 构建，验收 MySQL 已有 Persons 测试表的任务选择／全量 CSV／失效拒绝与显式默认恢复。未启停用户服务，不设置定时或派发／跟进 CI；源授权、原值授权与栅格均未改动，清单父项继续部分实现。

### MySQL 查询全量导出与能力提示（2026-10-23）

Develop 查询全量导出使用成功执行的冻结快照及同一 PreparedQuery 的 `QueryReadSession`，Manager 原生表全量导出使用 `TableReadSession`。Develop 前端按当前引擎实例及查询语言启用入口，后端在签发 Execution Authorization（EA）或创建会话前再次核对同一实例的 Runtime Descriptor。能力缺失返回 `422/query_export_unsupported`，普通查询仍可用；描述读取失败返回 503，权限拒绝与角色选择冲突各用原有状态码。MySQL 在只读 repeatable-read 事务中消费单一结果流，保留用户显式 LIMIT 和逐批保护，关闭或取消时回收会话。完整行为见[Develop 查询全量导出](../develop/CLAUDE.md#查询全量导出)及[引擎查询读取会话契约](../docs/spec/addp引擎插件接口规范.md#queryruntimeprovider)。

**门禁与原始失败。** 旧构建的普通查询和保存导出方式成功，四次实际导出均因缺少 MySQL QueryReadSession 失败，未创建 Transfer 执行或 CSV。修复后的真实 MySQL T2 走 `make test-common-mysql-data-protection COMMON_MYSQL_TEST_ARGS=--local-disposable`，核对 1,007 行单流、业务 LIMIT、快照隔离、输出保护、取消和只读防写，临时资源清理为零。相关模块 Go、Swagger 及能力开关的受控浏览器用例通过。全 Develop 前端门禁为 91／92，`graph-fit-view` 缩放断言失败。全仓 Go 首次在 Ontology 来源漂移处停止，后续按实际语义更新 Transfer 修订 32，来源门禁及相关 Go T1 复验通过。委托携带角色选择继续拒绝，普通停止态任务创建的权限和操作范围保持。[Ontology 来源复核](../ontology/CLAUDE.md)

#### 本机真实 CSV 与撤销交付

构建 `dfa4ad16105d0421af13e55f3e4099acdd6751a2` 包含 MySQL 查询流式读取修复。普通开发者 #33 用同一 MySQL Persons 来源核对三个边界：

- **完整结果与独立导出方式。** 查询仍按默认保护显示。去掉业务 LIMIT 的查询预览为 500 行并提示截断，导出会话 #8 由同一用户执行，实际读写并下载 2,188 行。CSV 四列及 2,188 个唯一 ID 核对通过，1,121 个非空 phone 全遮盖、1,067 个为空。Transfer 执行 `78103957-6697-45be-a71e-5193bcc05888`。
- **新导出拒绝与默认恢复。** 撤销方式并同步后，保留旧选择时默认五行查询仍成功，新导出返回 409，未创建会话。显式移除导出选择并保存后，原 SQL 的 LIMIT 5 保持，默认导出会话 #9 实际下载五行，一个 phone 为前 3 后 4 掩码、四个为空。Transfer 执行 `3375a11c-2030-4684-8902-70a8e82797c8`。
- **旧文件的再次交付。** 同一个五行角色会话 #10 首次下载成功，撤销后、Transfer 投影更新前再次请求仍返回 200。在 Transfer 确认修订生效后重发原文件请求，Develop 与 Transfer 检查均实际返回 `409/query_protection_choice_invalid`，响应只有安全错误 JSON，无 CSV。该检查没有创建替代会话，手段为浏览器重发同一正式请求。当时页面未提供已有会话的再次下载按钮。此前保存到客户端的文件仍保留，临时方式已撤销、定义选择已清空。Transfer 执行 `06f09be1-dd60-44fb-99af-cbccf20f5b6f`。

完整文件审计、请求记录及图片分别见 `/tmp/p145-transfer-role-local-20261023/` 与 `/tmp/p145-redownload-local-20261024/acceptance-report.json`。瞬时的新导出拒绝没有完整截图，判定来自实际 HTTP 409 和页面可访问性文本。

#### 同租户会话隔离

固定本机构建 `e931905933bab26701e2586eb8df9a49d9da4a30`。开发者 #33 与管理员 #31 都能读取本人会话、实际保存同一查询的五行默认脱敏 CSV，管理员仍受默认保护。双向交换会话 ID 后，详情和文件四次均返回 404；两条文件响应及管理员读取对方详情的正文为 `query_export_not_found`，开发者读取管理员详情仅留状态证据。正常文件下载使用产品 Resource Ticket；交叉检查各用自己的 User 请求。实际下载验值的是 #11 与 #13，#12 只用于会话生成与交叉检查。报告见 `/tmp/p145-export-isolation-local-20261024/acceptance-report.json`。范围仅为同租户两名第一方用户、指定 MySQL 查询和五行 CSV，跨租户、其他格式与其他引擎另验。

#### 独立原生 Transfer 与目标建表

MySQL 新目标首轮在建表时实际返回 1170／42000。源主键为 `varchar(191)`，通用字符串目标映射为无键长度的 TEXT 主键。目标表不存在，读写计数未记录。这个失败属于物理目标定义，作为该次运行的未解决项保留。失败执行 `be831017-8e04-495a-9f57-e1f2f14cd876`。

后续同一第一方用户经正式向导使用同一 MySQL 源，手动 bounded／snapshot 写入三个新 PostgreSQL 目标，分别证实默认、全遮盖和显式清空选择后的恢复。三次均实际读写 2,188 行并逐行对照。默认两轮为 863 个标准掩码、258 个短值与 1,067 个空串按异常值抑制输出 NULL；角色轮为 1,121 个等长星号、1,067 个 NULL。保留失效选择再次启动时返回 409，执行数及原目标保持；角色轮使用该用户 EA，默认两轮无角色专用 EA。三条成功执行依次为 `da8ec17e-782a-4307-9b4a-74dac532ac22`、`3f435992-042e-4c7c-ad17-9da8d835f095`、`adb4d1d4-fddf-48b0-8514-543b77f2a100`。

本机任务最终停止且清空选择、无调度，临时方式撤销。三张脱敏验收表及执行审计留作复查。逐行核对见 `/tmp/p145-native-transfer-20261024/acceptance-progress.json` 与同目录各物理审计。

**覆盖边界。** 本节保留的是指定本机 MySQL 查询导出、撤销投影生效后的同会话交付拒绝、同租户隔离和 MySQL 源到 PostgreSQL 新表的原生任务。MySQL 目标建表失败、定时／委托／编排／连续增量、其他组合和对应 Hosted 结果按各自证据认定。后续默认文件规则变化见[导出文件保护变更失效](#导出文件保护变更失效2026-10-27)，角色资格自然到期见[Online 资格到期与旧文件](../scripts/docs/Online数据保护.md#transfer-资格自然到期与旧文件)。逐阶段记录及原件索引见[整理前的固定版本](https://github.com/pampa0629/addp/blob/ad09944650e300e73aeb38b801c1ea1152e99d39/security/CLAUDE.md#L348-L406)。

## 导出文件保护变更失效（2026-10-27）

按用户确认，默认与角色专用用户导出共同绑定 Transfer `export` 的实际来源和保护投影。Worker 在原生表／同一 PreparedQuery 的准备阶段，以 checkpoint 版本和当前 lease 将精确来源、投影身份／修订／checksum 与有效期写入现有 execution 私有 metadata；无选择的默认导出仍不增加角色或原值授权依赖。Transfer 在相关 upsert／release 的同一事务中不可逆标记旧记录失效，按 100 条分页处理；无关目标不影响文件，重复同修订同内容不误伤，恢复原规则不复活旧文件。Get/Open 每次读取当前模块自己创建的 Transfer execution result 的受控 `export_protection_current`，缺失、失效或自然到期均拒绝；执行成功审计保留，原文件仍沿既有暂存清理。旧执行没有记录时须重新导出，不设兼容放行。角色资格／选项原有精确原因继续优先保留，其他保护失效统一409/`export_protection_changed`及中英文重新导出提示，保留 `no-store`。Manager／Develop 的默认与角色下载统一使用已有认证 Blob 客户端，交付阶段拒绝显示错误，不保存成文件；不修改普通源文件、瓦片或栅格下载。

真实 PostgreSQL 首次暴露既有同修订投影按字符串比较的问题：JSONB 规范化键顺序及空白导致内容相同被误报修订冲突。已复用 Common 严格 JSON 解码按完整对象内容比较，保留精确数字及未知字段，拒绝重复键／不同内容，未放宽修订裁决。首次失败日志 `/tmp/p145-export-version-transfer-postgres.log`；旧 cached-success 路径的反向回归确实失败，日志 `/tmp/p145-export-version-common-red.log`。首份 HTTP 对象夹具曾缺 Last-Modified，补齐协议后验证，不将夹具失败记作产品保护失效。

实际验证：`make test-go`（JSONB修正后的全仓 Go T1）通过，后续 Transfer 内部及清理断言扩展由 `make test-module MODULE=transfer GO_ONLY=1` 再次通过；`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test projection-store"` 的真实投影存储组及完整 `make test-transfer-postgres` 通过且无跳过，新增 TestIntegrationPostgresExportArtifactProtection 覆盖未纳管后纳管、无关表、同修订幂等、同事务回滚、规则更新／release 后不可复活、自然到期、重新加载、原生／PreparedQuery 两条真实准备钩子及101个文件跨批失效，回滚核对所有夹具 execution 与临时 schema 零残留。该组不是完整 Common PostgreSQL 引擎门禁，不记为实际用户 T4。

`make test-common-frontend` 284项、`make test-develop-frontend` 110项受控浏览器及构建、`make test-manager-frontend` 387项组件／单元与281项受控浏览器及构建通过。新增 Develop 默认方式的中英文 status／file 409场景确认有明确提示且无文件下载。Swagger 用当前已提交基线加本会话源文件在临时副本走标准生成，隔离其他会话未提交字段变化；94／60／38条 Manager／Develop／Transfer 路由覆盖通过，顺带同步基线已有注解的生成差异，无对应产品行为变更。已有模块／前端标准入口和 Go CI 自动发现本次文件；Transfer PostgreSQL 标准入口同次登记新增用例，无新 workflow 或验收 suite。

本轮不启停服务、不更改实际角色／原值／源授权，不派发、轮询或定时跟进CI。恢复 Docker 后完成上述 T2；新构建实际 HTTP CSV 与旧文件拒绝仍待用户重启全部模块后验收，隔离 Hosted 由专门CI会话跟进，均未计为通过。Excel 实际行155/B空（动态脱敏与静态加密执行总述）、156/B155（动态脱敏实时生效）父项仍为部分实现，由p0统一维护。日志：`/tmp/p145-export-version-go-jsonb-final.log`、`/tmp/p145-export-version-common-projection-postgres-green.log`、`/tmp/p145-export-version-transfer-postgres-final.log`、`/tmp/p145-export-version-transfer-delivery-final.log`、`/tmp/p145-export-version-common-frontend.log`、`/tmp/p145-export-version-develop-frontend.log`、`/tmp/p145-export-version-manager-frontend.log`。


2026-10-27 补充收口：4c新增ExportSession私有保护记录触及Ontology已捕获的Transfer service整文件来源，但漏同步平台来源审查，专门CI会话报回漂移。已在本地用 `make test-platform-review` 强制非缓存复现，按平台定义规范逐项复核：CreateTask停止态创建语义/权限/效果不变，新增初始化仅在已验证模块来源的CreateAdHocExecution中，运行和再次交付继续未建模。平台修订34→35，只更新service真实提交4c及摘要/用途，保留所有来源、覆盖和绑定；定义其余字段逐项相同。非缓存来源门禁、Ontology/Transfer Go T1均通过，未扩展业务规则、IAM或发布内核；不将此前通过记录冒充该门禁已通过，不将本次本地结果记为CI恢复或新构建实际CSV通过。详见 ontology/CLAUDE.md 和 `/tmp/p145-export-platform-review-audit.json`；CI及待重启后的实际CSV边界保持。


2026-10-28 修后 CI 回传归档：专门 CI 会话已核实真实 Job 终态，本会话仅复核其保存的 Job 元数据与原始日志，未联网查询、重跑或触发 CI。修复提交 `79fa05e7b9907b0ad978ce8716ac050b56314a80` 的 [Go workspace Job 122248960679](https://github.com/pampa0629/addp/actions/runs/40521342460/job/122248960679) 实际执行 `Run all tracked Go module tests` 并成功，原始日志含 `ontology/internal/platform 0.676s` 通过；包含该修复的后继 `7ebf98370ebc965e09cb5744983dbf8ee7fbbbce` 的 [Platform consistency Job 122257071998](https://github.com/pampa0629/addp/actions/runs/40523616999/job/122257071998) 实际执行 `Run platform consistency gate` 并成功，同包 `0.176s` 通过。独立核对两份日志 SHA-256 与专门会话审计一致、Job/run/head SHA/attempt 精确匹配、成功步骤实际完成非跳过，并验证修复提交是后继来源的祖先。因此本次4c引起的来源漂移已有修后实际 CI 恢复证据；旧失败记录保留，不推广为当前或未来 HEAD 的全部 CI 通过。

证据：`/tmp/addp-ci-20261027-160029/Transfer_79fa_review_source_actual_CI_recovery_audit.json` 与对应 `job-122248960679.log`、`job-122257071998.log`；本会话独立复核 `/tmp/p145-export-source-ci-return-independent-audit.json`。此归档仅补来源审查门禁恢复，不能替代新构建的实际 CSV、旧文件拒绝与重新导出恢复验收，也不证明修订35已在运行环境激活。用户已确认重启且四个相关 Backend 曾核对包含4c；当前尚未收到已有 `/tmp/p145-run-export-protection-acceptance.sh --temporary-protection-cycle` 的运行报告，CSV验收保持未验证。本轮仅文档归档，复用已完成测试，不重复产品门禁，不新增任务、调度、配置或权限，不操作本地服务。


### 默认导出保护变更的本地实际 CSV 收口（2026-10-28）

用户重启后，沿 Gateway 正式登录 outdoor-develop（User33、Tenant1）及 addp-owner（User31、Tenant1，完成现有 TOTP），对既有 MySQL `business.security_persons_acceptance_20261015` 完成默认 Manager 原生表全量导出与 Develop 同一直接字段 SQL（LIMIT5）导出的三阶段实际 HTTP 验收。四个相关 Backend 的 ready 响应均记录构建 `13b34fb5f22e28f932ae633d7780ebdd615f2b6d`，包含产品修复 `4c9e1e53ee1e895c398dd0e0aaa46de54d832422`；不据此推断 Worker 的独立构建 SHA 或本体修订35已激活。

42项请求与42个唯一 request ID逐项关联对应 Owner 的运行日志路由／状态，实际生成6份CSV：每阶段 Manager2188行、Develop5行，独立复核CSV字节摘要、列序、唯一行标识、非保护字段及保护结果。默认／恢复阶段Manager均为863个掩码值和1325个空单元格，Develop为1个掩码值和4个空单元格；临时全遮盖阶段Manager1121个非空值均只含星号、1067个为空，Develop仍1个全遮盖和4个空单元格。全遮盖与默认参数不同，不要求两阶段非空数量相同；恢复默认的两份CSV摘要与各自初始文件完全一致。

精确既有 Transfer/export 覆盖策略临时 active 后，两个初始会话的状态／文件在中英文下均409/`export_protection_changed`；覆盖恢复 revoked 后，初始与全遮盖四个旧会话仍全部拒绝，合计24次真实拒绝，响应保持 `no-store`、明确重新导出建议且无文件附件。重新导出两份默认CSV成功。独立只读核对6条真实Transfer执行均success、读取数等于写入数、精确DataItem与三阶段保护版本绑定；前四条私有记录不可逆invalidated，恢复后的两条有效。执行成功历史保持，未用下载拒绝改写原成功审计。Owner日志使用路由模板，精确会话对应由冻结的脚本循环、会话／execution绑定及私有保护记录共同核对，不声称日志展示了原始会话路径参数。

策略由原revoked/version6经active/version7恢复revoked/version8，两条新修订均为User31的本次唯一验收原因；保留策略修订、执行与导出会话审计，文件沿既有暂存生命周期保留，不声称零残留或追回用户已下载副本。两份临时登录均正常logout204。未修改角色、源授权、原值授权或源表数据，未自行操作服务生命周期或跟进CI。

前两次临时脚本失败均保留：首次遗漏管理员既有MFA步骤，在登录阶段退出；第二次Manager请求漏带已有Meta item_id，未取得字段快照而在保护准备阶段fail-closed，未写出CSV。已按现有认证契约补齐MFA／上下文响应解析，Manager来源与正常UI及既有成功脚本一致携带item_id44573，未放宽保护校验或改变产品代码。临时脚本11项回归及语法检查通过。独立审计初次因两批旧文件复用检查名称而拒绝，后按冻结顺序、42个唯一请求与实际Owner日志逐项校验，仍严格要求24拒绝／6物理CSV／6执行及恢复事实；未修改原始运行报告。

证据：`/tmp/p145-export-protection-http-20261027/acceptance-report.json`、同目录`independent-runtime-audit.json`、`request-endpoint-evidence.json`及6份CSV；独立命令`python3 /tmp/p145-export-protection-independent-audit.py`退出0。失败材料及脚本修正证据为`/tmp/p145-export-protection-http-20261027-failed-login/`、`/tmp/p145-export-protection-http-20261027-failed-missing-source-snapshot/`、`/tmp/p145-export-protection-login-fix-audit.json`、`/tmp/p145-export-protection-source-fix-audit.json`。本轮新增的是限定默认出口的本地真实HTTP证据，不计为浏览器实际验收或隔离Hosted通过，也不替代角色专用文件、其他引擎／格式、自然到期、全量Service续页或在途并发撤销验证。相关投影在Owner同步生效前仍有既有窗口。清单实际行155/B空总述、156/B155动态脱敏实时生效继续部分实现；优先将本轮同一矩阵纳入既有隔离套件，CI由专门会话跟进，静态透明加密及通用SQL等既有缺口保留。


### 默认导出失效矩阵接入隔离套件（2026-10-28）

既有`security-mysql-owner-protection`领域契约升级v33，浏览器v9不变。复用合成银行卡五行表、现有普通治理账号、既有Meta Locator和Transfer/export覆盖策略，新增`default_export_protection`：Manager原生表与同一Develop直接查询快照各做默认／全遮盖／恢复默认三次真实导出；6份CSV在保护及相邻字段／Unicode／空值断言后保存于已有artifact目录，逐次复核实际Transfer UUID、trigger_by、五行读写和非任务定义来源。两个初始会话在规则更新后、全部四个旧会话在恢复默认后分别校验中英文status/file精确409/export_protection_changed、无结果／附件及重新导出建议，共24拒绝，最终42个边界使用会话／动作／语言和独立请求UUID作为身份；轮询另外逐次校验no-store。新文件恢复按记录值比较，不依赖原生表未声明的物理行序。

临时策略只在自己的Assessment和Transfer/export绑定下用当前版本变更。已有活动覆盖或并发他人变更时不接管；中断及PUT／POST／DELETE结果不确定时只读回、核对本次唯一原因和版本后撤销本次覆盖，不重试写操作，不覆盖他人事实。身份、基线和原查询快照保持。保留6会话／执行、策略审计及CSV，沿既有隔离Infra最终销毁；不新增权限、账号、授权、部署、清理接口或T4入口，保留原有各出口及物理清理边界。公共测试HTTP客户端仅增加Content-Type、Content-Disposition和X-Request-ID的保存及两种验收语言，禁止收集Cookie／Token；产品API及Swagger不变。

验证：`make test-security-online-runner`退出0，含38／58／31／293项Python回归、System合成身份Go门禁和CI登记检查；新增6个领域测试覆盖完整6CSV／24拒绝、全部42边界分别缺失／public／矛盾缓存头（126个反例）、错误原因／原值或默认值误替代／发起人或读写数错误／旧文件复活、缺Meta标识、现有活动策略拒绝、首次创建／未知写结果恢复、中断恢复及并发清理拒绝。HTTP客户端的成功JSON／CSV／错误响应验证安全头保留、Cookie不留存及未知语言在发请求前拒绝。定向反例通过不计作真实HTTP；日志`/tmp/p145-default-export-runner-targeted.log`与`/tmp/p145-default-export-runner-full.log`。现有Make入口与固定Hosted suite自动发现本次变更，CI注册及其删除反例同步为v33，无新增workflow。此前Persons的本地42项实际验收保持有效；新增v33真实隔离Hosted尚未运行，由专门CI会话跟进，不自行触发、轮询或设置定时。清单155/B空和156/B155仍部分实现。

### 保护算法摘要与已有资源定位（2026-10-28）

针对 MongoDB Engine11 `Outdoor.Persons / userInfo.phone` 的界面疑问，只读核对当前未退出纳管记录、Assessment、Manager/preview 字段 Policy 与已安装投影：字段覆盖为 `addp.mask.sm3/v1`，Policy 修订3、Manager 投影修订7；默认基线仍是前3后4掩码。未读取源手机号，也未调整任何规则、原值授权或源授权。原界面只展示通用“脱敏”，且 Manager 强制携带 `action=enroll`，因此难以查到已有具体算法。

本次先补充正式规范10.5，再修正唯一前端路径：配置摘要复用现有算法、参数和异常值说明；Finding 出口摘要消费后端已返回的规则算法，独立保留同步状态，缺少算法显示信息不可用，不在前端重算有效保护。Manager 的“查看/配置”沿标准 locator 打开现有 Security 页面。Security 用当前用户读取的 Meta item 指纹及既有 current/engine 分页精确定位未退出记录，刷新目标后复用已有详情会话；只有完整查找确认不存在且具备创建权限才打开既有纳管窗口。错误、重复身份、不完整分页、释放后变更和失效异步请求不解释为未纳管。入口采用既有 enrollment.read 与 Meta 目录读取约束，无需创建权限。没有新增 API、权限、保护决策或兼容 query；Swagger 无变更。

T1/T3/构建验证：`make test-security-frontend` 退出0，260项单元、37项受控浏览器和构建通过，含默认参数／显式SM3／实际出口算法独立展示、中英文只读详情导航及无 Security 写请求；单元覆盖跨页目标、完整不存在、403／协议与身份异常、重复／释放记录、路由切换及销毁。`make test-manager-frontend` 退出0，391项单元、291项受控浏览器和构建通过；另通过既有标准脚本 `cd manager/frontend && node ../../scripts/test/frontend-browser-gate.mjs e2e/security-protection-navigation.spec.js` 验证新增3项 Manager 入口，覆盖只读可查看、仅创建／无读取权限不显示入口、精确 locator 且无 action、无 Security 写入。新用例由既有 testDir 自动发现，无需新增 Make／CI 登记。日志 `/tmp/p145-protection-clarity-security.log`、`/tmp/p145-protection-clarity-manager.log`、`/tmp/p145-protection-clarity-manager-navigation.log`；文档相对链接及限定路径 `git diff --check` 通过。

本轮为前端修复，无需后端重启；未把受控浏览器计作个人环境的真实输出或 Hosted T4。未触发／跟踪 CI，仍由专门会话负责。后续待办记录于 `docs/sessions/数据脱敏与保护后续工作专题.md`，优先建议讨论 Service SQL 单表直接字段与别名的角色方式切片；不扩大源授权、栅格或静态加密范围。


### 字段保护方式分层展示（2026-11-02）

针对默认掩码与 Manager 专用 SM3 被拼接成长句的问题，按正式规范10.5整理唯一展示路径：Finding 卡片首先显示后端返回的当前各出口算法及独立同步状态，再展示识别／治理依据；默认方式和每个出口的已配置字段专用方式分别成行，参数及异常值处理通过原生 details 展开。配置存在不等同于投影采用，前端不重算基线与Policy裁决；配置读取权限不足时明确提示，仍如实展示可读的出口事实。自动发现与人工指定字段共用 `EnrollmentProtectionConfiguration.vue`，删除两处旧长句渲染及只为该渲染保留的模型字段，保留策略编辑／恢复对话框所需的既有摘要。没有新增API、权限、算法或授权，Swagger不变，不需后端重启。

验证：`make test-security-frontend`退出0，262项T1单元、37项T3受控浏览器及生产构建通过。新增／加强配置与实际出口算法不同、不可读取专用配置、基线缺失、人工评估不捏造出口事实、两个消费组件唯一复用及当前规则优先的断言。中英文、深／浅主题、四个出口（Manager SM3，其余前后掩码）、参数初始折叠／展开及480px窄窗口无横向溢出经浏览器验证，四张合成夹具截图已目视核对；无Security写请求。初始新增结构模型测试实际失败，旧三栏／长句架构断言按获批新布局修订后通过；截图检查发现旧合成Finding未带正式评估说明，补齐夹具事实后完整复验，不改变产品保护行为。标准Make入口与现有testDir自动发现覆盖，无新增CI编排。日志 `/tmp/p145-protection-layout-{red,green}.log`，截图 `/tmp/p145-protection-layout-visual/`。限定路径diff及文档链接检查通过。仅T1/T3及构建，不计个人环境实源或Hosted T4；CI仍由专门会话跟进。


2026-11-02，修复 Manager 受保护剖析结果二次读取误拒绝。`ProtectProfile` 统一接收完整源字段快照，严格校验组件路径、类型和结构指纹后执行幂等统计移除，写前及读后共用该执行器；不要求已移除的敏感统计重新出现。保留当前投影及源授权检查，不恢复原值或旧采样路径。读取失败后不继续显示旧执行进度，重试只读取结果。Security 的 Manager 配置入口改称“数据预览与剖析”，说明预览脱敏／移除时隐藏敏感及父级容器统计，预览拒绝时拒绝剖析；不新增独立策略、算法或授权。

本轮验证：`make test-manager-backend` 通过；`make test-manager-postgres` 33 项真实 PostgreSQL 检查通过，新增保护前处理→持久化→解码→重复保护及结构漂移拒绝检查；`make test-manager-frontend` 405 单元、321 独立浏览器及构建通过；`make test-security-frontend` 262 单元、37 独立浏览器及构建通过。首次回归先重现旧缺陷；接口签名遗漏调用和新浏览器夹具缺少表类型事实已修正并通过最终完整门禁，不把初次失败记为通过。标准入口与 CI 自动发现已覆盖新增测试，无新增登记路线。

用户重启 Manager 后，Chrome 实际读取 PostgreSQL engine #2 / `outdoor.p145_native_default_20261024` / Meta #44720 的成功执行 `2043d7b3-167b-4956-ac1f-ddde49102e5a`：HTTP 200、字段数 3，仅 `_id`、`nick_name`、`gender`；样本 2,000、扫描 2,188，观察记录不指向 `phone`。切换预览／剖析后同一执行再次 HTTP 200，无新建执行响应。限定本地证据 `/tmp/p145-pg-profile-fixed-20261102/read-proof.json`、`repeat-proof.json`、`profile-result.jpg` 与日志 `/tmp/p145-profile-{backend-final,postgres-final,manager-frontend-final,security-frontend-final}.log`。原纳管记录 `03a9a977-7c55-4149-8573-b1023f92a3fa` 保持长期保护，本轮未更改任何源／原值授权或源数据。实际配置页刷新被浏览器文档响应暂停阻断，新配置说明只有独立浏览器证据，未声称个人 Chrome 配置页通过；未运行新增 Hosted，CI 继续由专门会话跟进。剩余优先项为未支持剖析来源的能力提示与具体错误分类。
### Transfer PostgreSQL 两表 JOIN 脱敏落盘（2026-11-06）

在固定提交 `c5ce9c1fed61e70946a8d35d556f91fcde00f32a` 的隔离检出中，以 `make test-transfer-postgres` 在本地 `addp_test` 验证 Transfer 规划器、保护 Gate、原生 PostgreSQL 读写，20 个主测试和定向子测试通过，零跳过。两条记录按原值排序和分页选出，目标列分别落盘掩码与固定替换，两列保留各自字段来源。计算右侧受保护字段时，准备阶段拒绝，已有表的 OID 和记录保持，新目标未创建。专用 schema 已清理。报告位于 `/tmp/p145-transfer-join.zsGYYA/verification.json`。

在固定提交 `df3efe83ec32bbc2c38eea96863b0704cf477cd5` 补验真实 Transfer HTTP 路由、bounded Worker 首次领取、执行详情与事件。正常执行读写各两行，两次敏感计算均返回 `preparation / protection_computed_output_unsupported`，读写均为零，物理结果与前轮一致。任务、执行和事件记录已回滚，专用 schema 已清理。使用 System 身份与引擎事实夹具，经同一 PostgreSQL 门禁的定向运行通过四个主测试，零跳过。此前全量门禁在 continuous 仓储检查失败，首次定向还遇到 PostgreSQL 恢复和测试夹具编译问题。最终报告保留各轮日志与三个执行 UUID，位于 `/tmp/p145-transfer-worker-join.RRZjuA/verification.json`。

提交 `9c0d82b51448138578e7a1cdecb80580c1d45fcb` 将三种场景纳入既有 Security Hosted 领域报告 v43。最终领域 190 项、登记 149 项通过。聚合入口的指标夹具清理测试超过 20 秒，保留失败记录。真实 Hosted 待专门 CI 会话回传。独立审核脚本以一组模拟产物与八个反例检查 SHA、运行步骤、UUID、拒绝计数和清理，位于 `/tmp/p145-v43-transfer-join-audit.py`。[Hosted 验收约定](../scripts/docs/Online数据保护.md#transfer-postgresql-两表查询脱敏落盘)


### v44 完整 Hosted 验收与清理闭环（2026-11-08）

**验收结果。** 固定源码 `bd68a07014353d5c4a693fd863b3a3428bfdf460` 的 [Run 42454542147／Job 128650659824](https://github.com/pampa0629/addp/actions/runs/42454542147/job/128650659824) 实际通过 v44／v9 的 27 阶段、21 张截图目视和完整清理。覆盖四出口保护拒绝与恢复、同构建重启、角色方式／资格撤销与资格自然到期、Service 参数修订与旧游标、Transfer 两表 JOIN 的成功落盘与拒绝零写入。默认导出核对 42 条响应及六份原始 CSV，规则变化后拒绝旧会话与文件，恢复后可新导出。六项临时许可均正式撤销，领域 23 建／23 删、两组浏览器各 2 建／2 删，Infra 零残留。[原始归档 13609052160](https://github.com/pampa0629/addp/actions/runs/42454542147/artifacts/13609052160)

**覆盖边界。** 七原因四出口共 20 个 runtime 格、8 个 protocol_only 格。尚未生效仅有真实 PostgreSQL T2。多数子场景原始 HTTP 正文、部分方式撤销回执的缓存头未归档。当前字段检索的证据边界见下文，Service 到期页面的具体提示见本页[中英文复验](#v44v10-中英文保护到期和恢复-hosted-验收2026-11-10)。

<a id="v44-hosted-前置元数据可见性阻塞2026-11-07"></a>
<a id="v44-三项元数据查看许可夹具适配2026-11-07"></a>
<a id="v44-metadata-only-与字段检索断言冲突2026-11-07"></a>
<a id="hosted-三项精确读取权临时前提2026-11-07"></a>
<a id="v44-六项许可实测与未就绪探针扫描前提2026-11-07"></a>

**来源准备。** 三个精确目标为 MongoDB `Outdoor.Persons` 和 MySQL `security_fixture.customers`、`security_fixture.bank_card_accounts`，分别签发 `metadata_read` 和 `read`。准备者独立于消费账号，按当前 Run、租户、主体、目标及动作核对六个请求 UUID，先撤 read 验证不可读，再撤 metadata_read 验证不可见。该验收使用临时 read，字段名只靠 metadata_read 检索的能力由独立产品证据认定。修复 `20d03f724` 已有 Manager T1、PG＋Meilisearch T2 和 Linux CI 记录，真实跨服务／浏览器仍需独立证据。[精确许可准备与清理](../scripts/docs/Online数据保护.md#hosted-精确许可准备与清理)

| 固定源码及实际 Job | 首败与清理 |
| --- | --- |
| `0af01095941be47c97d2d0383bcb69b23781fd68` · [128248930706](https://github.com/pampa0629/addp/actions/runs/42322135716/job/128248930706) | MongoDB 已扫描入库，消费账号缺元数据许可，未取得唯一 `Outdoor.Persons`。领域与页面未执行，生命周期清理通过，Infra 零残留。 |
| `5bcac0793dd85d505c436f2d6e1aa4f95991da80` · [128387317730](https://github.com/pampa0629/addp/actions/runs/42367089598/job/128387317730) | 当时仅 metadata_read 无法满足字段检索，领域与页面未执行。三项许可已撤销，Infra 零残留。 |
| `b4779ba443c42e423240132c332ccb08cbb8fd23` · [128422248189](https://github.com/pampa0629/addp/actions/runs/42378933245/job/128422248189) | 未就绪探针在目标 deep 刷新前纳管，缺预期发现身份。领域与页面报告缺失，六项许可正式撤销，Infra 零残留。 |

<a id="v44-develop-查询几何契约夹具修正2026-11-08"></a>
<a id="v44-sql角色服务空参数数组修正2026-11-08"></a>
<a id="v44-精确3afbae复验总超时2026-11-08预算方案待确认"></a>
<a id="v44-预算与安全阶段诊断2026-11-08"></a>
<a id="v44-业务报告与清理令牌到期2026-11-08"></a>
<a id="v44-重启构建身份与-swagger-工具版本2026-11-08"></a>

**业务与收尾。** 原 20 分钟总预算被实际触达，后续固定为 40 分钟。隔离套件复用 4 小时夹具会话，覆盖业务及正式撤销，产品会话和角色资格的 90 秒到期仍按各自规则核验。

| 固定源码及实际 Job | 首败与清理 |
| --- | --- |
| `82f9af3592bb994fc933810984fb736c3776a0c9` · [128454804932](https://github.com/pampa0629/addp/actions/runs/42389802508/job/128454804932) | Develop 的二维 Point 夹具不识别现行 `{wkt, srid}`，对应原始响应未归档。无最终领域或页面报告，六项许可已撤销，Infra 零残留。 |
| `3f34efd2da97ef172a6ded15037bae246c83c1df` · [128525779027](https://github.com/pampa0629/addp/actions/runs/42413276305/job/128525779027) | 无参 SQL 服务合并断言失败。固定代码和回归定位 DTO 空参数序列化为 `null`，原管理响应未归档。六项许可已撤销，Infra 零残留。 |
| `3afbae0ec2921e237ee711b62d3d909a417504fc` · [128584861739](https://github.com/pampa0629/addp/actions/runs/42432701298/job/128584861739) | `online_scenario_timeout`，无领域报告及浏览器截图。六项许可已撤销，Infra 零残留。 |
| `8a7d36a28283401962f685da102e49734a04e2b4` · [128603163154](https://github.com/pampa0629/addp/actions/runs/42438722028/job/128603163154) | 业务和页面报告通过，清理遇到会话到期 401。缺正式撤销回执，Infra 零残留，整体失败。 |
| `839a4d6c0178fbb02d7c2620ff06ec815b337a0c` · [128628787628](https://github.com/pampa0629/addp/actions/runs/42447289580/job/128628787628) | 旧 Swagger 生成器改变 Manager 构建输入，重启身份断言失败。六项许可已撤销，Infra 零残留，领域和页面未完成。 |

[整理前的完整记录](https://github.com/pampa0629/addp/blob/f99ee03a999e6818fd9378beb7f5a6fd867adbb9/security/CLAUDE.md#L701)。

### Service 具体保护原因的解释接入（2026-11-08）

DP-06已完成代码：Service复用Common类型分类，在原HTTP错误形状内给出七原因与中英文建议，预览区域持续展示，不改变保护或授权规则。Service Go、12项真实PG、120项前端和构建、10项相关受控浏览器及Swagger65方法覆盖通过；完整模块在共享Manager验收登记失败，未算通过。新版个人HTTP／Hosted未运行，不继承前一节bd68的T4；原隔离自然到期浏览器断言同次改为精确原因／建议和持续区域，报告v44/v9与清理边界保持。详细首次失败及验证见[Service记录](../service/docs/testing/查询与要素协议.md#service-保护拒绝细分与持续提示2026-11-08)，证据 `/tmp/p145-service-reasons/verification.json`。

### Service 提示修复的 v44/v9 Hosted 闭环（2026-11-08）

固定源码 `5a00785ae3640c7543b4822d97d4904c15e562a8` 的 [Run 42472539366／Job 128706485615](https://github.com/pampa0629/addp/actions/runs/42472539366/job/128706485615) 实际通过 v44／v9 的 27 阶段、21 图目视和完整清理。Service 英文到期页持续显示原因与处理建议，清空旧结果，恢复后显示五行安全结果。留存报告、六份 CSV 和六项精确撤销回执通过独立核对，Infra 零残留。其他原因页面仍按各自证据认定。后续[中英文到期和恢复](#v44v10-中英文保护到期和恢复-hosted-验收2026-11-10)已实际复验。

### Service #7 本地输出与停用收尾（2026-11-08）

已完成用户批准的私有#7限定状态周期：#33／Tenant1，v11停用→v12启用→v13停用；四次中英文JSON／CSV实际200、各五行，双phone别名运行时保护检查和no-store正确，两CSV摘要相同且与历史默认基线一致。配置摘要保持，最终GET与恢复响应一致，会话注销；没有策略、角色或授权写入。保留报告独立审计及12反例通过，原脚本失败保留，非产品失败；不把只有报告／摘要的证据写成原始响应／CSV逐格独立复核，也不扩大为新拒绝／页面／T4。完整原件、版本及限制见[Service本地收尾](../service/docs/testing/查询与要素协议.md#私有-sql-服务本地启停收尾2026-11-08)，独立审计 `/tmp/p145-service-reasons/local-readiness/local-cycle-independent-audit.json`。本轮只记录本地结果，已有e32限定Hosted结论保持独立。

### Service 五类已有 HTTP 拒绝的文案断言（2026-11-08）

沿已获同意的提示验收范围，收紧既有未就绪／结构不匹配／操作不支持／计算输出不支持／自然到期五个Service HTTP场景：同一验证器必须接收由夹具确定的预期原因，继续严格403、两成员及data_protection_required，并核对完整中文原因及恢复建议。不从响应正文推断原因，不导入产品翻译生成期望，不接受通用保护、源权限、其他原因或附带私有细节的消息替代。正式无Accept-Language请求默认中文；已有英文到期页面仍为另一份证据，未新增其他语言请求或UI场景。

此轮只增强原场景断言，没有业务实现／API／部署变更。AST对照基线f2c93af2d确认209处HTTP request表达式完全相同，仅验证器及三个场景调用处改变；v44/v9结构、20runtime／8protocol_only、资源、身份、40分钟预算及清理保持。已确认根Make现有自动发现和Platform CI路径覆盖，未新增入口／workflow。新增五类消息匹配、错原因／缺建议／权限提示／私有后缀等反例；已有计算与四出口组合测试同时执行，正例夹具改成各自实际协议文案。

定向12项通过；标准 `make -o test-protection-deployment-runner -o test-graph-online-runner -o test-online-dispatcher-unit -o test-online-registration-unit test-security-online-runner` 所属272项Python及System Fixture Go退出0；四个聚合依赖目标本轮未运行，不称完整聚合通过。登记CLI、Python语法及本会话diff检查通过。首轮新增定向测试因旧验证器不接收原因参数而失败，红／绿日志保留。证据 `/tmp/p145-service-reason-assertions-{red,green,owner-gate,registration-cli}.log`，请求／版本不变证明 `...-scope.json`，固定输入 `...-verification.json`。未新跑数据库、本地HTTP或Hosted；旧5a成功不追认为这些新增内容断言通过，新实际运行仍由专门CI会话安排回传，不自行调度、轮询或设置定时。

### Service 五类具体原因与建议的 Hosted 闭环（2026-11-08）

固定提交 `a06d7433e46bd1265134251ee2f3af9ce77c0ae8` 的 Job `128732582625` 覆盖五类默认中文 HTTP 拒绝文案、英文到期页面及零残留清理。中文真实页面在该轮未逐项验收，原始子场景 HTTP 正文未归档。完整记录见[Service 查询测试专题](../service/docs/testing/查询与要素协议.md#service-五类具体原因与建议的-hosted-闭环2026-11-08)。

### v44/v10 中英文保护到期和恢复 Hosted 验收（2026-11-10）

<a id="service-中文到期页面的隔离验收接入2026-11-08尚待-hosted"></a>

**验收结果。** 固定源码 `421da33a7455f9b5a36df3e852cd37d5be4a2f8f` 的 [Run 42805905216／Job 129794843854](https://github.com/pampa0629/addp/actions/runs/42805905216/job/129794843854) 实际通过 27 阶段，23 张截图通过结构检查，四张 Service 拒绝和恢复图已目视。同一私有服务在规则自然到期后，`en` 与 `zh-cn` 两次均实际返回 HTTP 403／`data_protection_required`。页面持续显示对应语言的原因及续期建议并清空旧结果。恢复长期有效后返回五行，只含 `id`、`customer_code`。六项许可正式撤销，领域 23 建／23 删，两组浏览器各 2 建／2 删，Infra 零残留。[原始归档 13763838201](https://github.com/pampa0629/addp/actions/runs/42805905216/artifacts/13763838201)

**覆盖边界。** 真实浏览器断言核对了语言头、状态、响应形状与恢复数据，归档保留报告和截图，缺对应原始网络响应。真实页面只收口了保护规则到期与恢复，其他原因页面和切换语言时清除旧短时通知仍未验。角色资格自然到期由既有场景独立认定。[Service 查询测试专题](../service/docs/testing/查询与要素协议.md#service-中文到期页面的隔离验收接入2026-11-08尚待-hosted)

<a id="v10-首轮准备失败与检索续查消费者修正2026-11-09"></a>
<a id="检索续查修复的自然-ci-证据2026-11-09"></a>
<a id="v10-第二轮准备失败与源许可观察消费者修正2026-11-09"></a>
<a id="v10-第三轮浏览器语言头失败2026-11-09"></a>
<a id="v10-语言头修复后的-hosted-构建中断2026-11-10"></a>

| 固定源码及实际 Job | 首败与清理 |
| --- | --- |
| `6efcd87d366b2e41b299ac4af14a94a84caae25d` · Job 128787376318 | Manager 续查探针页大小为 100，准备时返回 400。领域和页面未执行，Infra 零残留。[检索续查修复](../service/docs/testing/查询与要素协议.md#v10-首轮准备失败与检索续查消费者修正2026-11-09) |
| `f0792f6f1b8255b5b9457428495b9c7c525f970f` · [128808727268](https://github.com/pampa0629/addp/actions/runs/42506020215/job/128808727268) | 精确来源观察请求缺 nonce 和预期标记，返回 400，业务、页面均未执行。三项 metadata_read 已撤销，最终观察仍失败，六项闭环未证明，Infra 零残留。 |
| `4e16bd556bc5c5b67076a56732d653cac013458b` · [128943253399](https://github.com/pampa0629/addp/actions/runs/42550010623/job/128943253399) | 页面预期 `en`，实际收到 `en-US`。到期拒绝与恢复未执行，六项许可已撤销，Infra 零残留。 |
| `d02a26ef51138ce698617d1cd254b9b96aff2f2d` · [129667620475](https://github.com/pampa0629/addp/actions/runs/42767590544/job/129667620475) | 前四阶段通过，第五阶段重启触发 Manager 构建身份变化。共享字段 `temporal` 缺 Swagger 同步，属于另一轮生成输入问题。无最终报告、0/23 图，六项许可已撤销，Infra 零残留。 |

续查修订 `0792d77993dd8015f4a3611416955768fd69846b` 的 [Platform 128796860668](https://github.com/pampa0629/addp/actions/runs/42502098196/job/128796860668) 与 [Console 128796860653](https://github.com/pampa0629/addp/actions/runs/42502098196/job/128796860653)，来源观察修订 `7c92e7c84b28c581edd5dea3230e2da20adf5ea9` 的 [Platform 128815197630](https://github.com/pampa0629/addp/actions/runs/42508155900/job/128815197630) 各自通过所列 CI，Console 页面使用受控环境。真实业务结论以该轮 Hosted 报告为准。

[整理前的完整记录](https://github.com/pampa0629/addp/blob/f99ee03a999e6818fd9378beb7f5a6fd867adbb9/security/CLAUDE.md#L878)。

### PostgreSQL 单表分组输出重写（2026-11-11）

按已确认的下一步扩展原生 PostgreSQL 同一 PreparedQuery 的输出重写。支持单表、直接列分组或分组序号、直接列／显式别名输出及显式别名 count(*)。改写前将分组序号固定为原字段，继续原有排序别名和序号解析，避免输出替换后重新分组或排序。两个源值脱敏后相同仍保留独立分组及各自计数。普通掩码、固定值替换和移除复用现有变换器和交付处理，ReadSet、函数可信性和输出血缘继续走既有路径。与源字段不同名的输出别名分组、HAVING、分组集合、JOIN、DISTINCT、窗口及其他聚合仍在原拒绝边界内。

先补实现规范和红色回归，再修改现有重写器。新增 T1 覆盖三种分组引用与拒绝路径。真实 PostgreSQL T2 覆盖三种分组引用×三算法×三种排序×Execute／分批读取，共54次读取对照原查询再按共享规则保护的结果，核对计数、行序、分页、重复组、Unicode、短值及NULL。新用例名 TestIntegrationResolvePostgresQueryOutputRewriteGrouped 命中既有 Common PostgreSQL 门禁，无新标准入口或 workflow。

验证已实际通过：make test-module MODULE=common GO_ONLY=1；make test-common-postgres 完整标准门禁。通过前两个完整尝试在 KML 连接校验失败，首轮缺 host，第二轮还缺明确 database／sslmode。最终显式使用经 infra/status.sh 核实的 localhost:25432、addp_test 及 disable 后全门禁退出0，无跳过。未修改 KML 或基础设施。最初测试准备将 Args 写成 Parameters 的编译错误及修正记录保留。无本地服务重启、实际出口HTTP／浏览器或新增 Hosted 证据。

证据 /tmp/p145-grouping-{red,unit,common,module,pg-full,pg-final,pg-complete}.log，输入摘要 /tmp/p145-grouping-tested-inputs.json。其中pg-complete为最终全门禁，pg-full和pg-final为原失败。下一步加载Common新构建后，核对Develop实际分组查询、默认历史预览和导出，继而按具体出口补覆盖。静态加密与透明解密仍待前置讨论。

### PostgreSQL 分组查询本地 Develop／导出验收（2026-11-11）

用户重启后，通过 Gateway 的正式登录核对普通 outdoor开发者 #33／Tenant #1，使用已有 PostgreSQL engine #2、Meta #44720、outdoor.p145_native_default_20261024。三种 GROUP BY 写法 p.phone、phone、1 分别产生成功执行 6df22569-48c4-43e3-889d-1489cba5dc81、f0c3e098-ddcf-446d-90f6-c4a62cd0d494、f21eb225-70a1-47ad-aa1d-e3d2e59e3bb1。每条返回五组 contact／n，按源字段排序后取五组。实时结果与历史相同，重复读取历史不变，三种写法的行序及计数相同。

Develop 导出会话 #19 经独立 Transfer 执行 94347359-7a39-4359-9b66-cf26b6bed48a 成功生成 CSV。保留的实际文件逐字节 SHA256 为 96a4d551238f13f41aa50fb0f96c6fc7a8c4ebd08cff2de38dc57cf707f64471。独立解析五行、两列，手机号保留前3／后4，中间按长度遮盖，长度依次10、11、11、11、11，计数均1；根据实际CSV重算的行摘要与三条执行报告全部一致。脚本实际断言创建／下载 no-store 和注销成功，保留报告的 logged_out=true、rejections=[]。原始HTTP响应未留存，不能把报告内的实时／历史及响应头断言称为独立原始报文审计。

前两次本地失败保留。p5dviirx 报告停在额外 Manager 预览403，无查询或导出；此多余前置已移除，具体403原因未归档。mf68n8t8 的第一条执行成功，但临时脚本错误要求固定四个星号；开发者只读诊断核对精确Meta身份及已安装的保留前3／后4规则，第一行长度10、含三个星号，五行均符合实际规则。脚本修正为长度保留，变量长度红绿回归、原值／过量保留拒绝、正式Develop拒绝保留及注销检查通过。开发者存储诊断不代替当前用户HTTP验收。

原件 /var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/p145-grouping-local-cjagmlmn/，独立脚本及结果 /tmp/p145-grouping-independent/。本地表是此前脱敏Transfer的产物，因此本轮证明普通用户业务出口的分组可用及历史／导出一致性；原值分组、掩码碰撞及三算法的变换等价性仍取此前真实PostgreSQL T2。无个人浏览器或新增Hosted通过，无策略、角色或授权写入。下一步按原SQL范围核对Service查询服务的发布与实际输出，其他聚合和HAVING仍在已记录边界内。

### PostgreSQL 分组 SQL 本地 Service 发布与分页验收（2026-11-11）

普通 outdoor开发者 #33／Tenant #1 通过正式Gateway登录完成限定HTTP验收。沿用PG engine #2、outdoor.p145_native_default_20261024，以同一直接列分组及count(*)查询取得Develop执行 c86457e6-6bb0-4a79-be81-a2117612e190 的11行保护预览，作为两页各5行及继续分页的对照。正式Service输出契约检测后，新建私有临时SQL服务 #17、version1，稳定键为contact，无固定LIMIT、无角色专用选择。

实际两页JSON与CSV均成功。保留结果中的contact为变长前3／后4掩码，n均1；两页合并10行与Develop对照的前10行逐项相等，11行中的最后一行为仍有后页提供对照。独立解析两份实际CSV的字段顺序、行序、计数、掩码和原始字节摘要，分别为96a4d551238f13f41aa50fb0f96c6fc7a8c4ebd08cff2de38dc57cf707f64471和96bf04ffc3f984d5f4e4edccb2635c4cad52c59d24b3a9993247e6c0cec1ffcc。原脚本实际断言JSON旧游标续读、JSON／CSV的no-store及分页头、消费版本一致，最终以version1删除精确服务并GET确认404，随后注销。保留报告service_deleted／logged_out均true，rejections为空。未归档原始HTTP响应，协议头、游标和清理结果按运行断言及保留报告记账。

原件 /var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/p145-service-grouping-local-n2bdx9x6/；独立审计 /tmp/p145-service-grouping-independent/independent-audit.json。本轮只补临时脚本和证据，无产品代码变更、策略／角色／授权变更或个人服务启停。本地源是此前已脱敏Transfer产物，原值变换等价性仍取前述真实PG T2。本轮不包含Service分组的角色选择生命周期、个人浏览器或新增Hosted。下一步优先补Transfer独立分组任务的实际目标写入，再按具体出口补其他已确认形态。

### Transfer 分组任务本地准备期 Meta 可见性阻塞（2026-11-11）

用户执行 /tmp/p145-transfer-grouping-local.py，普通#33／Tenant1登录成功，在GET /api/v1/meta/items/44720返回404后停止并注销。真实失败原件为 /var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/p145-transfer-grouping-local-wfgt7k4w/acceptance-report.json，独立核对 /tmp/p145-transfer-grouping-independent/initial-failure-audit.json。无task_id、参考查询或Transfer执行，尚未创建任务或写入目标；task_deleted=false不表示遗留任务。受控脚本的00000000开头UUID只属于模拟检查，不计实际验收。

开发者只读核对Meta #44720仍属Tenant1／PG engine2，身份及fingerprint与此前相同，未软删除。现行Meta单项接口会将不可见与不存在均返回404，故当前账号的元数据可见性需先核实，不能把本轮当成Transfer分组或脱敏执行失败。未变更元数据／内容读取授权，未借其他账号继续运行；具体权限调整仍需用户确认。既有Develop／Service限定本地HTTP与Provider T2证据保留，独立Transfer落盘未执行。用户已授权后续同类脚本由本会话直接执行，正式登录凭据仍由用户在认证步骤输入。

同轮继续只读核对，已有验收任务 #16（p145原生表默认恢复验收20261024）仍保存PG outdoor的正式target.parent_locator。源Meta单项读取只是临时脚本为取得目标schema位置加入的额外前置，并非Transfer源读取或目标写入的正式授权替代。脚本已改为唯一读取路径GET /api/v1/transfer/task-definitions/16，在当前用户权限下核对精确任务身份及其PG目标parent_locator，再走同一正式创建／执行入口。保留原Meta404，不放行任何Transfer权限拒绝，不变更授权。成功／执行失败／启动拒绝三组受控脚本检查通过；修正版尚未实际运行。电脑控制工具明确拒绝操作Terminal，无法由该通道完成交互式认证；用户已授予的脚本执行授权继续有效。

### PostgreSQL 分组 Transfer 独立任务本地验收（2026-11-11）

修正额外定位前置后，用户实际运行同一临时脚本，正式登录核对 outdoor开发者 #33／Tenant1，通过既有任务 #16 的只读配置取得 PG outdoor 目标位置，未修改该任务。参考 Develop 执行 a88cb013-2823-47c0-8dd1-b49f2752cd93 返回五组。新建独立、无调度、未启用的任务 #56，再手动执行 84ac72e0-2444-40d2-b8ce-26e6f547ed7d；实际状态 success，trigger_by=33，records_read=records_written=5，批量大小2。目标回读执行 bbf4b1d4-5754-4231-8074-e889e33697c1 成功，保存的 contact／n 五行与参考逐行、逐字节一致，计数均1，掩码长度保留。任务 #56 已删除并核对 GET404，登录已注销；结果表 outdoor.p145_group_transfer_6b7afc3d0152 按约定保留供查看，未将保留结果表称作零残留。

原件 /var/folders/r1/l7b49c757dz02zftf_l9vl9r0000gn/T/p145-transfer-grouping-local-qch78ix7/，报告 SHA256 929e76b4441e8f4abc6e0bb6b382b2f0e416b7a89760c452ffbd138409eb8bf6；reference.json 与 target.json 的 SHA256 同为 ad5e87676c8cf0929da219818fb87bd74f7afb94d045629fd1aa05991874027b。独立脚本 /tmp/p145-transfer-grouping-independent/audit.py 实际退出0，结果 independent-audit.json；未导入验收生成脚本，核对三条非模拟UUID、精确任务和执行关系、五行两列、顺序、掩码及清理报告。身份、执行状态、读取／写入计数、删除／404和注销是正式HTTP脚本保存的断言，未归档原始HTTP响应，不称为独立原始报文复验。

本轮仅本地普通用户HTTP验收及文档补证，无新产品代码；重算此前T1／T2的三个Provider输入摘要仍一致，复用有效门禁，不重复全量执行。初次Meta404失败与其审计原件保留。源表仍为历史脱敏传输产物，本轮证明独立任务能够消费限定分组查询并保持目标结果一致；原值变换等价性另由真实PostgreSQL T2证明，角色方式失效、本地浏览器和新增分组Hosted均未验。下一步建议将Develop／Service／Transfer分组场景接入现有隔离套件，使用原值样本补实际默认保护和角色方式失效拒绝，再由专门CI会话安排运行。

### v46/v10 分组与角色保护 Hosted 闭环（2026-11-13）

**验收结果。** 固定业务源码 `45fe76a6f830d0968d73476e66812436659a4fd2`、控制面 `6d30e0f4fdc54a5035d7b3f88e397e6c3244eb68` 的 [Run 43217668163／Job 131180952821](https://github.com/pampa0629/addp/actions/runs/43217668163/job/131180952821) 实际通过 28 阶段和 23 图目视。PostgreSQL 单表直接字段分组与 `count(*)` 验证默认、角色选择、方式撤销拒绝及显式移除后恢复。Develop 分开验证同一 UUID 的当次结果与默认历史，Service 验证 JSON／CSV 两页及旧游标拒绝，Transfer 验证独立执行、物理回读和拒绝零写入。六项许可正式撤销，领域 26 建／26 删，两组浏览器各 2 建／2 删，Infra 零残留。[独立复核与留存证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#v46v10-分组角色脱敏-hosted-验收2026-11-13-独立复核)

**覆盖边界。** 方式撤销与角色资格撤销／自然到期分别核验。20 个 runtime 格、8 个 protocol_only 格的范围保持，尚未生效仅有真实 PostgreSQL T2。部分分组原始响应未归档。多表分组、复杂聚合、角色调度／编排身份、AK/SK、静态加密及后续前端竞态修复未由本轮覆盖。[三出口断言与清理合同](../scripts/docs/Online数据保护.md#v45postgresql-分组三出口与方式撤销) · [当次交付与默认历史](../scripts/docs/Online数据保护.md#v46分组当次结果与默认历史分开核对)

<a id="v45-postgresql-分组三出口隔离接入2026-11-11hosted-未运行"></a>
<a id="v45-分组首失败与-v46-当次历史消费者修正2026-11-11"></a>
<a id="v46-唯一复验的外部-dns-准备失败2026-11-12-审计"></a>
<a id="v46-分组所选-transfer-的签发前提缺口2026-11-12hosted-首失败保留"></a>
<a id="v46-分组-transfer-主动恢复的-put-前提缺口2026-11-12"></a>

| 固定业务源码及实际 Job | 首败与清理 |
| --- | --- |
| `7a91abaee4cfaefbf35b32776452240c7e3b47fe` · [130472411237](https://github.com/pampa0629/addp/actions/runs/43005665942/job/130472411237) | v45 在第七阶段失败，消费者将 Develop 的默认历史与角色当次输出比较。分组最终报告与页面缺失，六项许可已撤销，Infra 零残留。 |
| `7faff9ab8b24e0678c465325491ca69b84c39f82` · [130619097907](https://github.com/pampa0629/addp/actions/runs/43048558678/job/130619097907) | Playwright 系统依赖下载遇 DNS 失败。业务和浏览器未进入，许可未签发，隔离环境清理通过。 |
| `7faff9ab8b24e0678c465325491ca69b84c39f82` · [130924454141](https://github.com/pampa0629/addp/actions/runs/43140011471/job/130924454141) | Transfer 启动时执行授权签发 403，未创建 Worker 执行。夹具缺 `develop.data_write.execute`。无最终报告、0/23 图，六项许可已撤销，Infra 零残留。 |
| `2664dd82881a553f6634afbbb4a2c3389803d9f0` · [131054875358](https://github.com/pampa0629/addp/actions/runs/43180455765/job/131054875358) | 显式移除旧选择时任务 PUT 返回 403／`permission_denied`。夹具缺 `transfer.task.update`。无最终报告、0/23 图，六项许可已撤销，Infra 零残留。 |

两项既有 Permission 仅补给该套件的一次性消费账号。来源许可、原值授权和产品权限按各自契约判定。

<a id="v45-完整标准门禁补证2026-11-11t4-待回传"></a>
<a id="v46-精确修复候选-linux-标准门禁2026-11-11非-hosted"></a>
<a id="2664-精确候选的-linux-platform-自然门禁2026-11-12非-hosted"></a>

**门禁边界。** v45 专项目标通过，早期本地聚合出现 Metric／Graph 包装失败，同候选后来有完整入口退出 0 的证据。v46 固定候选 `7faff9ab8b24e0678c465325491ca69b84c39f82` 的 [Platform 130516352816](https://github.com/pampa0629/addp/actions/runs/43018760077/job/130516352816) 和 `2664dd82881a553f6634afbbb4a2c3389803d9f0` 的 [Platform 130977934296](https://github.com/pampa0629/addp/actions/runs/43156617594/job/130977934296) 实际通过。补任务更新权限后，本地 Graph 包装仍有三个 60 秒子进程超时，完整入口失败。新版的真实 T4 只按上面的最终 Hosted 证据认定。

[整理前的完整记录](https://github.com/pampa0629/addp/blob/f99ee03a999e6818fd9378beb7f5a6fd867adbb9/security/CLAUDE.md#L972)。

### Owner 投影存储首次并发初始化的目录读取修正（2026-11-12，T2）

自然Run43143870160／Job130939582473、源码5df18e46507ada2f00cd5acc43cabbc126377b45的TestProjectionStoreMigrationIsSerializedAgainstPostgres实际失败：CREATE SCHEMA IF NOT EXISTS projection_store_concurrent_it触发SQLSTATE23505／pg_namespace_nspname_index。原日志4962–4968和数据库日志5237–5238保留于/tmp/addp-ci-1200-1112.J8xJkB/logs/130939582473.log，不用后继偶发通过覆盖。原样本地projection-store标准组、32轮及256轮临时诊断曾通过，均只作排障观察，不算修复；临时探针仅在/tmp并经原标准入口的Go overlay运行，未改数据库清单或产品进程。

加强原并发用例为八个独立连接全部就绪、先各自观察schema缺失后同时迁移，并在失败时保留PID、锁及DDL时序。该条件在本地真实PostgreSQL复现同一23505；时序证实锁逐个放行，却不能直接依赖后续CREATE的缺失缓存检查。按既有事务／schema迁移锁契约先完善规范，再将schema存在性判断改为锁后独立SQL读取pg_catalog.pg_namespace，确认不存在才创建。迁移锁、版本和结构核验不变，无重试、吞错、Worker DDL或第二条迁移路径。

最终同一测试输入的红绿校验走make test-common-postgres COMMON_POSTGRES_TEST_ARGS='--test projection-store'：用Go overlay仅还原5df原schema.go时退出2并重现23505；恢复修复源码退出0，投影存储完整子组无跳过，涵盖同构／历史／漂移、未来生效历史、四出口原因矩阵、首次并发初始化和版本提交锁竞争。不是完整Common PostgreSQL全门禁。make test-module MODULE=common GO_ONLY=1退出0；make test-projection-store-ownership退出0（9项／5504个Go或SQL文件），git diff --check通过。独立输入、红绿日志、原CI元信息和摘要/tmp/p145-projectionstore-fix-independent/{red-green.json,verification.json,red.log,green.log,first-ci-job.json}；Go及唯一归属日志/tmp/p145-projectionstore-{common-go,ownership}.log。既有自动发现和Common PostgreSQL CI已覆盖，无新增workflow。此修正尚无后继Linux自然CI或运行验收，不启动个人服务、不申请重启，也不消耗／重派Hosted授权。


### Owner 投影存储修正的 Linux 完整 PostgreSQL 自然门禁（2026-11-12）

精确修复7d51762d5054ea14d774cc68040d71adacf8f947的push [Run43155459026／Common PostgreSQL Job130974521958](https://github.com/pampa0629/addp/actions/runs/43155459026/job/130974521958)，attempt1已实际success。独立读取原始Job API，确认head SHA、completed及Run Common PostgreSQL gate步骤success；原日志339行完整checkout SHA一致，491–492行实际执行make test-common-postgres，无子组选项，不以summary或容器清理替代。原并发初始化测试4988行实际PASS；投影存储八项顶层全部PASS，原始日志无SKIP及error annotation。这是修复后新的完整Common PostgreSQL T2门禁成功，原5df并发首失败及本地红绿证据保留。

原件/tmp/addp-ci-security-2664-l_naer3d/common-130974521958.log，SHA256为3fe428ef81b21dbf240ebe08b927f6d6bdaf5c3fa879fc1e433fd65b27bf55df。独立脚本、Job元信息和结果/tmp/p145-common-natural-independent/{audit.py,job.json,audit.json}，脚本实际退出0。已核7d为2664dd82881a553f6634afbbb4a2c3389803d9f0祖先，common全目录、根Makefile、release工作流、Common PG入口、其容量脚本、Graph backend、go.work/sum及Infra端口核验输入均无差异，故该T2结果可适用于2664候选；只比较既有入口依赖，不调查栅格或其他suite。

本轮只补证据，未重复运行测试或Hosted、未启停个人服务。精确2664的Platform自然门禁仍由专门CI会话跟进，本轮未认定其通过；v46/v10新的完整T4尚未获单次授权或派发，原403与0/23图保持。下一步由专门CI会话核对该Platform结果后统一提出具体单次方案，不重复确认或自动重试。


### Service 保护拒绝后旧响应回流修正（2026-11-12，独立T1/T3）

不等待本次v46 Hosted，按已有“保护拒绝清旧行且持续显示原因”契约，核查Service页面在途响应归属。标准隔离浏览器的中英文正式Vue页均重现：第二请求403到期后，第一个旧200重新显示phone行及游标；原始失败保留。仅前端loadPreviewData增加当前请求／定义归属，旧成功、失败、finally不覆盖新展示，定义改变或停用后不接受旧结果。三个单元红例转绿；make test-service-frontend的123项／构建及Console标准隔离浏览器4项实际通过，make test-frontend-ci-registration通过；未改后端／权限／接口／Hosted套件，不操作个人服务或Chrome，也不把受控403当真实自然到期T4。细节与证据统一见Service [迟到响应专题](../service/docs/testing/查询与要素协议.md#保护拒绝后的旧预览响应回流2026-11-12)。


### Manager 预览归属核对与 Develop 原值截止修正（2026-11-12）

按用户继续核查迟到响应的授权，不查询或调度CI。Manager主预览已有请求序号与资源／子项身份判断，clearPreview失效两条请求；新增5项反例核对同资源新保护403后的旧成功／失败、父／子清空后的在途结果，82文件／414项单元通过，Manager产品代码未改。不能从该局部核对推断所有Manager出口或浏览器T4通过。

Develop现有原值复核在等待HTTP期间缺独立截止清空，且会展示携带已过截止的迟到原值；两个旧实现单元真实报红。仅修正queryPlaintextRefresh为截止与复核分别计时、拒绝已过期原值响应，并保持取消／退出失效与脱敏恢复。定向7项通过；正式Vue工作台的中英文2项隔离浏览器实际等待8秒截止，挂起5秒复核并核对清空、迟到200不回流及再次执行保护结果，通过。不修改系统或浏览器时钟，不改后端、权限、授权期限／历史规则；这是T1/T3，不是实际授权自然到期HTTP或T4。完整make test-develop-frontend实际退出0：test:workflow链、290项浏览器及构建通过，含两项截止测试；前端CI登记27项通过。用最终同一7项断言隔离回放旧控制器时3过4失败，未修改工作区源码，证据/tmp/p145-plaintext-deadline-independent/。细节与日志见Develop CLAUDE.md末节及/tmp/p145-develop-plaintext-deadline-{red,green,browser,frontend}.log。


### Develop 查询切换的旧提交归属修正（2026-11-12）

继续独立前端核查，正式 Vue 同组件切换任务后旧执行仍锁住新任务；最终同一 4 项单元断言隔离回放旧代码均失败，覆盖迟到预检／取结果失败、旧创建回执和失效确认。现沿既有执行序号失效旧观测，在路由切换时清除旧结果／原值复核与加载状态，防止旧响应覆盖新保护拒绝或过时确认继续提交。后台已提交执行、引擎切换限制和所有授权不变。定向 4 项及中英文三边界共 6 项受控浏览器通过，首次夹具混入自动诊断的失败保留。完整 make test-develop-frontend 实际退出 0，test:workflow 链、296 项浏览器及构建通过；前端登记 27 项／21 个模块通过。仅 T1／T3，不新增真实身份或 Hosted T4，不自行操作 CI 或个人服务。规范见实现规范 §10.2，产品与日志见 [Develop 记录](../develop/CLAUDE.md#查询任务切换后的旧提交失效2026-11-12)。


### Develop 导出切换与共享取消交付修正（2026-11-12）

查询清空／替换、任务切换及退出结束旧导出的前端观测，复用既有请求作用域和单一共享导出工具。迟到创建／轮询／文件不继续交付，旧失败或 finally 不污染新任务；共享工具在状态返回与 Blob 保存前复核取消。后台导出会话、授权和保护规则保持。最终 16 项同断言旧代码 10 过 6 失败，修后全部通过；中英文 8 项受控浏览器及共享 378 项通过。完整 Develop 标准前端门禁的工作流链、304 项浏览器及构建通过；Manager 消费方 414 项单元、358 项浏览器及构建通过；前端登记 27 项通过。无新增 Hosted 或真实账户 HTTP 验收，不自行跟进 CI、不启停个人服务。细节见[Develop 记录](../develop/CLAUDE.md#全量导出在途观测失效2026-11-12)。


### 8a068 自然前端门禁闭环（2026-11-13）

固定 `8a0682d92e9b46f53dbd6934965b4553d3cae226` 的 Run `43243077330` 成功，离线核对共享前端、Develop、Manager 与 Monitor 四个相关 Job 均通过。[自然CI原件与审计](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#8a068-导出观测修复的自然前端-ci2026-11-13) · [各 Job 与本地计数](../docs/sessions/数据脱敏与保护后续工作专题.md#三验收事项与本次闭环)。该轮覆盖 T0／T1／受控 T3 及构建，`45fe` 的限定 T4 独立认定。Manager 页面归属见[后续修正](#manager-全量导出归属修正2026-11-13)。


### Manager 全量导出归属修正（2026-11-13）

跨模块保护核查发现，旧导出创建回执、异常／finally 和 `resource-actions` 响应可能影响已切换的资源。取消规则、旧源码对照及原轮 T1／受控 T3 结果统一见[Manager 专题](../manager/docs/数据预览与资源树实现规范.md#全量导出观测与动作能力归属2026-11-13)。固定版本 Job、实际覆盖和原件见[后续 CI 证据](#f924-manager-自然前端证据2026-11-13)。


### f924 Manager 自然前端证据（2026-11-13）

f92420a13fd3a0e1c9e3cf8125f133c8b4362bec的Run43342310763／Manager Job131613299658已独立离线核对实际success、83文件426单元、366浏览器（含8新增中英文场景）及构建通过，12专项亦实际执行。只是本Job的自然T0/T1/T3，不推断整体Run、不处理无关CI、不新增Hosted。正式原件及命令见[证据正文](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#f924-manager-导出归属修复的自然-ci2026-11-13)，后续仍优先核查Transfer来源切换的角色候选归属。

### Transfer 角色候选来源归属核查（2026-11-13）

现有Step4序号保护通过9项实际Vue watcher行为检查和中英文6项受控页面回归：来源切换后旧候选／失败／finally不覆盖当前状态，折叠和不支持的来源／模式使旧观测失效；保留旧选择并提示不可用，主动移除后清除。没有重现需修的产品缺陷，仅补既有标准门禁中的测试；四组临时变异均被拒绝，不把变异失败称为原产品失败。完整`make test-transfer-frontend`139项单元／75项浏览器／构建通过，前端登记27项及21前端通过。未改角色、权限、API、执行契约或共享库，不启停个人服务、不新增Hosted、不自行跟进CI。证据及范围见[Transfer角色选择记录](../transfer/CLAUDE.md#角色专用导出选择)；下一步优先核查Service编辑页在表／SQL／参数变化时的角色候选归属。

精确0d1da0e2f的自然Transfer Job131661059381（Run43356929114）现已离线独立核对success：139单元含9专项、75浏览器含6新增中英文场景及构建实际通过。整个Run并非成功，仅确认所属Job；未检查或处理其他CI。原日志哈希、步骤和审计命令见[自然CI证据](../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#0d1-transfer-候选来源归属的自然-ci2026-11-13)。无新产品、运行验收或Hosted范围，后续优先级保持。
