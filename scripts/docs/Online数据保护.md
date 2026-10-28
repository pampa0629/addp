# ADDP 脚本：Online数据保护

[返回脚本导航](../README.md)

本页导航：[Transfer 保护与原值访问](#transfer-保护与原值访问) · [MySQL 与空间字段保护](#mysql-与空间字段保护) · [Service 角色方式撤销后的旧游标](#service-角色方式撤销后的旧游标) · [用户角色资格撤销](#用户角色资格撤销) · [角色资格自然到期](#角色资格自然到期) · [Develop 角色资格自然到期](#develop-角色资格自然到期) · [Transfer 资格自然到期与旧文件](#transfer-资格自然到期与旧文件) · [四出口逐原因矩阵](#四出口逐原因矩阵)

测试分层、身份和清理规则以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)为准；历史结果仅适用于所列源码和运行尝试，不代表当前 HEAD 已重跑。

## Transfer 保护与原值访问

`security-transfer-protection` 要求全量 System、Gateway、Meta、Security、Transfer 和 Manager，并使用 `business/scripts/online-security-transfer-fixture.sh` 管理固定 MongoDB 源与 PostgreSQL 目标。专用环境长期预置分别指向只读 MongoDB 账号和 PostgreSQL Fixture 的 Engine Instance，专用 Tenant 预置平台推荐的手机号敏感类型、识别方式和默认保护规则。suite 经 Gateway 扫描两端资源；首次运行允许为固定 MongoDB DataItem 建立一个永久 Security 纳管，后续必须复用同一纳管并确认 Transfer 已安装 `export → mask` 投影。每轮创建两条带 Run ID 的 bounded Transfer 任务：不输出手机号的可证明 MQL 必须正常写入，输出手机号的可证明 MQL 必须写入遮盖值且明文计数为零。目标表属于固定物理 Fixture；临时 Transfer 任务在 `finally` 按捕获 ID 删除并逐条确认 404，不能撤销投影、管理员绕过或为 MongoDB 增加白名单。

`security-plaintext-access` 复用同一个复合 Fixture，在固定 PostgreSQL `addp_online_security.exemption_source` 上建立永久 Enrollment 和手机号正式 Assessment。suite 使用两名不同的专用 User：申请人在 Manager 预览确认手机号已遮盖并从该出口提交原值访问申请，审批人在 Security 待审批队列批准；随后只允许申请人的 Manager 预览临时返回原值，审批人仍看到遮盖值，到期后不访问 Security 即验证 Manager 依据本地投影恢复遮盖。固定治理聚合、申请和授权修订作为专用 Tenant 审计事实保留。环境文件必须额外提供 `ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN`，且审批用户不得与申请用户相同。

## MySQL 与空间字段保护

`security-mysql-owner-protection` 使用唯一 GitHub Hosted Linux x86_64 disposable profile，由 `scripts/test/online-hosted-security-gate.sh` 复用共享 Hosted 生命周期。每轮从零启动 Infra、PostGIS PostgreSQL 与 MySQL，System owner helper 创建一次性 Tenant、最小权限非管理员 User、独立引擎登记用户和独立治理初始化用户；Business 只管理物理数据及 owner-only Engine descriptor，引擎登记和分类、等级、email/phone 敏感类型及默认保护初始化分别通过 System、Security 正式 API 完成。初始化令牌在业务验收前移除，不复用个人环境、永久身份、GitHub Environment 或仓库 Secret。suite 先验证默认保护同事务创建、非法参数整体回滚和探针清理，再经 Meta 扫描和 Security 正式 API 建立 Enrollment/Assessment。MySQL `customers.email` 在 Manager、Develop、Service、Transfer 四个出口均移除，但保留 5 条非敏感记录；临时任务及服务按捕获 ID 清理。成功、失败退出都停止应用并销毁当次业务容器及 Infra，核对零残留；凭据只保存在 `runner.temp` 的 owner-only Secret 目录并删除。

