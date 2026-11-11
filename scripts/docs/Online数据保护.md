# ADDP 脚本：Online数据保护

当前代码领域报告为 v45，新增 PostgreSQL 分组三出口与方式撤销；浏览器为 v10、23 张必需图。最近已完成的真实 Hosted 仍是 v44/v10（421da33a），新增 v45 范围尚未运行，旧版本通过不覆盖新断言。

[返回脚本导航](../README.md) · [返回测试与发布入口](测试与发布入口.md#test---测试与发布门禁)

本页导航：[现行契约与证据边界](#现行契约与证据边界) · [Transfer 固定夹具](#transfer-固定夹具) · [原值访问身份与清理](#原值访问身份与清理) · [MySQL 与空间字段保护](#mysql-与空间字段保护) · [Service 角色方式撤销后的旧游标](#service-角色方式撤销后的旧游标) · [用户角色资格撤销](#用户角色资格撤销) · [角色资格自然到期](#角色资格自然到期) · [Develop 角色资格自然到期](#develop-角色资格自然到期) · [Transfer 资格自然到期与旧文件](#transfer-资格自然到期与旧文件)（[v30](#v30资格到期与旧错误投影)／[v31](#v31角色原因与执行来源)／[v32](#v32响应缓存证据)） · [四出口逐原因矩阵](#四出口逐原因矩阵) · [默认导出规则变化失效](#默认导出文件随保护规则变化失效)

测试分层、身份和清理规则以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)为准；历史结果仅适用于所列源码和运行尝试，不代表当前 HEAD 已重跑。

公共准备统一引用[部署 profile](Online编排与观测.md#部署-profile-与触发)、[身份与拓扑预检](../../docs/spec/testing/T4-Online验收规范.md#开关身份与拓扑预检)、[报告与凭据脱敏](../../docs/spec/testing/T4-Online验收规范.md#报告)及[数据、超时与清理](../../docs/spec/testing/T4-Online验收规范.md#数据超时与清理)。`security-transfer-protection` 使用 macOS 专用永久夹具；`security-plaintext-access` 与 `security-mysql-owner-protection` 使用各自 Hosted 一次性环境，身份与清理范围分别按下文执行。

## 现行契约与证据边界

当前领域报告为 `addp.security-mysql-owner-protection-online/v44`、浏览器报告为 `addp.security-manager-export-browser/v9`，以[现行校验器](../test/security-mysql-owner-protection-online.py)为准。本页按主题保留 v23～v44 的增量要求。当前报告继承仍适用的断言。

保护方式、角色资格及导出有效性的能力契约见[数据安全规范](../../docs/spec/addp数据安全与隐私保护实现规范.md#711-默认方式与角色可选方式)。本文维护 suite 断言与版本差异；已归档的固定源码、运行尝试、失败和清理结果见[数据保护与执行血缘历史证据](../../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md)，不由版本号推导某次验收已通过。其中[四出口实际停机 v9](../../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#四出口实际停机--t4-v9)、[四出口重新发现 v10](../../docs/spec/testing/evidence/数据保护与执行血缘历史证据.md#四出口重新发现--t4-v10)分别维护对应旧证据，不作为 v23～v33 新增范围的通过记录。

### Hosted 精确许可准备与清理

2026-11-07 用户确认：为继续现有字段检索及脱敏验收，原普通消费 User 在 MongoDB `Outdoor.Persons`、MySQL `security_fixture.customers`、`security_fixture.bank_card_accounts` 三项上各保留独立 `metadata_read`，另补一项精确 `read`。六项仅存在于当次 disposable Hosted 环境，原七张 PostgreSQL `read` 范围、角色、功能 Permission 及原值审批不变。read 只解决源读取资格，不能绕过 Security 脱敏。正式“元数据查看权应允许搜索字段名”规则由「数据权限-性能优化」会话调整，本轮不能以持有read的成功替代metadata-only能力验收。

独立准备账号沿既有扫描权限先 basic 识别两个引擎，再从 System 读取可信指纹／边界。先签发三项 metadata_read 并核对同一DataItem可见性，再逐项签发 read，核对当前用户固定Manager检索裁决的 content_readable 从false到true。不得更换账号、对父节点授权或扩大数据项范围。原v44字段高亮、算法／重启与v9浏览器断言保留，不以名称检索替代。

每项写入前把 request_id、action、精确目标、身份及Run记录到0600私有 `security-record-grants.json`，包含未知写入结果。普通消费与浏览器不接收准备凭据。应用停止前，独立子进程重载私有凭据，按动作先撤销read并核对不可读，再撤销metadata_read并核对不可见；任一项失败仍尝试其余项，两类动作均尝试。初始化失败、场景失败、中断及正常结束都走此收尾，清理失败影响总结果。checkpoint仅允许同Run／主体、三目标×两动作的六条唯一请求，拒绝动作错配、重复、跨范围或丢失已签发关系。

准备／清理回执改用 `addp.security-record-grant-fixture/v2`，文件为 `security-record-grant-preparation.json`、`security-record-grant-cleanup.json`；成功须三项read和三项metadata_read、pending为空、content_unreadable与metadata_hidden确认。新旧夹具回执不兼容，不将此前三项元数据许可通过当作六项通过。领域仍v44、浏览器仍v9；原两次Hosted失败保留，现有确定性门禁与真实T4分别认定。

### 统一门禁与结果认定

确定性入口为 `make test-security-online-runner`，由 `make test-online-runner` 与既有 Platform CI 覆盖；共享存储与 Provider 的 PostgreSQL／MySQL 门禁仍各自认定 T2。真实 HTTP／浏览器使用既有 `security-mysql-owner-protection` Hosted suite，由专门 CI 会话安排与回传。套件复用现有部署、账号、角色、产品接口与 CI 路线；各场景经用户确认的最小身份及精确许可范围仍是独立前提。

确定性回归、协议单元格、旧版本结果与实际 T4 分开记录。本文没有新增或刷新运行证据，各新增断言原有的“未运行／待回传”状态仍保留；尤其 v27 不证明自然到期或其他出口、v30 不证明 v31 的新错误区分、v24 不证明 v25 的新增 runtime 单元格，v37可选默认参数、v38默认值修订及v39新默认续页范围仍待独立Hosted证据。未运行、失败、跳过或清理失败均不能计为通过，身份、敏感信息和清理要求继续按各主题执行。

## Transfer 保护与原值访问

### Transfer 固定夹具

`security-transfer-protection` 的环境字段沿[macOS 场景准备](Online编排与观测.md#按场景补充)，完整必需项以[Host Gate](../test/online-host-gate.sh)为准；夹具、读取身份与断言见[正式场景](../../docs/spec/testing/scenarios/数据保护.md#security-transfer-protection)。该入口全量启动 System、Gateway、Meta、Security、Transfer 和 Manager，由 `business/scripts/online-security-transfer-fixture.sh` 管理固定 MongoDB 源与 PostgreSQL 目标。

专用环境长期预置分别指向只读 MongoDB 账号和 PostgreSQL Fixture 的 Engine Instance，专用 Tenant 预置平台推荐的手机号敏感类型、识别方式和默认保护规则。

suite 经 Gateway 扫描两端资源；首次运行允许为固定 MongoDB DataItem 建立一个永久 Security 纳管，后续必须复用同一纳管并确认 Transfer 已安装 `export → mask` 投影。每轮创建两条带 Run ID 的 bounded Transfer 任务：不输出手机号的可证明 MQL 必须正常写入，输出手机号的可证明 MQL 必须写入遮盖值且明文计数为零。

固定 Engine、纳管和目标表属于专用 Runner 的永久 Fixture，不按本轮临时资源删除；临时 Transfer 任务在 `finally` 按捕获 ID 删除并逐条确认 404，不能撤销投影、管理员绕过或为 MongoDB 增加白名单。

### 原值访问身份与清理

`security-plaintext-access` 使用唯一 Hosted Linux x86_64 一次性部署，由[共享 Security Host Gate](../test/online-hosted-security-gate.sh)准备；当前身份、精确表读取 Grant、初始化与退出范围统一见[原值访问正式场景](../../docs/spec/testing/scenarios/数据保护.md#security-plaintext-access)。System owner helper 创建两名不同的普通 User：申请人持有原值申请权限，审批人持有审批与撤销权限，两者都不取得准备身份权限。`ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN` 由当轮身份夹具生成，不能使用个人环境审批 Token 或 macOS 永久夹具替代。独立准备身份建立手机号识别和默认遮盖规则，完成当次 `addp_online_security.exemption_source` 的精确读取授权后移除准备 Token。

申请人在 Manager 预览确认手机号已遮盖并从该出口提交申请，另一人在 Security 批准；仅申请人在有效期内看到原值，审批人仍脱敏，到期后 Manager 根据本地投影恢复遮盖，不调用 Security 判定或刷新投影。双用户页面与提前撤销的独有要求见[页面验收](../../docs/spec/testing/scenarios/数据保护.md#双用户页面与提前撤销)，真实停机期间的本地到期判定见[Security 不可达专项](../../docs/spec/testing/scenarios/数据保护.md#security-不可达期间的到期收敛)，各自运行与未覆盖范围分别认定。

Enrollment、Assessment、申请、授权及不可变修订在业务验收期间完整保留，退出随该次 disposable Infra 整体销毁；不篡改或直接删除审计，也不将业务期间保留与最终零残留混为一谈。身份、浏览器报告、恢复或清理缺失／失败均不能计为通过，具体证据要求沿上述正式专题。

## MySQL 与空间字段保护

该套件扩展至v44后的Hosted总场景预算为2400秒，单请求仍最多30秒；其他套件预算、产品保护同步30秒及三组资格自然到期各90秒保持。阶段观测沿同一领域Runner写入 `security-phase-timings.json`（`addp.security-phase-timings/v1`），每个固定阶段开始／结束原子保存状态、UTC时间及单调时钟耗时，包含准备、关系出口、角色生命周期、默认导出、清理、MongoDB与浏览器。未完成的阶段保留running，失败不写错误正文，记录不含SQL、数据、参数、凭据或原值。此文件只用于耗时／停止位置定位，不能替代原v44报告、v9浏览器、21图或清理证明；进程被强制终止时不得根据最后一次快照声称该阶段完成。

2026-11-08 用户确认：仅此套件的一次性身份复用 System 验收夹具已有的长场景会话配置，AccessTokenTTL 与 RefreshTokenFamilyTTL 均为4小时，完整身份及凭据仍随本轮 Job 的隔离生命周期销毁。原因是实际运行超过32分钟后，旧30分钟准备令牌在六Grant收尾时被正式认证以 token_expired 拒绝；从私有文件重载同一旧令牌不会续期。只调整 fixtureSessionConfig 的本套件分支，不改变产品默认会话、其他套件、权限、保护／授权有效期、90秒角色资格到期或40分钟场景预算，不新增刷新重试路线。更早运行的清理缺口保留，调整后的真实收尾须重新验收。

同构建重启仍必须保持 build_id／git_commit／source_fingerprint，不以重新生成文档为由放宽断言。标准Swagger入口按技术栈规定的swag模块构建版本选用工具，避免旧生成器修改docs.go导致验收中重编译；`swag --version`展示常量不作为版本事实。工具修复与本地生成稳定性只计T1，实际Hosted重启仍须由运行产物证明。

Develop 查询几何输出严格遵守[查询几何值契约](../../docs/spec/addp引擎插件接口规范.md)：在原生 EWKB 完成保护后，唯一 JSON 形状是 `{wkt, srid}`。空间属性保护夹具要求该对象恰含这两个成员，WKT 为有限二维 Point、SRID 为整数4326、坐标与合成源逐行一致；不把 GeoJSON、裸WKT或十六进制原生值作为 Develop 的兼容输入。Manager 预览、Service 原生表读取和 Transfer 成果预览保持各自现行表协议，不套用 Develop 查询投影。原查询、保护字段与四出口算法断言不变，不通过 SQL 空间函数改写规避原生输出核验。

`security-mysql-owner-protection` 的唯一 Hosted 部署、临时 workspace 和四出口参与链路统一见[正式准备规则](../../docs/spec/testing/scenarios/数据保护.md#部署与四出口主链路)，由[Security Host Gate](../test/online-hosted-security-gate.sh)复用共享生命周期。初始 MySQL 与空间表子场景使用当次 PostGIS PostgreSQL 与 MySQL；System owner helper 创建一次性 Tenant、最小权限非管理员 User、独立引擎登记用户和独立治理初始化用户；Business 只管理物理数据及 owner-only Engine descriptor，引擎登记和分类、等级、email/phone 敏感类型及默认保护初始化分别通过 System、Security 正式 API 完成。初始化令牌在业务验收前移除，凭据隔离沿[T4 专用环境](../../docs/spec/testing/T4-Online验收规范.md#专用环境)。

suite 先验证默认保护同事务创建、非法参数整体回滚和探针清理，再经 Meta 扫描和 Security 正式 API 建立 Enrollment/Assessment。MySQL `customers.email` 在 Manager、Develop、Service、Transfer 四个出口均移除，但保留 5 条非敏感记录；临时任务及服务按捕获 ID 清理。所有退出路径按[T4 清理规则](../../docs/spec/testing/T4-Online验收规范.md#数据超时与清理)停止本轮应用、销毁当次业务容器及 Infra，并核对零残留；`runner.temp` 的 owner-only 凭据目录随生命周期删除。

同一 Hosted suite 的 Console 浏览器阶段使用上述普通 User 正式登录，在 Manager 从已纳管的 MySQL `customers` 点击导出表格式能力声明支持的 CSV，通过 Python 标准 CSV 解析器核对完整五行、敏感 email 字段移除及非敏感字段保留，再以同一身份查询并打开 Monitor 中对应的 Transfer 一次性执行。执行必须成功、来源为 Manager、无任务定义引用，且 `triggered_by` 与已验证 User 一致；不允许浏览器自报身份或用 API 创建代替点击导出。测试身份只额外持有数据探查页面必需的 `manager.content.read`、`manager.derived_artifact.create/read` 与 `monitor.execution.read`；原有 `manager.data_item.read` 同时参与页面入口校验，准备身份仍不进入业务阶段。Playwright 失败、缺失或不匹配的浏览器报告使整个 suite 失败。全新匿名浏览器初始化时唯一一次 `/system/refresh` 的 401 及对应 Chromium 网络告警单独计数并验证；正式登录成功后，任何 401、业务失败响应或 warning/error 均使验收失败，JavaScript 异常全程检查。当前浏览器报告使用 `addp.security-manager-export-browser/v9`，完整必需字段以[现行校验器](../test/security-mysql-owner-protection-online.py)为准；保留上述匿名刷新计数、安全执行 UUID、业务计数与断言结果，截图归档，原始下载文件删除。Monitor 加入同一版本健康预检，应用与文件仍由既有 Hosted 生命周期清理；此扩展通过真实 T4 前不计为已验收。

同一 suite 还使用 PostgreSQL `addp_online_security.spatial_algorithm_source` 与 `spatial_algorithm_transfer` 验收空间表的普通属性脱敏。三个字符串字段通过 Meta 当前组件清单形成正式 Assessment，绑定当次初始化的 `phone` 类型及其初始等级；空间列名为 `location_point`。依次将该组合的默认保护切换为前后掩码、固定值替换、普通 SM3，等待四个 Owner 确认最新投影后核对五行输出、Unicode、null、空串异常效果和点坐标。掩码阶段还单独为 Manager 三个字段配置全掩码、固定值替换、SM3，验证字段配置只作用于 Manager preview，其他出口仍执行默认保护。测试成功、失败及中断退出时撤销本轮字段策略、删除临时 Service/Transfer 任务，并恢复运行前的完整 phone 默认规则；审计历史与当次 Enrollment/Assessment 随 disposable Infra 一同销毁。若发现前次遗留的活动字段策略，预检失败，不能替本轮静默接管。专用 User 还需 `security.assessment.create`、`security.policy.read/create/update/delete` 和 `security.protection_baseline.update`，当次 Tenant 与业务引擎仅供本 suite 使用。确定性协议、失败清理与 Fixture 回归由现有 `make test-online-runner` 和平台 T0 覆盖；真实执行仍使用 `make test-online ONLINE_SUITE=security-mysql-owner-protection`，未真实运行不得标记为 T4 通过。

### v23：通配符、计算拒绝与原因归类

单表通配符的 Provider T2 由既有 Common PostgreSQL `query-read-set` 与 MySQL 数据保护门禁覆盖，验证掩码／替换／移除、SM3 协同、四个 Owner 的共享消费、原值授权及默认历史、零行结构变化拒绝和事务结构锁。MySQL 隐藏列不按完整字段表强行展开。

Hosted `security-mysql-owner-protection` 报告 v23 的 `bank_card_metadata.develop_wildcard` 额外要求真实 Develop 执行 `SELECT *` 和 `SELECT b.*`，与同一固定夹具的直接字段脱敏逐项对照；

`bank_card_metadata.develop_mixed_wildcard` 要求额外执行直接别名位于通配符前后的两种混合投影，逐值核对敏感字段及副本的相同保护、NULL／缺失语义和非敏感副本保持；

`bank_card_metadata.develop_page_wildcard` 另外核对单层包装的外层星号／直接字段两种查询；

`service_sql_page` 使用实际 Meta 字段发布包含混合通配符的正式 SQL 服务，通过真实 cursor 连读 2／2／1 三页，严格校验全部敏感副本、行序和无重复遗漏，并删除本轮服务定义。

`computed_outputs` 另外要求真实 Develop 的 SUBSTRING／COALESCE／CASE／CAST／派生别名／UNION 六种敏感计算均在准备阶段拒绝，复读同一历史无结果；count(*) 和未受保护字段计算继续成功。额外发布的私有 SQL Service 必须按正式 403 合同拒绝敏感派生输出且没有结果，定义纳入本轮精确清理。只有实际运行通过才记为 T4，不把 Runner 单元测试计作运行验收。

保护原因细分沿用 execution-failure/v3 的闭合 reason：v23 必须精确核对 MySQL 六种计算及其历史为 `protection_computed_output_unsupported`，MongoDB 无法证明的派生输出为 `protection_lineage_unverified`，自然到期的 Develop／Transfer 为 `protection_expired`。浏览器同时核对具体原因与对应处理建议，不接受通用保护原因代替这些已证明原因；非法值导致的结果阶段拒绝尚无进一步类型化事实时仍为 `data_protection_required`。不得解析错误正文猜测子原因、放宽为任意保护错误或将原值授权扩大为计算许可。

### Service 已有拒绝场景的具体说明

未就绪、结构不匹配、操作不支持、计算输出不支持、自然到期的五个既有Service HTTP场景，除原403、`data_protection_required`及严格两成员响应外，还须校验各自完整的原因与处理建议。场景已确定预期原因，不从错误正文反推原因。领域客户端未设置Accept-Language，沿正式中间件默认中文校验独立固定文案；不导入产品翻译文件来生成期望值，也不接受通用保护、读取权限不足、其他原因或附带私有细节的文案替代。

本项仅收紧原请求的运行断言，v44领域／v9浏览器报告结构、20runtime／8protocol_only矩阵、资源、身份、请求数、预算和清理保持；没有新语言请求或新页面场景。新断言尚须专门CI会话安排真实运行，旧5a00785ae运行只证明原断言，不视为本项通过。Service尚未生效、无法证明血缘的原协议范围不扩为HTTP；既有英文到期页面证据保持独立。标准入口和自动发现已覆盖，不新增Make目标、workflow或部署依赖。

### v24／浏览器 v9：未就绪与结构拒绝

#### 未就绪探针

未就绪探针不能依赖“尚未人工复核”的自动发现窗口：命中检测器置信度的 Finding 会直接采用默认保护并进入 active。

纳管前，普通消费账号通过既有精确刷新入口，对原七张已获 read 的 PostgreSQL 表之一 `spatial_algorithm_source` 完成 deep 扫描，再读取同一 DataItem 并创建全新 Enrollment。准备账号的 basic 扫描只证明目录身份，不证明字段结构已就绪；无字段事实的零命中发现可以合法完成，但没有字段快照 Hash，不能代替本探针要求的有结构、零命中发现。不扩大扫描范围、权限或原总 deadline，也不放宽发现 UUID／字段快照 Hash 和前后修订不变的断言。

在既有、字段名不会命中检测器的 PostgreSQL 合成表人工指定敏感字段之前，先等待自动发现真实完成且 Finding 数为零，再等待五个 Owner 的 enrolling 空规则投影回执齐备，确认仍处于尚无人工 Assessment 的真实 enrolling 阶段后验证四出口 `protection_not_ready`；全部请求后再次核对同一发现、投影修订和 enrolling 状态不变，随后才沿原流程人工指定敏感字段并验正常脱敏。探针期间不暂停 Worker、不加锁、不改检测器阈值或产品状态。报告记录该组实际源与发现完成事实，后续人工指定、三算法、SQL拒绝和银行卡流程沿原路径继续。

#### 操作与结构拒绝

规则激活后，直接字段带字符串字面量过滤超出当前保守 SQL 输出重写子集时，验证 `protection_operation_unsupported`。

仅在当轮带 Security 归属标签的 MySQL 容器内给固定银行卡表增加临时可空探针列，不重扫 Meta 或更新保护规则，验证 `protection_schema_mismatch`，finally 精确删除探针列，并验证原规则修订未变及正常脱敏输出恢复。

三条拒绝必须各有实际 UUID、同一历史且无结果，普通用户真实 Owner／Monitor 页面按各自具体原因和建议展示，六张新增截图为必需证据；不接受其他保护原因代替，不改变源授权或产品规则。

#### 尚未生效的协议范围

`protection_not_yet_valid` 是现有协议防御状态，产品没有保护开始时间的设置入口，不为验收新增该配置或更改宿主机／进程时钟。通过既有 `make test-common-postgres` 自动发现的真实 PostgreSQL 用例，用协议显式时间参数安装未来投影、重新 Open 后在当前时间拒绝准备，并验证执行诊断持久化／安全观察和无结果，再由正常的新修订恢复掩码输出；仅记 T2，不冒称完整 HTTP／浏览器 T4。上述扩展实际运行前均不得计为通过，原 v23 的通过范围保持。


## Service 角色方式撤销后的旧游标

SQL角色服务定义检查分别验证：快照没有单一Meta Source、SQL文本与发布请求一致、`named_parameters` 与发布的参数数组完全一致。无命名参数的管理DTO仍必须返回 `[]`，不接受null或省略成员；不得把三项检查合并为“伪造来源或SQL改变”，也不在夹具中把null兜底成空数组。Service统一DTO转换边界负责保留已声明数组形状，不改变SQL、实际绑定、保护方式或来源快照。

同一 `security-mysql-owner-protection` 领域报告 v26 增加 `service_role_cursor_revocation` HTTP 验收，复用当轮 MySQL 银行卡合成表、正式 Assessment 和普通 User。通过本人角色自查与 Service 可选方式接口取得当前资格及引用；临时 Policy 只作用于 `service/service_execute`，默认前后保留 1/1，角色全遮盖 0/0，不新增或撤销用户角色、原值授权及源授权。只为该套件的一次性账号补已有 `service.definition.update`，其他套件权限不变。

先保存角色方式并发布私有原生表服务，JSON 与 CSV 实际读取 4 行及同一 JSON 游标的最后 1 行，独立核对五个唯一 ID、非敏感字段、全星号、Unicode、null 与 CSV 空单元格。撤销临时 Policy 后，必须等待 Service 同一投影 ID 的更高 revision、新发布 cursor 和 acknowledged 回执；保持服务定义及失效选择不变，用原游标分别请求 JSON、CSV，二者以及无游标对照必须精确返回 409／`query_protection_choice_invalid`，仅有安全错误、没有结果。随后验证可以保留失效选择直接停用，明确清除选择并保存、启用后恢复默认脱敏；不把选择撤销当作用户角色成员撤销。

运行报告仅保留游标 SHA256／长度、定义及策略修订、同步回执、HTTP 矩阵和独立结果断言，不归档游标、令牌或源值。成功、失败和中断均撤销本轮捕获的 Policy 并核对 revoked，删除本轮 Service 并确认 404，已有默认规则保持不变。新增场景是正式 HTTP T4，既有 Console 浏览器 v9 验收保持原范围；不新增浏览器通过声明。门禁与待回传状态沿[统一结果认定](#统一门禁与结果认定)。

### v34：SQL 双别名、撤销与默认恢复

同一领域 Runner 增加 `service_sql_role_cursor_revocation`，与原生表场景复用同一生命周期与逐页校验，不新增 suite、账号、权限或引擎。复用已扫描的合成银行卡表及正式 Assessment，发布私有单表 SQL，仅将敏感源字段直接输出为 `card_alias`、`card_copy`，不构造 SQL 的 Meta Source 快照。候选仅通过 POST JSON 提交 SQL 与引擎，必须只返回一个真实源字段身份；保存的单条角色选择不能改成别名身份。新选项 ID 由 Security 返回，Runner 不自造。

原生表和 SQL 场景均先发布空选择的默认服务，读取 4/1 两页 JSON／CSV；再创建本轮 Service 全遮盖方式并等待同一 Owner 投影新 revision/cursor acknowledged，保存选择并验证两个输出副本、Unicode、NULL、空字符串移除及普通字段。撤销后保持定义和旧游标不变，JSON／CSV 续读及无游标请求精确 409/`query_protection_choice_invalid`，无数据、不可缓存；保留选择的保存及停用后再次启用同样拒绝，版本不前进。直接停用保留失效选择，主动移除并保存后才恢复默认，恢复两页 CSV 的哈希必须与初始默认文件一致，不能把 `default_restored` 按全遮盖验算。

成功及拒绝响应严格验证单个 `Cache-Control: no-store`；报告只保存行数、结构、CSV 哈希、游标哈希／长度、角色观察和策略／定义／投影修订，不归档原值、CSV 正文、完整游标或令牌。本人角色、Membership、授权版本和默认 Baseline 保持；不做角色资格或原值／源授权变更。新 SQL 场景增加一个受控 Service 的创建／删除计数，政策修订保留为隔离 Infra 内审计事实，成功、失败及中断仍按捕获 ID 撤销 Policy、删除服务并确认 404。Console 浏览器 v9 及原有图片数量与边界不变。

确定性入口仍为 `make test-security-online-runner`，CI 自动登记必须同时约束 v34、SQL 场景派发及报告字段，不能仅新增未执行函数。本文约定的是新增断言，不表示 v34 已实际执行通过；既有本地 MySQL Persons 的十二项／六份 CSV 证据见 [Service 本地 SQL 验收](../../service/docs/testing/查询与要素协议.md#sql-单表角色方式本地验收2026-11-03)，不能替代隔离合成来源的新 Hosted/T4。CI 仍由专门会话跟进，不自行派发、查询、重跑或设置定时。

### v35：SQL 输入命名参数与游标绑定

在 v34 无参数 SQL 场景之外增加 `service_sql_parameter_role_cursor_revocation`，复用同一角色生命周期、合成表和普通 User。SQL 用 `:minimum_id` 替代过滤常量，发布一个 `int`、必填、无默认值的命名参数；只读取证明过的直接字段及双别名，不把参数用作标识符或 SQL 片段。保存时只持久化声明和角色方式引用，不保存本次调用取值，也不构造 Meta Source 快照；所有读取、角色候选和续页均显式提交同一参数映射，参数不放入 URL。

参数值为 1 时沿原 4/1 两页 JSON／CSV 验证默认、全遮盖及恢复；另以参数值 2 独立核对四行结果为 ID 2～5，普通字段、Unicode、NULL、双别名及掩码仍遵守同一 oracle，服务版本不因单次参数值变化而改变。用首个参数值取得的旧游标改用另一个值请求 JSON／CSV，必须精确返回 400/`invalid_structured_query`，无结果、JSON 错误正文及单个 `Cache-Control: no-store`。缺少必填、未声明参数和非法标量类型也按同一参数错误契约拒绝，不把它们误算作 409 保护方式失效。

边界检查分别在初始默认、角色方式有效及显式移除后恢复默认阶段执行；方式撤销后的续读与无游标拒绝继续使用合法且不变的参数，避免用参数错误冒充保护拒绝。前后定义、命名参数声明、当前用户角色及默认 Baseline 保持；成功、失败或中断沿同一捕获 ID 撤销 Policy、删除本轮服务并确认 404。只增加一个私有 Service 的创建／删除计数，不新增账号、权限、引擎或部署，不归档参数取值、游标、令牌、原值或 CSV 正文。

该阶段报告为 v35（现由 v42 接续），CI 登记必须约束新增场景的派发／报告、参数边界事实及服务计数。确定性门禁沿 `make test-security-online-runner`，不新增测试入口或工作流；实际 v35 Hosted/T4 尚未运行，仍由专门 CI 会话跟进。本切片只验证一个必填整数 SQL 输入，不扩展为全部参数类型、可选默认值、原值授权或角色资格变更的运行证据。

### v36：SQL 角色资格撤销与自然到期

在既有资格撤销、自然到期场景中，各增加一个私有参数 SQL 双别名服务，复用同一普通 User、有效 Policy、单次资格变更与真实等待，不新增角色、权限或第二次资格分配。原生表覆盖保留。SQL 使用既有必填无默认值的 int 参数 `minimum_id`，运行值固定为1；候选、首读、续页、拒绝与恢复都提交同一参数，不把参数错误冒充资格失效。

失效前两个服务分别完成4/1两页JSON/CSV正向对照；失效后保持各自服务定义、选择、Policy及保护投影不变，SQL旧游标及新请求的JSON/CSV均须精确403/`query_protection_roles_unavailable`，只有错误、JSON错误类型及单个no-store，不接受401、409或400。主动撤销仍先证明旧令牌401、正式重登同一普通User后资格403；自然到期仍保持同一有效会话、授权版本和主功能Permission，仅资格active/expired，不重登、续期、撤销或修改时钟。显式移除选择后两个服务分别恢复默认五行JSON/CSV。

SQL定义只保存声明，不伪造Meta Source，不持久化调用值。报告记录SQL独立服务ID、字段、受控结果哈希、游标摘要、精确拒绝和恢复；不保存凭据、游标全文、CSV或源值。两个新增Service均立即捕获ID，任何失败或中断都逐一删除并核对404，即使一项清理失败仍尝试其他清理；同一Policy仅由原生命周期撤销。资源总计增加2。该阶段报告v36（现由v42接续），原浏览器v9不扩展；沿现有Makefile入口及CI自动登记，无新增工作流。本文为断言合同，v36实际Hosted/T4未运行，CI仍由专门会话跟进。

### v37：可选默认参数的保护与游标

保留必填参数及SQL资格生命周期，在同一领域Runner增加 `service_sql_default_parameter_role_cursor_revocation`：发布一个必填性为false、默认值为1的int参数 `minimum_id`，SQL仍限定单普通表直接字段／双别名，不新增账号、角色、权限、引擎或部署。新增服务与其他场景共用角色方式选择、撤销、主动移除及精确清理主路径。 候选接口尚未接收已保存服务定义，按既有协议显式绑定发布默认值；正式查询才检验省略参数，不能把候选缺少SQL绑定当作默认消费。

在默认保护、角色全遮盖、恢复默认三个阶段，分别验证省略整个parameters成员、空参数映射、显式默认值和合法改值。省略与显式默认值的两页JSON／CSV须返回同样行／列及CSV字节；由省略参数产生的游标可带显式默认值续读，由显式默认值产生的游标也可省略参数续读，服务版本保持。改为另一合法值须真实筛选出不同ID，拿旧游标改值则精确400/`invalid_structured_query`、无结果、JSON错误类型、单个no-store。显式null、布尔、对象、空字符串及未声明参数同样拒绝，不能把显式null当作未传而套默认值。

服务定义中保存的是发布默认值，不是本次调用值；任意读取后声明、默认值、SQL及保存选择均不变，不伪造Meta Source。方式撤销后，省略和显式默认值的新查询及旧游标JSON／CSV都精确409/`query_protection_choice_invalid`，不能用400参数错误替代保护拒绝，也不静默默认恢复。主动移除角色选择后恢复初始默认CSV。新增一个私有Service的创建／删除计数，当前唯一报告v37，浏览器v9保持；报告不归档运行参数取值、源值、CSV正文、游标全文或凭据。确定性入口及CI登记沿已有Makefile，不新增工作流。此为验收合同，不表示新增Hosted/T4已运行，仍由专门CI会话跟进。

### v38：发布默认值修订与旧游标拒绝

沿[正式默认值修改入口](../../service/docs/architecture/查询发布与执行.md#已发布-sql-参数默认值修改)，在v37可选参数服务已完成角色撤销、主动清除及初始默认CSV恢复后，使用同一Service的版本PUT将minimum_id默认值从1改为2。定义必须持久化新默认并推进管理版本；SQL、来源、其他声明和空角色选择保持，运行查询不得反向写入默认。旧恢复游标在省略参数及显式旧默认1时，JSON／CSV都精确400/invalid_structured_query、无结果、JSON错误正文和单个no-store；显式旧值也拒绝以证明服务版本绑定，不能只验证改参数错误。

新查询省略参数与显式新默认2的JSON／CSV均返回2～5行且CSV字节一致；新查询显式旧值1仍可正常分页1～5，所有输出继续按当前默认保护核对双别名、NULL及空单元格。服务依赖版本须变化；既有角色事实和Baseline复核仍执行。报告新增published_default_revision，沿原摘要规则，不保存凭据、源值、CSV正文或游标全文；不增加Service／Policy／角色／部署数量，领域唯一v38、浏览器仍v9，继续既有失败／中断精确清理。确定性反例覆盖默认未持久化、版本未变、SQL被改及旧游标放行。现有标准门禁和CI登记同步覆盖，`make test-security-online-runner`退出0（41／80／198／185／376项及登记CLI），所属171项通过；新增Hosted/T4尚未运行，仍由专门CI会话跟进，不把T1/T2/T3当作Hosted结果。

### v39：新默认值游标的连续第二页

沿v38同一默认修订服务，新增固定可选string参数minimum_code（默认CUST-10001，谓词作用于原customer_code），仍仅修订int默认值；候选显式绑定两项，实际查询分别验证省略与显式两项默认。保留四次旧版本游标拒绝及显式旧值新查询。当前合成来源共5行，默认minimum_id从1改2后恰有4行，故用两页各2行与单次4行作参考；对应本地Persons两页5行与10行对照的同一契约，不新增样本或Service。修订前显式替代值取得第二页，修订后分别验证省略默认的新游标＋显式新默认续读、显式新默认的新游标＋省略默认续读。两页ID稳定排序、不重复、合并等于四行参考；各行的普通字段、双别名默认掩码、NULL／移除及CSV空单元格继续按独立合成值验证，三组第二页CSV字节须一致，Service依赖版本保持修订后值。

本矩阵正向JSON／CSV及无游标四行参考共用既有读取与断言。CSV分页has-more和Service-version响应头必须各恰好一个，且与对应JSON一致；HTTP传输只额外保留这两个无凭据头的全部值，禁止采集完整游标或通用Header。新游标改int为1或改string下界为CUST-10003的JSON／CSV共四次请求须精确400/invalid_structured_query、无数据、JSON错误及单个no-store，不能替代旧版本拒绝。报告在published_default_revision下增加continuation_checks，仅保留计数、ID／CSV摘要、版本与布尔事实，不保存源值、CSV正文、完整游标或凭据。两方向第二页、四行参考、CSV分页头、拒绝状态／原因／缓存和失败／中断精确清理必须有确定性反例。

领域唯一报告升级v39，浏览器仍v9、资源计数及领域／Infra清理边界不变。沿既有make test-security-online-runner及CI登记和删除派发／报告反例同步覆盖，不新增workflow或权限。新矩阵接入及确定性门禁不是Hosted通过；已有本地9项／9组JSON-CSV证据保留在Service测试专题，Hosted仍由专门CI会话跟进。

### v40：允许值列表内的默认修订与失败不变性

沿既有[参数允许值契约](../../service/docs/architecture/查询发布与执行.md#命名参数展示与固定选项)，同一可选参数服务发布int允许值1/2及固定string允许值CUST-10001/CUST-10003，并保留两种语言的选项标签。SQL、来源、角色生命周期和资源数量不变；v39的新游标改int=1或string=CUST-10003仍使用合法选项，必须因游标绑定而拒绝，不能被列表外错误替代。

合法int默认1→2前，正式版本PUT分别提交列表外int=3、列表外string=CUST-10002、合法int与非法string混合的部分更新，均须400/invalid_named_parameter_defaults、只含本地化error/error_code且无结果。每次GET完整定义须与更新前相同，不推进版本、不修改默认／允许值／标签；旧默认真实游标在这些失败后仍可正常返回最后一行JSON/CSV，版本及单个no-store、分页头保持。随后合法默认更新必须持久化且只改默认，v39的新旧游标拒绝、双方向续页、默认保护、CSV空单元格及精确清理全部继续执行；报告在published_default_revision中增加invalid_default_updates、unchanged_default_cursor及option_domains_preserved，不记录凭据／游标全文／源值。

领域唯一v40、浏览器v9及14+task_ids资源计数保持，沿现有Runner与CI登记门禁，不新增部署或权限。Service标准PostgreSQL补选项保留、列表外失败不落库及失败后旧游标继续可用；Console已有默认编辑用例增中英文固定选项变体，不替代自由标量用例。确定性、受控浏览器、个人环境HTTP与Hosted分别记录；本轮Hosted仍由专门会话跟进，不计未运行通过。

### v41：字符串默认值修订后的保护与续页

沿同一可选参数Service和[已发布默认值修改契约](../../service/docs/architecture/查询发布与执行.md#已发布-sql-参数默认值修改)，完成int默认修订后保持minimum_id=2，仅将minimum_code默认从CUST-10001改为列表内CUST-10003。修改前用显式新string值取得2行＋1行与单次3行参考；string的null、数字、布尔、空串、列表外值及混合部分非法更新均须400/invalid_named_parameter_defaults，完整定义、两个默认、选项与双语标签、版本不变，原int修订游标仍可续读。合法string更新只改变该默认及现有版本事实，原版本游标省略／显式原默认的JSON／CSV四次请求均400/invalid_structured_query、无结果、单个no-store。

修订后省略和显式两默认值的新查询／双方向续页须与修订前显式新string参考相同，仍按独立源值核对两别名掩码、NULL／移除、CSV空单元格、逐页版本与has-more头；新查询显式旧string值仍返回原2～5行掩码结果。新游标改int或string合法选项的四次请求须明确拒绝；stale管理version更新须409/resource_version_conflict且定义不变。报告在published_default_revision中增加string_default_revision，不归档游标全文、凭据或源值；领域唯一v41，浏览器v9、14+task_ids资源计数及既有全部清理边界保持。不新增服务、Policy或部署，沿现有Service Go、PostgreSQL及Security Runner/CI登记门禁。该段是断言合同，实际结果分层记录，未运行的个人环境HTTP／Hosted不计通过。

## 用户角色资格撤销

领域报告 v27 的 `service_role_membership_revocation` 与上述方式撤销独立执行。System-owned 初始化夹具只在该隔离 suite 另建普通 Service 用户，功能权限放在其主角色，脱敏选择资格放在第二个仅重复已有服务定义读取权限的业务角色；角色管理 Permission 不可租户自定义，因此仅复用初始化时已有的隔离 Tenant 管理员，限定读取/撤销精确 Assignment，不进入数据消费路径。业务阶段经正式 IAM API 精确撤销第二个 Assignment，不修改主角色、Policy、Assessment、Service 选择或保护投影，不使用管理员消费身份或直接 SQL。

先证明所选方式及同一游标的两页 JSON/CSV 正常。撤销后，旧令牌必须 401，但这只计认证失效；随后通过正式密码登录签发同一用户的新会话，确认 Tenant/Membership/功能 Permission 不变、授权版本前进，本人角色自查只减少该业务角色。保持 Policy 修订、Service 定义、保护投影不变，旧游标 JSON/CSV 及无游标对照必须精确返回 403／`query_protection_roles_unavailable` 且无结果，不接受 401、409 或通用保护错误替代。显式移除选择后，默认脱敏五行两页仍可用；不重新授予业务角色。成功、失败和中断均核对目标 Assignment 已撤销，并精确清理本轮 Policy/Service，审计事实随隔离 Infra 销毁。

普通测试用户与隔离 IAM 控制身份的凭据只进入当轮 owner-only Secret 环境文件，不写入报告或传给浏览器；报告只记录身份、Assignment、当前角色与 Permission 边界、令牌失效状态、查询游标摘要和精确拒绝/恢复事实。门禁沿[统一结果认定](#统一门禁与结果认定)；本项不证明角色自然到期和其他出口。

## 角色资格自然到期

领域报告 v28 的 `service_role_membership_expiry` 独立于方式撤销和资格主动撤销，沿用 v27 的普通测试用户、主功能角色、资格角色定义及隔离 IAM 控制身份。通过正式 IAM API 新建该用户的单条有期限资格 Assignment，期限基于 System 当前角色观察的数据库时间，仅用于本轮合成来源；不修改旧撤销记录、个人账号、产品期限配置或系统时钟。先配置有效 Policy 并等 Service 同步，再建立 90 秒资格、正式登录并验证两页五行 JSON/CSV 与旧游标可用，确保正常对照在到期前完成。

等待真实时间跨过期限后，同一 Access Token 必须仍能取得有效 AuthContext，主体/Tenant/Membership/授权版本/功能 Permission 不变，本人角色观察只减少该资格角色且数据库观察时间已跨过 `valid_until`。Assignment 的持久状态仍为 active、动态 `effective_state=expired`，期限不变、没有撤销事实；Policy/投影/服务定义与选择保持有效。旧游标 JSON/CSV 及无游标对照必须精确 403／`query_protection_roles_unavailable` 且无结果，不接受 401、方式失效409或通用保护错误替代。明确移除选择后，同一会话恢复五行默认脱敏。

自然到期路径不调用 revoke，不重新登录或续期。失败/中断时仅处理本轮捕获的精确 Assignment：仍有效时撤销，已自然到期时核对 expired 后保留审计事实；清理跨越到期点产生的冲突须重读核实，不重试角色写命令。Policy/Service 仍沿现有精确清理；报告只保留身份、期限、数据库观察、查询游标摘要与拒绝/恢复事实，凭据隔离沿 v27。确定性门禁与尚待回传的 Hosted 范围沿[统一结果认定](#统一门禁与结果认定)。

## Develop 角色资格自然到期

领域报告 v29 的 `develop_role_membership_expiry` 使用独立普通 Develop 用户。主角色保留查询执行、数据读取、执行授权创建、本人历史读取及 Meta 读取；资格角色只重复主角色已有的服务定义读取 Permission，不增加功能权限。复用隔离 IAM 管理员及既有资格角色定义，正式 API 新建该用户的 90 秒 Assignment；不变更 Service 用户或其已到期审计记录。先发布 Develop/query 专用 Policy 并等待同步，再基于 System 数据库观察时间建立期限并登录一次。

到期前同一次真实查询必须得到角色全遮盖实时结果，而同一 UUID 的历史始终默认脱敏；保存的执行快照保留原选择。等待真实时间后，同一 Access Token、主体、Membership、授权版本和功能权限仍有效，Assignment active/expired 且没有撤销事实。保持 Policy、Assessment、投影及执行快照不变，复读旧实时结果必须精确 409/query_protection_roles_unavailable；保留选择重新查询必须形成新的真实 UUID，在 preparation 阶段以 protection_role_choice_unavailable 拒绝，同一历史无结果。旧成功历史仍默认脱敏；显式提交空选择的新查询恢复五行默认结果。新查询的执行授权期限必须仍有效，不能把会话、功能权限、执行授权或保护投影到期冒充资格拒绝。

仅失败或中断时清理尚有效的本轮 Assignment；成功不撤销、不重新登录、不续期。沿用精确 Policy 撤销、凭据隔离和 existing suite/Makefile 门禁，新增查询执行与审计事实随隔离 Infra 销毁，不新增产品接口或浏览器专项。验证层级与待回传范围沿[统一结果认定](#统一门禁与结果认定)。

## Transfer 资格自然到期与旧文件

### v42：扫描准备身份与业务精确刷新

用户已确认本切片仅适配Hosted隔离夹具。现有独立source initializer在原11项源准备功能之上，仅对security-mysql-owner-protection追加meta.scan_task.execute/read，沿其已有正式引擎管理范围完成PG整引擎basic初识别；首次扫描成功后按原正式接口签发addp_online_security内既有7张表的精确read Grants，随后才由consumer等待检索可用。准备身份须在任何扫描／授权写入前核验为同一临时Tenant内与consumer不同的当前User，权限精确为原集合加两项；任一身份或权限不符即停止，不自动补权。

消费User保持原角色与功能权限。初始化后的PG扫描只通过原7张表中的精确Meta item_id请求deep刷新，显式携带对应engine_id，不请求整引擎、不使用父schema范围、不留准备Token到业务Runner或浏览器。精确项必须由consumer自己的正常目录解析后证明名称、table类型及正整数ID；目标不在原7表、身份解析不明或扫描失败时明确停止。MySQL／MongoDB来源扫描和源授权范围保持既有契约，不对其新增数据项授权。MongoDB查询三个PG目标分别刷新并归档逐表真实扫描UUID映射，删除旧的单个目标scan字段；其他单目标扫描UUID保持其实际精确项语义，报告明确PG精确刷新范围，领域唯一v42，浏览器v9与14+task_ids资源计数保持。

门禁沿System夹具Go及现有make test-security-online-runner：覆盖准备／消费角色分离、basic初识别先于7表授权、消费检索在授权后、扫描失败无授权写入、缺少／多余权限停止、精确item请求体、越表拒绝、三个目标均刷新、无消费侧PG整引擎路径和失败／中断清理。CI登记同步当前领域版本与派发／报告断言，不新增workflow、产品API或备用路线。四出口、六计算、Mongo、角色资格生命周期、int/string默认修订、浏览器及完整清理断言全部保留。本切片的门禁和真实Hosted结果分别记录，不自动追加派发或把原v41初始化失败改记通过。

### v30：资格到期与旧错误投影

领域报告 v30 的 `transfer_role_membership_expiry` 独立使用普通 Transfer/Develop 导出用户。主角色固定持有手动任务 CRUD/执行、Develop 查询/读写效果、执行授权创建、Meta 读取及重复资格只读 Permission；额外资格角色沿用已有定义，只通过正式 IAM API 新建本用户的 90 秒 Assignment。先发布 transfer/export Policy 并等待回执，再开始期限，不改变 Service/Develop 先前用户、资格审计、源授权或原值授权。

到期前保存带选择的独立 bounded/snapshot/native-table 任务，真实执行将既有合成 MySQL 银行卡表五行写入套件既有 PostgreSQL 目标，逐值核对全遮盖及相邻字段。另用同一用户的成功 Develop 查询快照冻结独立 export 选择，经唯一全量导出路径生成实际 CSV 并下载核对五行、Unicode、null/空单元格；不将预览或成功状态代替文件字节。查询本身仍默认脱敏，与 export 选择分开。

等待真实时间后，要求同一会话、主体、成员、授权版本和主功能 Permission 不变，仅资格角色自然消失，Assignment active/expired 且无撤销。保留任务及选择时，启动和仅改名称的更新须精确 409/transfer_protection_roles_unavailable；本人直接 selection check 同样拒绝，任务执行列表保持原一条且目标逐值不变。旧成功导出会话状态、文件再次下载及从原查询再次创建导出均拒绝且无结果；v30 当时的 Common Transfer BFF 将409合并为方式不可用，因此 Develop 三处精确为409/query_protection_choice_invalid，不能伪称其已细分角色原因。

System观察与Transfer直接拒绝独立证明资格归因，不解析错误正文，不改变现有错误契约。

随后显式清空任务选择再启动，同一用户恢复默认五行；新查询显式空 export 选择后生成新 CSV 并逐值核对默认保护。原有特殊方式文件仍拒绝，不因其他任务恢复默认而解锁。成功不 revoke/重登/续期；失败/中断仅清理本轮仍有效 Assignment，删除本轮任务并核对404、撤销本轮 Policy。复用已有合成目标及 Infra，任务执行、导出会话和文件事实由隔离 Infra 最终销毁；不新增永久清理接口或把保留审计计为残留失控。门禁与待回传范围沿[统一结果认定](#统一门禁与结果认定)。

### v31：角色原因与执行来源

v31 修正上述 v30 的 BFF 原因合并：Transfer资格自然到期时，Develop旧会话、旧文件、原查询再次导出及默认恢复后旧文件复读都必须精确409/query_protection_roles_unavailable；方式失效仍为409/query_protection_choice_invalid。客户端、导出Service及三处HTTP错误写回分别回归角色/方式、401/403和异常协议；不改变保护裁决、权限或选择。已有v30记录只代表旧错误投影，不能当作新原因区分的T4证据。新版运行证据按[统一结果认定](#统一门禁与结果认定)单列。

#### CSV 执行来源与拒绝条件

同一 v31 的两次 CSV 正向对照（资格到期前的角色方式、显式移除选择后的默认方式）还须在下载前，以同一普通 User 通过 `GET /api/v1/transfer/executions/{execution_id}` 复核会话所指 Transfer UUID：状态成功、无任务定义（`task_id=0`）、手动执行、`trigger_by` 为已验证的当前用户、实际读写均为五行。只验证会话成功或文件字节不能替代这项执行来源证明；任一事实不符即中止该对照，不下载文件、不产生通过结论。普通默认导出的 User Actor／Execution Authorization 私有字段不在该公开 DTO 中，也不能以 `trigger_by` 推导或补造。

领域报告结构保持不变，复核的是已有 `transfer_execution_id`，无新增权限、入口或部署路线。确定性回归分别对两种方式注入发起人不匹配／缺失、错误 UUID、绑定任务、失败状态、非手动触发和读写计数错误，并继续要求精确清理；这些 T0/T1 反例不替代新的真实角色到期 HTTP／Hosted 验收。

### v32：响应缓存证据

v32 在同一角色资格自然到期场景中增加导出响应缓存证据，当时取代 v31；当前领域报告已由 v37 接续，不保留平行运行契约。该版本不新增部署、账号、角色、权限或验收入口。共享 HTTP 传输只额外保留实际 `Cache-Control` 头的全部值，不收集 Cookie 或其他凭据头；正常 JSON、CSV 字节流和 HTTP 错误响应均走同一读取路径。

到期前角色 CSV 与主动清空选择后的默认 CSV，创建、每次状态读取、文件下载都必须精确返回单一 `no-store`，并在各自 CSV 证据中保留三阶段头值。

到期后的旧会话状态、旧文件下载、原查询再次导出，以及默认恢复后的旧文件复读，继续要求精确 `409/query_protection_roles_unavailable`、无结果，并在 `export_refusal_cache_control` 中分别保留四个实际头值。

缺失、空值、`no-cache`、允许保存／公开缓存或重复矛盾头均拒绝验收；不能以此前默认 CSV 的成功响应头替代这些资格失效证据。既有六个真实 UUID、同会话自然到期、投影与目标不变、默认恢复及精确清理边界全部保留。确定性反例和传输回归沿[统一门禁](#统一门禁与结果认定)，该版本要求不替代当前 v37 的独立运行证据。

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

未就绪探针的发现、空规则回执和清理前提统一见[v24 未就绪探针](#未就绪探针)，不另设自动发现窗口或修改产品状态。

查询源拒绝语句必须符合实际原生方言：PostgreSQL 的字符串字面量过滤可以安全重写，不能因 MySQL 的保守限制而预期 PostgreSQL 拒绝。Transfer 的操作不支持探针使用直接字段的 `SELECT DISTINCT`（血缘直接、输出重写不支持）；计算输出探针使用 `CAST(value_a AS text)`（血缘派生），不依赖尚不能证明所有函数依赖的 SUBSTRING 重载。两者须经真实 PostgreSQL Provider／Transfer pipeline 的精确准备拒绝和目标不变回归，且保留字符串过滤合法对照必须只写脱敏值；不扩展 Provider 或函数可信范围。


## 默认导出文件随保护规则变化失效

### v33：现行范围与来源绑定

领域报告 v33 取代 v32 作为当前套件契约，保留既有角色资格自然到期、四出口原因矩阵、MongoDB血缘和浏览器v9全部断言。本轮复用已纳管的合成MySQL银行卡五行表、现有普通治理账号及独立Transfer/export覆盖策略，不新增角色、权限、原值或源授权，不改变源数据和部署路线。Manager原生表来源必须携带Meta树提供的item_id以获得计划字段快照；Develop使用同一成功直接字段查询的固定快照，显式空查询／导出选择。MySQL查询连续读取已由既有Provider实现，本轮不扩展引擎能力。

### 三阶段导出与文件核对

同一用户完成默认、临时全遮盖、显式恢复默认三个阶段。每阶段分别经Manager原生表和Develop查询创建独立导出，核对会话与真实Transfer执行UUID、本人trigger_by、无任务定义、手动执行及实际五行读写，下载并逐值独立校验6份CSV的结构、唯一行标识、Unicode、null/空单元格、非保护字段及掩码。CSV仅在保护断言通过后写入现有artifact目录；恢复阶段按记录值与初始文件比较，不能要求未声明排序的原生表字节顺序固定。

### 旧会话失效与响应证据

临时覆盖发布并由Transfer确认更高修订后，两个初始会话的状态和文件在中英文下均应409/export_protection_changed；撤销该覆盖并确认再次推进投影后，初始及全遮盖的四个旧会话仍全部拒绝，合计24次真实拒绝。随后新建两个默认导出成功，旧文件不能因恢复原规则而复活。所有创建、状态轮询、成功文件和拒绝响应须单一no-store，拒绝JSON无结果／附件且给出本地化重新导出建议。传输层仅记录Cache-Control、Content-Type、Content-Disposition和X-Request-ID，不收集Cookie、Token或其他凭据头；每个最终边界以owner、阶段、会话、动作、语言及唯一request ID标识，不能复用展示名称当作唯一键。最终矩阵严格42项，状态轮询另留缓存检查证据。

### 策略恢复、审计与未覆盖范围

本轮策略变更必须绑定自己的Assessment、Transfer/export和唯一验收原因，采用当前版本的乐观并发控制；已有活动覆盖或并发他人变更时拒绝接管。失败、中断以及写请求结果不确定时均按真实读回的本次归属／修订恢复为revoked，不重放写请求、不覆盖他人修改。身份和功能资格、保护基线及原查询快照保持不变。成功保留6个导出会话、执行与策略修订审计及文件证据，随既有隔离Infra最终销毁，不新增清理接口或声称领域零残留包含这些保留审计；不得把物理清理报告与本轮实际保留范围混淆。此矩阵现已由文末v44／v9实际Hosted运行补证；确定性Runner门禁及此前本地Persons验收本身仍不能替代Hosted证据。

## Transfer PostgreSQL 两表查询脱敏落盘

领域报告 v43 在空间算法阶段之后读取原有的两个精确授权表 `spatial_algorithm_source` 和 `spatial_algorithm_transfer`。左表使用已恢复的手机号默认掩码，右表对上一阶段写出的合成 SM3 数据创建正式 Assessment 和本轮 Transfer/export 固定替换策略。沿原有 System 身份、Gateway、Transfer 任务 API 与独立 bounded Worker 执行，等待右表的 Transfer 投影确认后再提交查询。

查询按右表原值排序，使用 `LIMIT 2 OFFSET 1`，目标表应只有 ID 1 和 3。ID 1 左列为掩码，ID 3 左列因源值过短写入 NULL，右列均为固定替换值。两个别名分别保留左、右字段来源。敏感表达式 `pg_catalog.lower` 分别写向已存在目标和未创建目标，两次均在准备阶段以 `protection_computed_output_unsupported` 拒绝，读写计数为零。每条用例核对原执行 UUID、发起人和首个 Worker attempt 的开始及终态事件。

当次 PostgreSQL 夹具提前创建一个空目标，随后由 Worker 写入。物理核对检查两个精确目标的表标识、行和不存在状态，并以两个源表的摘要核对源数据。领域报告只保存判定及数量。任务按 API 删除并确认 404，固定替换策略按版本撤销并等待同步。物理清理只处理本轮预检时已证明不存在的目标名，校验两表均无残留。右表的治理记录与执行审计随隔离环境销毁。确定性校验仍使用 `make test-security-online-runner`，真实 T4 由专门 CI 会话执行。

## Service PostgreSQL 两表 JOIN 角色资格撤销（v44）

复用空间算法及 Transfer JOIN 已准备的两个 PostgreSQL 普通表 `spatial_algorithm_source` 与 `spatial_algorithm_transfer`、原有精确表范围和 Service 普通用户。两个来源的 `value_a` 各自安装 Service 角色选项。候选必须按 DataItem 返回两条同名字段及各自输出列，保存只选择左表。撤销前逐页核对 JSON 与 CSV，左表两个别名全遮盖，右表保持默认掩码，NULL 与 CSV 空单元格对应。查询按 id 分页，每页状态及 CSV 响应头独立核验。

沿现有真实 IAM 撤销，仅撤销同一个资格分配。旧会话核对 401，同一普通用户重新登录后，两种格式的旧游标及无游标查询均须精确 403 / `query_protection_roles_unavailable`、无结果且 `no-store`。两侧 Policy、投影和已保存选择保持。主动移除选择后恢复双表默认脱敏及两页结果。临时服务与两条角色策略在成功、失败或中断后精确清理。领域报告在 `service_role_membership_revocation.postgres_join_eligibility` 留存候选列、分页、拒绝和恢复证据。浏览器合同仍为 v9。


## v44／v9 实际 Hosted 证据（2026-11-08）

[Run42454542147／Job128650659824](https://github.com/pampa0629/addp/actions/runs/42454542147/job/128650659824)，attempt1，实际SHA `bd68a07014353d5c4a693fd863b3a3428bfdf460`。领域v44、浏览器v9、27阶段及完整清理通过，21图全部目视，六项临时metadata/read许可最终正式撤销；默认导出42条边界和六份CSV、Transfer两表JOIN、Service SQL资格与默认参数修订等限定范围由本次运行补证。不是早期v33／v43提交的精确重跑，原失败仍保留。

独立审计核对原始Run／Job与实际步骤、归档SHA／CRC、同构建新实例重启、拒绝原因与UUID、六份CSV固定值及全部清理回执；报告断言和未保留的原始HTTP正文不混淆。七原因四出口保持20个runtime／8个protocol_only，尚未生效仅T2。证据、未验范围和全部审计命令统一见[Security本次验收记录](../../security/CLAUDE.md#v44-完整-hosted-验收与清理闭环2026-11-08)。本节只记录实际验收，不改变上述稳定合同或扩展权限；后续不再将本次已覆盖的导出、JOIN和资格生命周期统列为“Hosted未运行”。


2026-11-08，Service细原因解释接入后的同一自然到期浏览器探针，改为检查“保护规则已到期＋续期或长期有效”的精确文案及预览区域持续提示，不再要求通用文案的短暂toast；恢复后提示须消失。403、两成员错误体、五行恢复、原资源与21张图片要求不变，领域v44／浏览器v9报告结构不变。新版断言的确定性检查和前端T3不代替新的真实Hosted运行；上一节bd68的成功仅适用当时源码。详见[本轮实现与验证](../../service/docs/testing/查询与要素协议.md#service-保护拒绝细分与持续提示2026-11-08)。

### 浏览器 v10：同一自然到期周期的中英文 Service 页面

在既有私有 Service 与 Transfer 自然到期浏览器周期内，通过 Console 的正式语言菜单切换为中文，不更改 localStorage 或伪造语言响应。Service 初始英文五行结果保持；自然到期后先核对英文拒绝，再切换中文，以实际请求的 `Accept-Language: zh-cn` 和完整中文原因／续期建议核对同一403。两个页面均必须持续展示错误区域、清空旧结果，并且不能显示“暂无数据”替代失败。恢复同一资源为长期有效并同步后，先验证中文五行安全结果／错误消失，再回到英文验证原恢复场景。切换后必须等待 iframe 控件语言同步；恢复后保留原资源精确清理。

浏览器唯一报告升级 v10，新增 `service_chinese_expiry_verified` 和 `service_chinese_recovery_verified` 两项严格布尔事实；预期 Service 403 从1变为2，新增中文拒绝／恢复两张必需图片，原21图保持，总数23。领域报告 v44、20runtime／8protocol_only、同一90秒自然到期、原资源数量、主体、权限与40分钟预算不变。新增三次Service请求（中文拒绝、中文恢复、中文初始安全结果），无新增业务写入；语言切换通过现有Console/iframe协议，不增加认证或保护协议。中英文均需先有五行结果再验证清空。此切片只补真实到期页面的中文证据，不代表其他原因已完成真实UI验收；未执行的新Hosted不得记为通过。现有Security Runner及登记门禁自动覆盖，新增版本／图片与反例同次适配，不新增workflow或运行路线。

### 技术字段检索消费现行续查契约

Security准备、重新发现／重启恢复及浏览器中的技术字段检索沿Manager唯一 `GET /search` 契约，固定q和engine范围，API批次采用合法page_size=50；不传管理页码、不接受旧total／页码模型。JSON仍是 `data.results`，并明确核对page_size、布尔exhausted及不透明next_cursor。直到exhausted为true才完成本次完整查找；短批或空批不能代替耗尽，空批有游标必须继续。保持同一查询和页大小，将游标原样传回，重复游标、畸形分页及跨引擎／重复目标身份均拒绝；不在日志／报告保存完整游标。既有短deadline约束完整续查，不因分页追加等待或每页重置deadline。

浏览器准备的同源技术索引探针同样续查所有批次；实际检索页面通过现有“继续查找／加载更多”按钮完成同一游标链，不能仅检查首页或以直接API命中代替页面展示。继续保留真实字段名高亮、精确locator和资源身份核对。保护尚未就绪与搜索隔离只按原稳定码等待；403权限不足、400、无效／到期游标等明确失败，不自动重开查询掩盖错误。此为已授权Security验收消费者适配，不改变Manager产品协议、权限、数据或保护规则；v44/v10、90秒、23PNG、两个Service403及全部清理要求保持。新增分页模拟反例纳入原Security Runner／登记入口，不新增workflow。新的Hosted复验需用户明确授权，不能将原6ef失败或清理成功记为中文页面通过。

### 精确源许可准备与清理的观察协议

Security隔离夹具复用 `online-engine-registration.py` 的 `ExactRecordGrants`，read许可的准备／签发后／撤销后及最终观察必须遵守[System搜索观察协议](../../docs/spec/addp授权上下文规范.md)。每次独立观察生成新的16字节小写十六进制query_nonce，首次显式expected_observation_tag=null，验证规范观察时间、精确目标、布尔裁决、可见目标指纹及合法不透明标记；同次观察再带原nonce和标记复核，依据变化或协议不符直接失败，不重开或重试。签发／撤销故意改变依据，因此下一观察另起新nonce，不把变更前标记跨写入当作预期不变事实。标记不落盘、不写日志、不成为权限或未来Allow；metadata_read仍使用原meta-query观察，不修改System接口／权限／业务保护规则。

这是现行消费者契约修正，新增真实请求形状／标签时序的确定性拒绝回归，由既有Security Runner标准入口及其当前Platform CI覆盖；共享调用方Transfer沿原入口验证。没有新增运行时、数据库、配置或Hosted重派授权。旧失败、清理失败和未执行部分分别保留，修复测试不能替代真实六Grant和v44/v10验收。

### v45：PostgreSQL 分组三出口与方式撤销

手工派发该 suite 时，可用 `security_source_commit` 锁定已审阅的完整小写 40 位源码 SHA；只接受派发提交的祖先，且必须与实际 checkout 的 HEAD 一致。该输入只用于 `security-mysql-owner-protection`，不改变其他 suite 的源码选择。派发提交与实际源码提交分别留证，领域、浏览器和清理报告按实际源码核验；省略时沿本次派发提交执行。

沿现有 v44 套件增加 `postgres_grouped_outputs` 领域阶段，唯一领域报告升为 v45；浏览器仍为 v10、23 张必需图，原七表精确读取、账号、权限、90 秒资格到期及 2400 秒预算保持。复用已纳管的 `spatial_algorithm_source.value_a` 原值样本和正式 Assessment，不新增源表或读取许可。限定单表直接列 `GROUP BY id,value_a`、`id AS bucket`、`value_a AS contact` 及 `count(*) AS n`，按原 id 排序，保留五个分组与计数；不扩展 HAVING、其他聚合或产品语法。

Develop 查询及重复历史、私有 SQL Service 两页 JSON／CSV、Transfer bounded/snapshot 独立任务和物理目标均与独立固定样本核对。初始默认规则保留前3／后4，三个出口分别沿现有 Policy 创建前1／后1默认及全遮盖角色选项，使用同一普通登录用户的现有角色保存选择；不借用发布人身份、不改角色资格。全遮盖后仅撤销这三项方式，等待各 Owner 确认；保留旧选择的 Develop 执行须准备阶段精确拒绝、同一历史无结果，Service 新读和旧游标的 JSON／CSV 须409/`query_protection_choice_invalid`且 no-store，Transfer 原任务启动须409/`transfer_protection_choice_invalid`且不新增执行或改写物理目标。该阶段验证方式撤销，不将它计为角色资格撤销或自然到期。

显式移除选择后重新查询、服务读取和任务执行，恢复最初默认输出。期间核对普通用户本人角色、授权版本与全套 Baseline 不变；原值、SQL全文、令牌、游标及CSV正文不进入报告，仅归档保护输出摘要、真实UUID、计数和拒绝事实。复用已有受控物理夹具，目标只能为本Run的精确分组表；先确认不存在，再由fixture准备归属于原Writer的表，正式任务沿既有replace写入。成功、失败或中断都尝试删除捕获的任务／服务、撤销活动Policy并等待同步、删除及证明目标不存在、核对源摘要不变；清理失败保留原业务错误且使整体验收失败。新增一个Service和两条任务计入领域资源清理，Policy审计保留到一次性Infra销毁。现有Security Runner、Fixture及登记门禁同次覆盖，无新workflow或本地服务启停。此为新断言合同，真实v45 Hosted尚未运行，交专门CI会话安排后再认定结果。