同一 Hosted suite 的 Console 浏览器阶段使用上述普通 User 正式登录，在 Manager 从已纳管的 MySQL `customers` 点击导出表格式能力声明支持的 CSV，通过 Python 标准 CSV 解析器核对完整五行、敏感 email 字段移除及非敏感字段保留，再以同一身份查询并打开 Monitor 中对应的 Transfer 一次性执行。执行必须成功、来源为 Manager、无任务定义引用，且 `triggered_by` 与已验证 User 一致；不允许浏览器自报身份或用 API 创建代替点击导出。测试身份只额外持有数据探查页面必需的 `manager.content.read`、`manager.derived_artifact.create/read` 与 `monitor.execution.read`；原有 `manager.data_item.read` 同时参与页面入口校验，准备身份仍不进入业务阶段。Playwright 失败、缺失或不匹配的浏览器报告使整个 suite 失败。全新匿名浏览器初始化时唯一一次 `/system/refresh` 的 401 及对应 Chromium 网络告警单独计数并验证；正式登录成功后，任何 401、业务失败响应或 warning/error 均使验收失败，JavaScript 异常全程检查。浏览器报告只接受 v2，包含上述匿名刷新计数、安全执行 UUID、业务计数与断言结果，截图归档，原始下载文件删除。Monitor 加入同一版本健康预检，应用与文件仍由既有 Hosted 生命周期清理；此扩展通过真实 T4 前不计为已验收。

同一 suite 还使用 PostgreSQL `addp_online_security.spatial_algorithm_source` 与 `spatial_algorithm_transfer` 验收空间表的普通属性脱敏。三个字符串字段通过 Meta 当前组件清单形成正式 Assessment，绑定当次初始化的 `phone` 类型及其初始等级；空间列名为 `location_point`。依次将该组合的默认保护切换为前后掩码、固定值替换、普通 SM3，等待四个 Owner 确认最新投影后核对五行输出、Unicode、null、空串异常效果和点坐标。掩码阶段还单独为 Manager 三个字段配置全掩码、固定值替换、SM3，验证字段配置只作用于 Manager preview，其他出口仍执行默认保护。测试成功、失败及中断退出时撤销本轮字段策略、删除临时 Service/Transfer 任务，并恢复运行前的完整 phone 默认规则；审计历史与当次 Enrollment/Assessment 随 disposable Infra 一同销毁。若发现前次遗留的活动字段策略，预检失败，不能替本轮静默接管。专用 User 还需 `security.assessment.create`、`security.policy.read/create/update/delete` 和 `security.protection_baseline.update`，当次 Tenant 与业务引擎仅供本 suite 使用。确定性协议、失败清理与 Fixture 回归由现有 `make test-online-runner` 和平台 T0 覆盖；真实执行仍使用 `make test-online ONLINE_SUITE=security-mysql-owner-protection`，未真实运行不得标记为 T4 通过。

单表通配符的 Provider T2 由既有 Common PostgreSQL `query-read-set` 与 MySQL 数据保护门禁覆盖，验证掩码／替换／移除、SM3 协同、四个 Owner 的共享消费、原值授权及默认历史、零行结构变化拒绝和事务结构锁。MySQL 隐藏列不按完整字段表强行展开。Hosted `security-mysql-owner-protection` 报告 v23 的 `bank_card_metadata.develop_wildcard` 额外要求真实 Develop 执行 `SELECT *` 和 `SELECT b.*`，与同一固定夹具的直接字段脱敏逐项对照；`bank_card_metadata.develop_mixed_wildcard` 要求额外执行直接别名位于通配符前后的两种混合投影，逐值核对敏感字段及副本的相同保护、NULL／缺失语义和非敏感副本保持；`bank_card_metadata.develop_page_wildcard` 另外核对单层包装的外层星号／直接字段两种查询；`service_sql_page` 使用实际 Meta 字段发布包含混合通配符的正式 SQL 服务，通过真实 cursor 连读 2／2／1 三页，严格校验全部敏感副本、行序和无重复遗漏，并删除本轮服务定义。`computed_outputs` 另外要求真实 Develop 的 SUBSTRING／COALESCE／CASE／CAST／派生别名／UNION 六种敏感计算均在准备阶段拒绝，复读同一历史无结果；count(*) 和未受保护字段计算继续成功。额外发布的私有 SQL Service 必须按正式 403 合同拒绝敏感派生输出且没有结果，定义纳入本轮精确清理。只有实际运行通过才记为 T4，不把 Runner 单元测试计作运行验收。

保护原因细分沿用 execution-failure/v3 的闭合 reason：v23 必须精确核对 MySQL 六种计算及其历史为 `protection_computed_output_unsupported`，MongoDB 无法证明的派生输出为 `protection_lineage_unverified`，自然到期的 Develop／Transfer 为 `protection_expired`。浏览器同时核对具体原因与对应处理建议，不接受通用保护原因代替这些已证明原因；非法值导致的结果阶段拒绝尚无进一步类型化事实时仍为 `data_protection_required`。不得解析错误正文猜测子原因、放宽为任意保护错误或将原值授权扩大为计算许可。

后续 v24／浏览器 v9 在同一隔离套件补三类真实拒绝：在既有 PostgreSQL 合成表人工指定敏感字段之前，先等待自动发现真实完成且 Finding 数为零，再等待五个 Owner 的 enrolling 空规则投影回执齐备，验证四出口 `protection_not_ready`；全部请求后再次核对同一发现、投影修订和 enrolling 状态不变，随后才沿原流程人工指定敏感字段并验正常脱敏；规则激活后，直接字段带字符串字面量过滤超出当前保守 SQL 输出重写子集时，验证 `protection_operation_unsupported`；仅在当轮带 Security 归属标签的 MySQL 容器内给固定银行卡表增加临时可空探针列，不重扫 Meta 或更新保护规则，验证 `protection_schema_mismatch`，finally 精确删除探针列，并验证原规则修订未变及正常脱敏输出恢复。三条拒绝必须各有实际 UUID、同一历史且无结果，普通用户真实 Owner／Monitor 页面按各自具体原因和建议展示，六张新增截图为必需证据；不接受其他保护原因代替，不改变源授权或产品规则。

`protection_not_yet_valid` 是现有协议防御状态，产品没有保护开始时间的设置入口，不为验收新增该配置或更改宿主机／进程时钟。通过既有 `make test-common-postgres` 自动发现的真实 PostgreSQL 用例，用协议显式时间参数安装未来投影、重新 Open 后在当前时间拒绝准备，并验证执行诊断持久化／安全观察和无结果，再由正常的新修订恢复掩码输出；仅记 T2，不冒称完整 HTTP／浏览器 T4。上述扩展实际运行前均不得计为通过，原 v23 的通过范围保持。


## Service 角色方式撤销后的旧游标

同一 `security-mysql-owner-protection` 领域报告 v26 增加 `service_role_cursor_revocation` HTTP 验收，复用当轮 MySQL 银行卡合成表、正式 Assessment 和普通 User。通过本人角色自查与 Service 可选方式接口取得当前资格及引用；临时 Policy 只作用于 `service/service_execute`，默认前后保留 1/1，角色全遮盖 0/0，不新增或撤销用户角色、原值授权及源授权。只为该套件的一次性账号补已有 `service.definition.update`，其他套件权限不变。

先保存角色方式并发布私有原生表服务，JSON 与 CSV 实际读取 4 行及同一 JSON 游标的最后 1 行，独立核对五个唯一 ID、非敏感字段、全星号、Unicode、null 与 CSV 空单元格。撤销临时 Policy 后，必须等待 Service 同一投影 ID 的更高 revision、新发布 cursor 和 acknowledged 回执；保持服务定义及失效选择不变，用原游标分别请求 JSON、CSV，二者以及无游标对照必须精确返回 409／`query_protection_choice_invalid`，仅有安全错误、没有结果。随后验证可以保留失效选择直接停用，明确清除选择并保存、启用后恢复默认脱敏；不把选择撤销当作用户角色成员撤销。

运行报告仅保留游标 SHA256／长度、定义及策略修订、同步回执、HTTP 矩阵和独立结果断言，不归档游标、令牌或源值。成功、失败和中断均撤销本轮捕获的 Policy 并核对 revoked，删除本轮 Service 并确认 404，已有默认规则保持不变。新增场景是正式 HTTP T4，既有 Console 浏览器 v9 验收保持原范围；不新增浏览器通过声明。确定性回归仍经 `make test-security-online-runner`，真实隔离运行仍归现有 suite 和专门 CI 会话，未实际运行前不得记为 Hosted T4 通过。

## 用户角色资格撤销

领域报告 v27 的 `service_role_membership_revocation` 与上述方式撤销独立执行。System-owned 初始化夹具只在该隔离 suite 另建普通 Service 用户，功能权限放在其主角色，脱敏选择资格放在第二个仅重复已有服务定义读取权限的业务角色；角色管理 Permission 不可租户自定义，因此仅复用初始化时已有的隔离 Tenant 管理员，限定读取/撤销精确 Assignment，不进入数据消费路径。业务阶段经正式 IAM API 精确撤销第二个 Assignment，不修改主角色、Policy、Assessment、Service 选择或保护投影，不使用管理员消费身份或直接 SQL。

先证明所选方式及同一游标的两页 JSON/CSV 正常。撤销后，旧令牌必须 401，但这只计认证失效；随后通过正式密码登录签发同一用户的新会话，确认 Tenant/Membership/功能 Permission 不变、授权版本前进，本人角色自查只减少该业务角色。保持 Policy 修订、Service 定义、保护投影不变，旧游标 JSON/CSV 及无游标对照必须精确返回 403／`query_protection_roles_unavailable` 且无结果，不接受 401、409 或通用保护错误替代。显式移除选择后，默认脱敏五行两页仍可用；不重新授予业务角色。成功、失败和中断均核对目标 Assignment 已撤销，并精确清理本轮 Policy/Service，审计事实随隔离 Infra 销毁。

普通测试用户与隔离 IAM 控制身份的凭据只进入当轮 owner-only Secret 环境文件，不写入报告或传给浏览器；报告只记录身份、Assignment、当前角色与 Permission 边界、令牌失效状态、查询游标摘要和精确拒绝/恢复事实。复用 `make test-security-online-runner` 与既有 Hosted suite，不增加新部署、产品接口或 CI 路线。新增确定性通过不等于真实 Hosted T4 通过；CI 继续由专门会话跟进，角色自然到期和其他出口仍单列未验范围。

## 角色资格自然到期

领域报告 v28 的 `service_role_membership_expiry` 独立于方式撤销和资格主动撤销，沿用 v27 的普通测试用户、主功能角色、资格角色定义及隔离 IAM 控制身份。通过正式 IAM API 新建该用户的单条有期限资格 Assignment，期限基于 System 当前角色观察的数据库时间，仅用于本轮合成来源；不修改旧撤销记录、个人账号、产品期限配置或系统时钟。先配置有效 Policy 并等 Service 同步，再建立 90 秒资格、正式登录并验证两页五行 JSON/CSV 与旧游标可用，确保正常对照在到期前完成。

等待真实时间跨过期限后，同一 Access Token 必须仍能取得有效 AuthContext，主体/Tenant/Membership/授权版本/功能 Permission 不变，本人角色观察只减少该资格角色且数据库观察时间已跨过 `valid_until`。Assignment 的持久状态仍为 active、动态 `effective_state=expired`，期限不变、没有撤销事实；Policy/投影/服务定义与选择保持有效。旧游标 JSON/CSV 及无游标对照必须精确 403／`query_protection_roles_unavailable` 且无结果，不接受 401、方式失效409或通用保护错误替代。明确移除选择后，同一会话恢复五行默认脱敏。

自然到期路径不调用 revoke，不重新登录或续期。失败/中断时仅处理本轮捕获的精确 Assignment：仍有效时撤销，已自然到期时核对 expired 后保留审计事实；清理跨越到期点产生的冲突须重读核实，不重试角色写命令。Policy/Service 仍沿现有精确清理；报告只保留身份、期限、数据库观察、查询游标摘要与拒绝/恢复事实，凭据隔离沿 v27。确定性回归沿 `make test-security-online-runner`，实际 Hosted 尚待专门 CI 会话运行，不新增部署或 CI 路线。

## Develop 角色资格自然到期

领域报告 v29 的 `develop_role_membership_expiry` 使用独立普通 Develop 用户。主角色保留查询执行、数据读取、执行授权创建、本人历史读取及 Meta 读取；资格角色只重复主角色已有的服务定义读取 Permission，不增加功能权限。复用隔离 IAM 管理员及既有资格角色定义，正式 API 新建该用户的 90 秒 Assignment；不变更 Service 用户或其已到期审计记录。先发布 Develop/query 专用 Policy 并等待同步，再基于 System 数据库观察时间建立期限并登录一次。

到期前同一次真实查询必须得到角色全遮盖实时结果，而同一 UUID 的历史始终默认脱敏；保存的执行快照保留原选择。等待真实时间后，同一 Access Token、主体、Membership、授权版本和功能权限仍有效，Assignment active/expired 且没有撤销事实。保持 Policy、Assessment、投影及执行快照不变，复读旧实时结果必须精确 409/query_protection_roles_unavailable；保留选择重新查询必须形成新的真实 UUID，在 preparation 阶段以 protection_role_choice_unavailable 拒绝，同一历史无结果。旧成功历史仍默认脱敏；显式提交空选择的新查询恢复五行默认结果。新查询的执行授权期限必须仍有效，不能把会话、功能权限、执行授权或保护投影到期冒充资格拒绝。

仅失败或中断时清理尚有效的本轮 Assignment；成功不撤销、不重新登录、不续期。沿用精确 Policy 撤销、凭据隔离和 existing suite/Makefile 门禁，新增查询执行与审计事实随隔离 Infra 销毁，不新增产品接口或浏览器专项。确定性 T0/T1 与实际 HTTP T4 分别记录，CI 由专门会话跟进。

## Transfer 资格自然到期与旧文件

领域报告 v30 的 `transfer_role_membership_expiry` 独立使用普通 Transfer/Develop 导出用户。主角色固定持有手动任务 CRUD/执行、Develop 查询/读写效果、执行授权创建、Meta 读取及重复资格只读 Permission；额外资格角色沿用已有定义，只通过正式 IAM API 新建本用户的 90 秒 Assignment。先发布 transfer/export Policy 并等待回执，再开始期限，不改变 Service/Develop 先前用户、资格审计、源授权或原值授权。

到期前保存带选择的独立 bounded/snapshot/native-table 任务，真实执行将既有合成 MySQL 银行卡表五行写入套件既有 PostgreSQL 目标，逐值核对全遮盖及相邻字段。另用同一用户的成功 Develop 查询快照冻结独立 export 选择，经唯一全量导出路径生成实际 CSV 并下载核对五行、Unicode、null/空单元格；不将预览或成功状态代替文件字节。查询本身仍默认脱敏，与 export 选择分开。

等待真实时间后，要求同一会话、主体、成员、授权版本和主功能 Permission 不变，仅资格角色自然消失，Assignment active/expired 且无撤销。保留任务及选择时，启动和仅改名称的更新须精确 409/transfer_protection_roles_unavailable；本人直接 selection check 同样拒绝，任务执行列表保持原一条且目标逐值不变。旧成功导出会话状态、文件再次下载及从原查询再次创建导出均拒绝且无结果；当前 Common Transfer BFF 将409合并为方式不可用，因此 Develop 三处精确为409/query_protection_choice_invalid，不能伪称其已细分角色原因。System观察与Transfer直接拒绝独立证明资格归因，不解析错误正文，不改变现有错误契约。

随后显式清空任务选择再启动，同一用户恢复默认五行；新查询显式空 export 选择后生成新 CSV 并逐值核对默认保护。原有特殊方式文件仍拒绝，不因其他任务恢复默认而解锁。成功不 revoke/重登/续期；失败/中断仅清理本轮仍有效 Assignment，删除本轮任务并核对404、撤销本轮 Policy。复用已有合成目标及 Infra，任务执行、导出会话和文件事实由隔离 Infra 最终销毁；不新增永久清理接口或把保留审计计为残留失控。门禁沿 make test-security-online-runner 和既有 Hosted suite；确定性 T0/T1不代替实际 HTTP T4，CI仍由专门会话跟进。

领域 v31 修正上述 v30 的 BFF 原因合并：Transfer资格自然到期时，Develop旧会话、旧文件、原查询再次导出及默认恢复后旧文件复读都必须精确409/query_protection_roles_unavailable；方式失效仍为409/query_protection_choice_invalid。客户端、导出Service及三处HTTP错误写回分别回归角色/方式、401/403和异常协议；不改变保护裁决、权限或选择。已有v30记录只代表旧错误投影，不能当作新原因区分的T4证据。新版真实Hosted仍由专门CI会话跟进。

同一 v31 的两次 CSV 正向对照（资格到期前的角色方式、显式移除选择后的默认方式）还须在下载前，以同一普通 User 通过 `GET /api/v1/transfer/executions/{execution_id}` 复核会话所指 Transfer UUID：状态成功、无任务定义（`task_id=0`）、手动执行、`trigger_by` 为已验证的当前用户、实际读写均为五行。只验证会话成功或文件字节不能替代这项执行来源证明；任一事实不符即中止该对照，不下载文件、不产生通过结论。普通默认导出的 User Actor／Execution Authorization 私有字段不在该公开 DTO 中，也不能以 `trigger_by` 推导或补造。领域报告结构保持不变，复核的是已有 `transfer_execution_id`，无新增权限、入口或部署路线。确定性回归分别对两种方式注入发起人不匹配／缺失、错误 UUID、绑定任务、失败状态、非手动触发和读写计数错误，并继续要求精确清理；这些 T0/T1 反例不替代新的真实角色到期 HTTP／Hosted 验收。

领域 v32 在同一角色资格自然到期场景中增加导出响应缓存证据，取代 v31 作为当前运行契约，不新增部署、账号、角色、权限或验收入口。共享 HTTP 传输只额外保留实际 `Cache-Control` 头的全部值，不收集 Cookie 或其他凭据头；正常 JSON、CSV 字节流和 HTTP 错误响应均走同一读取路径。到期前角色 CSV 与主动清空选择后的默认 CSV，创建、每次状态读取、文件下载都必须精确返回单一 `no-store`，并在各自 CSV 证据中保留三阶段头值。到期后的旧会话状态、旧文件下载、原查询再次导出，以及默认恢复后的旧文件复读，继续要求精确 `409/query_protection_roles_unavailable`、无结果，并在 `export_refusal_cache_control` 中分别保留四个实际头值。缺失、空值、`no-cache`、允许保存／公开缓存或重复矛盾头均拒绝验收；不能以此前默认 CSV 的成功响应头替代这些资格失效证据。既有六个真实 UUID、同会话自然到期、投影与目标不变、默认恢复及精确清理边界全部保留。确定性反例和传输回归仍由 `make test-security-online-runner` 自动发现，真实隔离运行结果须由专门 CI 会话另行回传。

## 四出口逐原因矩阵

领域报告 v25 起在既有 suite 内给出 `protection_reason_matrix`，固定七种原因 × Manager preview、Develop query、Service execute、Transfer export。每个单元格必须明确 `runtime` 或 `protocol_only`，不能把共享协议测试当作对应 HTTP 出口已运行；矩阵的 runtime 单元格只有该次请求及对应强断言通过后才能生成。Manager／Service 保留原有仅含本地化 error 与 error_code 的保护 403，原因由受控的纳管状态、固定 SQL、原规则和结构变化事实证明，不增加公开子原因或解析错误正文。Develop／Transfer 校验同一执行 UUID 的历史、精确诊断、拒绝无结果；Transfer 另校验零写入及目标逐值不变。

| 原因 | Manager | Develop | Service | Transfer |
| --- | --- | --- | --- | --- |
| 计算输出不支持 | 协议层 | 实际 MySQL 查询 | 实际 MySQL SQL 服务 | 实际 PostgreSQL 查询源传输 |
| 血缘无法证明 | 协议层 | 实际 MongoDB 查询 | 协议层 | 实际 MongoDB 查询源传输 |
| 保护未就绪 | 实际 PostgreSQL 预览 | 实际 PostgreSQL 查询 | 实际 PostgreSQL 服务 | 实际 PostgreSQL 原生表传输 |
| 保护到期 | 实际预览 | 实际查询 | 实际服务 | 实际传输 |
| 尚未生效 | 协议层 | 协议层 | 协议层 | 协议层 |
| 结构不匹配 | 实际预览 | 实际查询 | 实际服务 | 实际传输 |
| 操作不支持 | 协议层 | 实际 MySQL 查询 | 实际 MySQL SQL 服务 | 实际 PostgreSQL 查询源传输 |

Transfer 的这两个 SQL 查询源拒绝复用套件已纳管的 PostgreSQL `spatial_algorithm_source.value_a` 合成字段，报告记录实际源 Locator 和引擎；它们不证明 MySQL 查询源已获支持。该矩阵最初使用的版本尚未声明MySQL `compute.query.read_session`，其配置校验400未计为保护拒绝；后续已实现的MySQL查询连续读取不改变这两项既有PostgreSQL拒绝探针的证据归属。MySQL 原生表的未就绪、结构不匹配、自然到期和正常脱敏搬运仍使用原有探针；不为验收扩大 Provider 能力、权限或源授权。

这是当前普通表／MongoDB 集合验收 profile 的覆盖边界，协议层单元格仍明确保留实际出口 T4 缺口，不表示产品入口永远不可能触发。`make test-common-postgres` 的 `TestProjectionStoreReasonMatrixAgainstPostgres` 对四个 Owner 的七种共享协议原因持久化、重开、拒绝和正常修订恢复作 T2 验证；PreparedQuery 证明与编译器拒绝由受控协议夹具提供，不能冒称原生引擎或 HTTP T4。尚未生效仍通过协议显式时间参数验证，不更改系统时钟或添加产品设置。新增临时 Service／Transfer 定义必须按实际 ID 删除并确认消失，结构探针沿既有归属 fixture 的 finally 恢复；无需额外数据库、部署入口或 CI 路线。实现、确定性门禁与实际 T4 结果分别记录，v24 旧证据不能替代新增 runtime 单元格。

未就绪探针不能依赖“尚未人工复核”的自动发现窗口：命中检测器置信度的 Finding 会直接采用默认保护并进入 active。套件因此复用已有、字段名不会命中检测器的 PostgreSQL 合成表，在发现完成且零 Finding、尚无人工 Assessment 的真实 enrolling 阶段验证四出口；不暂停 Worker、不加锁、不改检测器阈值或产品状态。报告记录该组实际源与发现完成事实，后续人工指定、三算法、SQL拒绝和银行卡流程沿原路径继续。

查询源拒绝语句必须符合实际原生方言：PostgreSQL 的字符串字面量过滤可以安全重写，不能因 MySQL 的保守限制而预期 PostgreSQL 拒绝。Transfer 的操作不支持探针使用直接字段的 `SELECT DISTINCT`（血缘直接、输出重写不支持）；计算输出探针使用 `CAST(value_a AS text)`（血缘派生），不依赖尚不能证明所有函数依赖的 SUBSTRING 重载。两者须经真实 PostgreSQL Provider／Transfer pipeline 的精确准备拒绝和目标不变回归，且保留字符串过滤合法对照必须只写脱敏值；不扩展 Provider 或函数可信范围。


## 默认导出文件随保护规则变化失效

领域报告 v33 取代 v32 作为当前套件契约，保留既有角色资格自然到期、四出口原因矩阵、MongoDB血缘和浏览器v9全部断言。本轮复用已纳管的合成MySQL银行卡五行表、现有普通治理账号及独立Transfer/export覆盖策略，不新增角色、权限、原值或源授权，不改变源数据和部署路线。Manager原生表来源必须携带Meta树提供的item_id以获得计划字段快照；Develop使用同一成功直接字段查询的固定快照，显式空查询／导出选择。MySQL查询连续读取已由既有Provider实现，本轮不扩展引擎能力。

同一用户完成默认、临时全遮盖、显式恢复默认三个阶段。每阶段分别经Manager原生表和Develop查询创建独立导出，核对会话与真实Transfer执行UUID、本人trigger_by、无任务定义、手动执行及实际五行读写，下载并逐值独立校验6份CSV的结构、唯一行标识、Unicode、null/空单元格、非保护字段及掩码。CSV仅在保护断言通过后写入现有artifact目录；恢复阶段按记录值与初始文件比较，不能要求未声明排序的原生表字节顺序固定。

临时覆盖发布并由Transfer确认更高修订后，两个初始会话的状态和文件在中英文下均应409/export_protection_changed；撤销该覆盖并确认再次推进投影后，初始及全遮盖的四个旧会话仍全部拒绝，合计24次真实拒绝。随后新建两个默认导出成功，旧文件不能因恢复原规则而复活。所有创建、状态轮询、成功文件和拒绝响应须单一no-store，拒绝JSON无结果／附件且给出本地化重新导出建议。传输层仅记录Cache-Control、Content-Type、Content-Disposition和X-Request-ID，不收集Cookie、Token或其他凭据头；每个最终边界以owner、阶段、会话、动作、语言及唯一request ID标识，不能复用展示名称当作唯一键。最终矩阵严格42项，状态轮询另留缓存检查证据。

本轮策略变更必须绑定自己的Assessment、Transfer/export和唯一验收原因，采用当前版本的乐观并发控制；已有活动覆盖或并发他人变更时拒绝接管。失败、中断以及写请求结果不确定时均按真实读回的本次归属／修订恢复为revoked，不重放写请求、不覆盖他人修改。身份和功能资格、保护基线及原查询快照保持不变。成功保留6个导出会话、执行与策略修订审计及文件证据，随既有隔离Infra最终销毁，不新增清理接口或声称领域零残留包含这些保留审计；不得把物理清理报告与本轮实际保留范围混淆。真实隔离运行尚需专门CI会话回传，确定性Runner门禁及此前本地Persons验收均不能替代新的Hosted证据。
