# ADDP 测试与验收规范

本文定义 ADDP 测试分层、标准入口、环境边界、CI 编排和 Online / Release 验收的稳定规则。具体可用目标、suite 登记和运行参数以根目录 `Makefile`、`scripts/README.md`、`scripts/test/` 及模块 owner 文档为准。

## 一、基本原则

1. 每项测试必须归入 T0-T5，并明确 owner、依赖、数据边界和触发方式。
2. 平台只统一协议、入口、编排、安全检查和报告；业务夹具、断言、清理及残留核验归 owner 模块。
3. 根 `Makefile` 是测试与验收的唯一公共入口，复杂准备和安全检查放在 `scripts/test/`；不得保留模块私有旁路、兼容别名或在 workflow 中复制业务测试逻辑。
4. 测试结果只有通过、失败和按规范明确跳过三种语义。缺少依赖、凭据或未实际执行测试不得伪装成通过。
5. 开发服务生命周期与测试生命周期分离。`scripts/dev/restart.sh` 不隐式运行测试，日常测试入口也不接管开发者已经启动的服务。
6. 专用 T4 Runner 可以在宿主机准入通过后编排隔离测试部署，但不得复用或接管个人开发环境。

## 二、测试分层

| 层级 | 名称 | 典型内容 | 运行依赖 | 默认触发 |
| --- | --- | --- | --- | --- |
| T0 | 静态与契约检查 | 格式、依赖与登记一致性、授权清单、Swagger 路由覆盖、测试夹具约束 | 无运行中服务 | 每次改动、PR |
| T1 | 单元与组件测试 | Go package、Python 单元、Repository mock、Vue 组件与状态逻辑 | 无，或进程内临时资源 | 每次改动、PR |
| T2 | 模块集成测试 | Migration、Repository、事务、锁、真实 PostgreSQL / Redis / 对象存储 | disposable 基础设施 | 相关模块改动、PR |
| T3 | 前端 Smoke / E2E | 路由、权限提示、状态恢复、关键表单和浏览器交互 | 独立测试端口或受控夹具 | 相关前端改动、PR |
| T4 | 跨模块 Online 验收 | 真实 System 认证、Gateway 路由、多个 owner、Worker、补偿和最终一致性 | 隔离的完整 ADDP 测试部署 | 手工；首跑稳定后可夜间执行 |
| T5 | 发布认证 | 安装包生命周期、真实 OS 凭据后端、HA、故障切换、在线厂商证据 | 产品或 Runtime 专用认证环境 | 发布前、Tag 或手工执行 |

T0-T3 证明确定性代码与模块集成；T4 证明真实运行拓扑；T5 证明产品或 Runtime 的发布条件。测试文件所在目录、与某个发布 workflow 共用编排，都不能改变其层级。

## 三、标准入口

PG 源数据授权生命周期使用独立 `pg-source-authorization` T4 suite，经 `make test-online ONLINE_SUITE=pg-source-authorization` 在 GitHub Hosted disposable 部署执行。复用正式 IAM 夹具、共享 Hosted 生命周期及已有 PostgreSQL 业务源，不连接个人开发库。办理 User 与消费 User 分离，前者仅具备本场景办理资格，后者只具备 Meta 基础扫描／目录和 Manager 读取功能权限；消费 User 不获得签发或委托资格。对单张普通表验证未授权拒绝、有限期 Grant、重复签发拒绝、精确 basic 强制重扫后身份与边界不变、Deny 优先及解除、撤销和自然到期；直接批准阶段的相邻已扫描表保持未授权。读取须经过真实 Manager 预览，不用 System read-check 成功替代；同一消费凭据跨自然到期前后使用，不调整时钟或手工撤销代替到期。失败或中断按原命令编号收权、解除本轮 Deny，核对无当前来源，并由 Hosted 生命周期销毁本轮账号、源容器和 Infra。报告仅保存身份引用、状态、行数和边界摘要，不保存凭据或原始数据。确定性反例与生命周期清理由 `make test-pg-authorization-online-runner` 验证并纳入 Online/Platform 门禁；首次真实 Hosted 通过前保持手工触发，不计为已完成 T4，不加入夜间调度。

同一 PG suite 还验证同一 DataItem 上的个人、部门和项目组三种独立来源：普通成员与无成员关系的同权限旁观 User 分离，组织成员关系在正式 IAM 夹具中先建立、再签发会话，不扩大办理或消费 Permission。当前授权查询须准确合并三种来源，全部记录按直接接收主体筛选；逐项撤销个人／部门来源不能误删项目组来源，个人和组织 Deny 分别解除，最后一个 Grant 撤销后仍保留 Deny-only 解除入口。另以夹具已有的租户管理员独立执行成员查询／结束命令，通过正式 IAM API 依次结束消费 User 的部门和项目组关系，不创建含受限 IAM Permission 的自定义角色：每次变更后旧 Access Token 在 AuthContext 和 Manager 预览均须返回 401；同一账号经正式登录取得当前凭据后，退出部门仍可由项目组读取，退出最后一个组织则拒绝读取。身份、Tenant Membership 和四项消费 Permission 不变，当前来源随成员关系减少，组织 Grant 及直接历史保留，不能把成员退出实现为全组撤权。每个阶段均与真实 Manager 预览对照，旁观 User 和相邻表始终拒绝；失败或中断按本轮全部接收主体清理，不能只清个人 Grant。凭据只保存在 owner-only 临时文件和进程内存，不进入报告；同一场景还保持真实 Console／Manager 浏览器会话打开，在两次成员退出后通过页面重读触发既有 401 静默刷新：每次只轮换一次 Refresh Cookie，Console 与 iframe 的身份和新 Token 收敛，保留同一 iframe、路由和资源选择，不重复登录或误跳页面级 Forbidden；仍有项目组来源时继续显示两行夹具数据，最后一个来源消失时显示数据项级拒绝并清除旧行。控制协调只传本轮标识和阶段，不传 Token；原始 Playwright 输出及失败附件留在 Secret 目录，公开报告只保留状态与计数。此成员退出阶段不包含业务批准；Catalog 批准在后述独立阶段验收，其他引擎不在本 suite 范围内。

PG 同一 suite 另覆盖 Meta 事实依赖短暂离线与恢复：只在 disposable Hosted 部署通过标准生命周期停止／启动 Meta，不改变源表、DataItem 边界、IAM 权限或凭据。已有 Grant 且无 Deny 时，事实查询与 Manager 读取须明确返回依赖不可用，不回退原生读取，也不伪装未扫描；离线期间可对已知授权目标建立 Deny。恢复后不重扫，数据项身份、批准摘要与原 Grant 历史不变，原 Deny 仍阻止实际读取；显式解除后方可由原有效 Grant 读取，最后撤销并清理。故障操作有 Hosted 准入、超时、真实监听及进程身份验证；失败时仍尝试恢复 Meta 并清理本轮规则。此项是事实依赖可用性验收，不计为同身份组成边界扩大或列授权验收。

PG 业务批准沿用同一 suite，在上述阶段完成后，对此前未授权的相邻单表显式初始化 Catalog 批准要求。隔离部署增加 Catalog，仍不启动 Asset／Portal 或扩展其他引擎。独立业务负责人仅具备目录读取、盘点、编目和共享确认权限；办理 User 另具备目录读取、盘点与独立办理权限，消费 User 的权限保持不变。使用正式基础扫描变化自动建档、编目责任、共享边界核对、业务确认与办理 API，经 Catalog／System 各自 OAuth 身份自动受理和签发；验收脚本不持有机器 Token、不直接写批准或 Grant。仅业务确认时实际读取仍拒绝，签发后核对相同 DataItem 指纹、边界、接收方、期限及唯一签发历史，并由 Manager 读取确定性数据；同参重试不重发、续期或复活撤销。Deny 优先、解除及撤销继续复用 System 的同一路径，旁观 User 始终不能读取。所有写入编号在发送前登记；失败、中断也按本轮两个目标清理规则，并随 Hosted 生命周期销毁账号及目录事实。此阶段补充业务批准闭环，不把只读签发历史当作实际访问证据。

Catalog 可选依赖的故障验收复用上述 PG 业务 Grant、同一消费凭据及单一 Hosted 故障协调器：标准停止 Catalog 后，Manager 仍能凭原 Grant 读取，System 签发历史和精确目标的业务批准要求不变。新的人类办理请求须明确返回模块／代理不可用；对无授权旁观账号尝试直接批准须因批准方式不符而拒绝，不能由失联自动降级或补权。Catalog 离线期间仍可建立 Deny、解除并撤销原 Grant，实际预览逐步验证拒绝、恢复与最终拒绝；恢复 Catalog 后同参重放原已签发请求不得复活撤销，新失败请求不得产生受理或签发。停止／恢复均核验真实监听、同构建新进程及其他 owner 未重启，失败仍恢复并按预登记编号清理。不重扫、不切换批准方式、不新增角色；不将未受理的新请求与停机前已受理、可沿原窗口续办的请求混淆。

PG 显式批准方式切换沿用同一 suite，只为隔离办理夹具增加独立更新 Permission，不改变消费及确认权限。对业务 Grant 生效的单表验证 `catalog v1 → independent v2 → catalog v3`：无更新权限不能切换；切换不改既有 Grant、期限和已受理原请求，也不自动给旁观账号补权。旧版本的新直接批准及旧版本切换须明确冲突；新版本直接批准可显式授予旁观账号该表读取权，返回业务批准后该 Grant 仍有效，但新的直接批准被拒绝。同模式更新不推进版本，已签发原命令只恢复原历史；该账号的临时 Grant 撤销后再次拒绝读取，因此“旁观账号始终拒绝”仅适用于未显式获权的阶段。新 Catalog 办理若携带旧版本，可按既有提交前持久化协议先返回 pending，但不得受理、签发或自动替换版本；后台核清须关闭原编号，同参恢复仍为 closed。全过程使用真实 Manager 预览及 System 历史，不重扫、不改本地账号，失败仍回收本轮全部来源。

平台公共入口为：

```bash
# T0-T1：全仓无外部服务确定性门禁
make test

# 当前工作区受影响模块的 T0-T3 门禁
make test-changed
make test-changed BASE_REF=<ref>

# 指定 owner 模块的 T0-T3 门禁
make test-module MODULE=<module>

# 仅运行 owner 的 Go T1，便于开发定位；不计为完整模块门禁
make test-module MODULE=<module> GO_ONLY=1

# 全部已登记 disposable 基础设施门禁
make test-integration

# 仅在持有合法 License 的 owner 受控 Linux 主机人工运行，不进入 GitHub Actions
make test-integration-owner-managed

# 本地与 CI 共用的发布工作流安全审计（首次执行需下载固定版本工具）
make test-workflow-security

# 一个已登记的跨模块 Online suite
make test-online ONLINE_SUITE=<suite>

# 一个已登记的产品发布 suite
make test-release RELEASE_SUITE=<suite>
```

约束如下：

- `make test-changed` 与 CI 共用 owner 影响计算；共享代码改动按真实依赖扩散到已登记消费者。
- `make test-module` 自动发现模块的 Go、Python、CMake/C++、前端与基础设施门禁，不维护第二份模块清单。
- `make test-integration` 严格串行调用 owner 的 T2 事实入口，不复制测试逻辑。
- `test-online` 与 `test-release` 必须显式选择已实现的 suite；未实现能力不得以占位 suite 登记。
- 不提供跨 T0-T5 的 `test-all`。T4 与 T5 的身份、基础设施和安全前置条件不同，完整认证由 CI 分别编排标准入口并分别报告。
- 开发者和 AI 不得自行拼接一组语言命令替代上述标准入口；模块内部命令仅用于 owner 开发与标准入口实现。

## 四、T0-T3 确定性与基础设施边界

### 4.1 T0-T1

SuperMap Workflow 的协议、算子目录及资源主机测试使用根 `make test-supermap-workflow`，通过临时构建目录运行不加载厂商 SDK 的 CTest，依赖 CMake、C++20 编译器和 nlohmann-json 开发包。CMake owner 必须同时登记根 `make test` 依赖及 Platform CI 调用，由构建登记检查和模块门禁核对。真实 SGM 读取、OSGB 导出和发布边界仍由受控 SDK 镜像构建阶段的 `sgm-conversion-test` 验证；公开协议门禁不代表 SDK 转换或 T4 已通过。

T0-T1 必须：

- 不依赖已经启动的 ADDP 服务。Infra 生命周期单测必须同时隔离容器状态与宿主机监听查询，使用夹具内的端口事实；真实端口探测由专门的端口解析回归覆盖，不能让 mock Docker 与真实 lsof 混合决定启动行为。观测平台夹具必须清除父进程的个人观测选择、部署路径和传输配置，再应用用例显式输入。
- 不连接开发业务数据库。
- Online 浏览器源码的语法门禁必须按所属模块的 ESM 契约显式校验，使用 Node --check --input-type=module 从 stdin 读取源码；不得把不同 Node 小版本的文件模式检查返回 0 等同于完整回调语法有效。根 .node-version 是 Node 主版本唯一事实源，运行前按二进制实际版本核对，不根据安装目录或别名推断。非法语法必须在所属 runner 的后续确定性测试与夹具编译之前拒绝。
- 可重复执行，失败后不留下外部状态。
- 需要 Online 条件的测试默认不进入普通语言测试；对应专用门禁显式开启并拒绝意外 Skip。
- 模块门禁执行平台 T0 和 Go T1 时必须移除 `*_POSTGRES_TEST_DSN`、`ADDP_*_INTEGRATION` 等 T2 显式开关；只在对应 T2 步骤保留调用方提供的条件，避免平台内嵌单元测试绕过 owner 锁并清理共享测试库。
- 模块生命周期公共契约必须在 T1 覆盖 `starting → registered`、`registered → recovering → registered`、确定性拒绝进入 `failed`、取消后 `stopped` 与限时注销。
- Backend 路由测试必须证明 `/health/live` 不触发外部调用，`/health/ready` 只在自身必需 Infra 和 System 注册都就绪时返回 200，业务路由在 Not Ready 时返回 `503 module_not_ready`。
- Worker/Scheduler 测试必须证明未注册或 `recovering` 时不领取新工作，恢复后无需重启进程即继续领取。

Model3D 原生安装的下载完整性、私有资源隔离和损坏缓存拒绝由现有 `make test-model3d-workflow` 自动发现；开发生命周期契约纳入 `make test-dev-lifecycle`。Linux 产品与 macOS 原生共用 IfcConvert 版本清单及 Cargo.lock。Manager Hosted T4 启动正式 Model3D Runtime 镜像并验证镜像身份和默认入口，退出按当次标签删除容器，不调用开发 wrapper；对应编排和登记检查沿用既有 Online suite。原生真实转换预检属于开发准备，不代替上述正式门禁。Manager Hosted T4 的模型覆盖包括 DAE、3DS、IFC、单体 OSGB 与 SKP。SKP 使用自有、带内嵌彩色 PNG 的英寸三角形和两个组件实例；Meta 只登记格式身份，Runtime 生成后核对米制尺寸、实例位移、内嵌贴图、缓存预览、血缘和领域清理。Model3D 标准产品构建必须在最终 Runtime 镜像中，通过正式 operator 转换 Business 自有压缩 OSGB 夹具并验证 GLB；builder 阶段的原生转换测试与最终 Runtime 的 `--help` 检查均不能替代该验证。

### 4.2 T2

Spark PostgreSQL 发布复用 `make test-common-hdfs` 中已有的独占 PostGIS 和真实 Spark Worker，验证已有目标与新目标提交成功但响应丢失、提交前失败的回滚及清理、回滚未确认时保留证据，以及提交成功后关闭连接或暂存清理失败不改写已提交事实。故障注入只包裹正式 JDBC 控制连接，实际提交、数据核对与暂存检查仍由原生 PostgreSQL 和独立连接完成；T1 同时验证原始错误不被回滚、查询资源关闭或连接关闭错误遮盖。沿用现有 owner、CI 输入登记和零残留生命周期，不连接个人 Business 实例。

Spark MySQL/Doris 暂存发布由原生 Linux x86_64 Hosted 门禁 `make test-common-spark-table` 验证；独占 Doris 通过 Business 唯一入口验证新建集群和重复登记，核对 Backend ID 不变、FE/BE 就绪后完成官方连接器读写；历史 IP 地址迁移及反例由 `make test-business-config` 的确定性夹具覆盖，本机已有 Backend 的真实迁移另行验收，不计为 Hosted 迁移覆盖；macOS 的显式 MySQL 子集不计为完整 T2 通过。门禁拥有独占 MySQL 8.0、Doris 2.1.0、Spark Master/Worker，固定官方镜像摘要，无宿主端口、不连接 Business，失败与中断均删除本轮容器、卷和网络并核对零残留。断言覆盖真实 Worker、覆盖/追加/空成果、目标定义保留、字段与约束拒绝、计算失败、bigint/Decimal 精确读回、Doris SQL 生成 Decimal 的 JSON 写入精度和官方连接器读写回路中的中文、emoji、空串、null、合法尾随空格及长文本，以及 MySQL 提交响应丢失后保留原提交和暂存证据；不把 T1 mock 或现有 Go Provider 门禁当作 Spark 写入验证。变更选择通过声明的 Runtime 及夹具输入归 Common T2，Release/T2 CI 调用同一入口；此 T2 不证明 Develop/Console 端到端验收已完成。

Document 原生转换使用独立入口 `make test-document-native`，在已准备的私有 LibreOffice 和固定中文字体上，经正式 `document_to_pdf` 算子转换三页 PPTX，核对页数、中文文本、工作目录清理及源样例不变。缺失或损坏的依赖直接失败，不跳过、不隐式安装。测试使用临时输入、输出和 profile，不启动 ADDP 服务；由 Platform CI 的 Document Workflow Job 准备依赖后调用。普通 start/restart 只负责依赖与服务就绪，不执行该功能测试。HTTP／算子确定性测试继续使用 `make test-document-workflow`，跨模块产物生成与浏览器预览继续由 Manager T4 验证，三者分别报告。

T2 使用真实但可丢弃的基础设施，并满足：

- CI Job 使用独占 Service 和随 Job 销毁的数据库。
- 托管外部服务的 owner gate 必须在脚本头声明 `# ADDP_T2_SERVICES=<service,...>`；模块门禁、CI 注册和后续新增数据库类型都消费该声明，不按数据库名称维护发现分支。
- owner 自建的临时服务通过 `ADDP_T2_OWNED_SERVICES` 和 `ADDP_T2_COMPOSE_FILE` 登记，必须拥有独立 Compose 项目、回环随机端口以及退出清理和零残留检查。镜像使用固定 tag 与 digest；需要沿用仓库源码构建时，允许引用仓库内的单一 Dockerfile，但外部基础镜像须固定 tag 与 digest，Git 源码须在同一构建步骤中核对固定提交，构建上下文须位于仓库内，Dockerfile 与本地复制输入须登记到 `ADDP_T2_INPUT_FILES`；目录声明以 `/` 结尾，递归覆盖该仓库子树的新增、修改和删除，构建检查与变更选择共用同一覆盖判定。登记检查拒绝仓库外路径、构建参数覆盖和未固定源码；门禁必须实际构建，不接受开发环境已有同名镜像作为构建证据。
- 必须由调用方注入连接条件的 owner gate 同时声明 `# ADDP_T2_REQUIRED_ENV=<name[|alternative],...>`，逗号表示“同时需要”，竖线表示等价的安全前置条件。`make test-module` 与 `make test-changed` 必须在执行任何 T0/T1 前一次性检查全部所需条件，缺失时失败关闭并给出 owner、变量及安全测试环境提示；`--dry-run` 仅展示计划，不要求真实连接条件。CI 登记检查必须确认对应 Job 显式提供每组条件中的至少一个变量。
- 需要 owner 持有合法 License 或受控介质的门禁必须声明 `# ADDP_T2_OWNER_MANAGED=<runtime>`，只通过 owner 受控 Linux 主机上的 `make test-integration-owner-managed` 人工执行，不进入 GitHub Actions、普通 `make test-integration` 或 macOS 定时巡检。登记检查必须拒绝 workflow 调用这类目标及其聚合入口。脚本必须验证官方介质与 License SHA-256、拥有 disposable 容器全生命周期并验证零残留。
- 本地共享 `addp-postgres` 只允许使用 `addp_test` 与 `addp_iam_test`，并且只能通过根 `Makefile` 或 `scripts/test/` 的标准入口操作。
- 本地 PostgreSQL T2 门禁必须由调用方显式提供测试 DSN 或端口；不得在未提供时回退到 `.env` 的首选端口。调用方先用 `bash scripts/infra/status.sh` 核实 ADDP Compose 实际映射。标准 PostgreSQL 门禁在执行测试或清理前，必须核对本地回环连接的端口与当前工作区 `addp-postgres` 的实际 Compose 映射一致，且 database 仅为 `addp_test` 或 `addp_iam_test`；容器不存在、归属不符或映射不一致时直接失败，不根据 `.env` 猜测。门禁前置检查不得输出一个写死首选端口的可复制连接命令。GitHub Actions 的独占 Service 与辅助 macOS CI 的独占临时 PostgreSQL 使用各自入口显式配置的端口和 database，不套用本地共享 Infra 校验。
- 禁止为单次验证直接创建或删除数据库；现有标准入口不能满足隔离时，先完善入口及自动清理。
- 门禁在任何破坏性动作前校验数据库身份，拒绝开发库、生产库或不满足 owner 安全约束的连接。
- 每个场景只清理自己拥有的 Schema 或带唯一运行标识的事实，并验证零残留。
- 门禁拒绝意外 Skip，避免“命令成功但测试未运行”。
- Common PostgreSQL 的默认 `make test-common-postgres` 保持完整门禁，同时覆盖表写入准备、安全列演进和空间结构拒绝。真实低权限账号必须在没有数据库级 CREATE 权限时完成已有 schema 下的建表与 upsert 准备；缺失 schema 仍须按数据库权限拒绝或创建，测试结束清理本轮 schema、账号及授权并验证零残留。仅验证查询读取集合、输出血缘及查询会话值表示时，可在同样的显式测试连接条件下运行 `bash scripts/test/common-postgres-gate.sh --test query-read-set`；该分组包含真实资源改名及同名重建的定位身份案例，以及 BYTEA 的 NULL／空值／非 UTF-8／别名／表达式／跨批次保真和文本、精确 numeric、JSON 表示回归，保留数据库身份检查、Skip 拒绝和场景清理。分组结果只证明所选范围，不替代完整 T2 门禁；CI 继续使用无分组选项的默认入口。
- HighGo 写入沿用 Common PostgreSQL 标准入口：默认完整门禁及 `bash scripts/test/common-postgres-gate.sh --test highgo-write` 在 disposable PostgreSQL 上回归独立 HighGo 驱动和共享写入协议，由既有 Hosted Common PostgreSQL Job 覆盖。`--test highgo-write --highgo-native` 为显式原生认证，必须注入全部 `ADDP_TEST_HIGHGO_*` 连接字段，强制 TLS 并先核验 V4.5；只在本次成功创建的随机 schema 内操作合成数据，不创建／删除 database。提交、Abort、失败批次原子回滚与类型往返结束后逐一清理已知测试表，以 RESTRICT 删除自有 schema 并验证零残留；失败不自动重试写入。协议回归与原生认证分别报告，不互相替代。
- Manager 派生任务定义、语义唯一约束、资源绑定与资源回收的持久化契约由 `make test-manager-postgres` 在真实 PostgreSQL 中覆盖；同一门禁的 Service 用例依赖 PostGIS，验证生成目标的 3857 类型声明、2D／Z／M／ZM 保留、空目标校验、未声明旧目标拒绝及无内容 SELECT 权限的目录核验，清理自建 Schema 与事务内角色并核对零残留。该门禁只允许本地 `addp_test` 或 CI 随 Job 销毁的 disposable database；CI 复用固定摘要的 PostGIS 15-3.4 镜像。同一门禁使用独占 Meilisearch 验证技术与正文快照任意顺序并存、重复提交、空值清除及单指纹删除；本地标准脚本自动创建临时容器并在退出时删除，CI 使用随 Job 销毁的固定摘要 Service，禁止连接开发内容索引。
- Orchestrator 的独立进程故障回归归入既有 `make test-orchestrator-postgres`：测试进程运行正式监督器，在提交与等待阶段分别强制终止持有租约的运行者，再由新进程验证过期失败收敛、不重发请求、旧租约写入拒绝、子执行引用保留及唯一终态事件。HTTP Provider 是受控夹具，因此不作为 T4 真实拓扑验收。Go 测试自动发现和现有 Hosted PostgreSQL Job 覆盖同一入口，无新增基础设施依赖；子进程和当次数据库事实必须在成功、失败路径清理并核对残留。

Doris／ClickHouse 会话失败回归分别沿用 `make test-common-doris-decimal`、`make test-common-clickhouse-decimal` 和现有 Release/T2 Job；T1 使用同一断言矩阵验证会话状态和参数绑定，不能代替原生 T2。每个引擎必须覆盖非法 JSON、后续 chunk 执行失败、写入取消、关闭取消、Abort、正常关闭和空会话七个场景，核对首次错误保留、失败后拒绝续写（含空批次）、重复关闭及提交标记。原生 T2 须从独立连接核对实际数据：先成功写入一行，再让下一批的前 1000 行提交、后一个 chunk 执行失败，必须保留已提交的 1001 行，同时会话失败且无成功标记；此前提交行的 JSON、bytes 与普通文本须保持原值。每个合成表先确认名称未占用，退出逐表清理并验证零残留；门禁独占固定摘要容器、随机回环端口及全部退出路径的容器／卷清理，不连接业务实例、不运行已保存的 Transfer 任务或触发扫描。ClickHouse 就绪检查必须等待镜像初始化结束后的正式服务，不能以初始化临时服务响应成功作为就绪证明。

Transfer 的 `transfer-relational-sql-etl` Hosted T4 在同一普通 User／Tenant 下追加 Doris、ClickHouse 各两个合成失败场景，沿用现有 suite、生命周期、浏览器和 CI 入口，不复用个人开发部署。独占目标使用固定摘要镜像和随机回环端口，Doris 复用 Business 唯一 FE／BE 启动入口，在夹具实例开启严格写入并限制目标分区范围；ClickHouse 使用 CHECK 约束。1001 行源输入按显式顺序进入同一批次，先提交 1000 行，再因原生约束拒绝末行；另以末行非法 JSON 验证本批无新增行。两者均保留预置哨兵行，独立物理读取分别必须得到 1001 行与 1 行，不能用 execution 的 `records_written=0` 推断目标为空。每个任务必须为 failed、使用不同执行 UUID 且关闭自动扫描，目标 Meta 事实前后保持未扫描；同一登录身份在真实 Transfer 执行详情页核对安全诊断、终态和执行编号，并生成截图，禁止恢复原始 error_msg 或暴露运行时正文。提交标记仍由原生 T2 验证，专业 API 不为验收暴露私有 checkpoint。任务按当次所有权清理，容器／卷在成功、失败及中断路径销毁并核验零残留。runner 通过不计为 T4 通过，首次真实 Hosted 成功须另记证据。

Oracle 的既有 `make test-common-oracle-decimal` 默认使用 Slim 一次性容器验证非空间类型写入及删除；`make test-common-oracle-decimal ORACLE_SPATIAL=1` 使用固定摘要的完整镜像，额外验证原生 SDO_GEOMETRY、空间索引、DROP 后的登记状态、存在／缺失表的精确登记清理、重复删除及其他表登记保留。两种配置均由既有 Hosted Oracle Job 执行，容器只绑定随机回环端口，退出销毁容器及卷；不得连接业务 Oracle 或以模拟视图代替原生空间认证。

### 4.3 T3

T3 的 PR 主路径使用独立端口、受控 API 夹具和非个人登录态。真实 System、Gateway、owner Backend、真实身份与数据源的浏览器链路归入 T4，不与确定性浏览器测试混跑。

租户执行过程事件的真实 PostgreSQL 事务、并发限额、租约过期、Owner 范围分页和清理锁竞争由 `make test-common-postgres` 的 `TestExecutionEventsAgainstPostgres` 覆盖；Transfer 历史自由文本移除由 `make test-transfer-postgres` 的 `TestIntegrationPostgresExecutionEventsRemoveRetiredText` 覆盖；同一入口登记 continuous 事件、Runtime fencing／取消、自动恢复、恢复退避／熔断和结构阻塞组，验证真实行锁等待后过期的进展／终态／续租拒绝、错配租户／执行／模块拒绝、事件与状态回滚、每日并发预算、过期运行收敛及事件清理保留概览。Common 同一事件组验证 continuous attempt 迁移和 Owner 裁决预算，未开始的取消不生成过程事件。两者均已登记标准入口，Common 的全量门禁包含该执行事件组。共享运行过程组件的唯一所有权由 `make test-common-frontend` 自动发现，Monitor 的事件分页、截断及撤权清除回归由 `make test-monitor-frontend` 的确定性 Playwright 覆盖。上述测试不替代真实 User、Gateway、Owner 与 Worker 的 Online T4，也不代表个人环境已部署新的迁移和保留维护循环。

确定性 Playwright 统一通过 `scripts/test/frontend-browser-gate.mjs` 启动，模块 `package.json` 的 `addpBrowserTest.fixtures` 只声明夹具 owner、可选 base 和就绪路径。启动器每轮分配回环临时端口，并保留端口租约直到退出，避免同机门禁在探测与 Vite 绑定之间选择同一端口；实际绑定仍使用 `--strictPort`，外部抢占时明确失败，不复用其他服务。各 Playwright 配置使用 `common-frontend/basic/src/utils/browserTestIsolation.mjs` 的 `withBrowserTestIsolation`，由共享实现统一生成 CLI、代理环境、baseURL、就绪地址和独立结果目录；断言与跨模块夹具通过 `browserTestOrigin` 获取当轮地址，不保留固定端口路径。每个 Vite `webServer` 设置 `ADDP_E2E=1`、回环 host、`reuseExistingServer: false` 和 `gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 }`。Vite 继续使用共享 `withFrontendTestIsolation` 关闭 HMR，缓存与 owned PID 记录置于当轮运行目录，清理前核验当轮进程标识，旧 PID 记录不得授权停止无关进程；成功、失败及 SIGINT/SIGTERM 路径清理当轮进程、缓存、运行目录和端口租约，成功删除结果目录，失败保留独立截图、trace 与非敏感运行清单。CI 从 `${runner.temp}/addp-<owner>-playwright-results-*/` 收集当轮失败证据。登记检查在 `make test-platform` 中拒绝旧启动路径、模块自行覆盖隔离字段、缺失共享 Vite 隔离及跨模块夹具依赖遗漏；共享库单测覆盖并发、失败和中断清理。Online T4 的专用部署配置不套用此确定性夹具规则。

浏览器测试重点证明布局、路由、权限反馈、状态恢复、关键交互和响应式行为，不重复后端已经覆盖的全部字段或业务规则。

前端矩阵 Job 按 `playwright: true` 声明统一上传失败输出目录，产物按模块、工作流运行与 attempt 区分。运行浏览器门禁的步骤通过 `TMPDIR` 对齐测试临时目录与上传步骤使用的 `runner.temp`。System 浏览器门禁保留失败时的 trace、截图与页面上下文；角色分配用例分阶段确认角色选择、下拉菜单关闭、授权原因和提交条件，避免只在最后的按钮点击处报告超时。

## 五、T4 Online 验收协议

瀚高 `highgo-consumer-flow` 使用手动 GitHub Hosted Linux x86_64 disposable profile。Business 从既有固定摘要社区镜像和唯一 HighGo 初始化入口建立当次普通业务账号及客户／商品／订单／明细样例，另建当次 PostgreSQL 源；不读取个人 `.env` 或连接已有业务实例。System 夹具提供独立登记身份、来源授权办理身份和最小权限消费 User，正式 API 完成两引擎登记及 PostgreSQL 源表的精确读取授权；瀚高沿用已有消费路径，不将本验收解释为瀚高独立源授权认证。验收深度扫描四表，核对 11 个具名约束、10 个索引、键顺序及普通表无分区事实，通过正式 Manager API 和 Console 预览验证，再将 PostgreSQL 订单两次覆盖到瀚高专用哨兵表，核对七字段、金额和无重复写入。退出销毁当次平台、业务容器、卷及身份并验证零残留；证据不保存凭据。确定性反例与生命周期清理由 `make test-highgo-online-runner` 负责，并接入 Online/Platform 登记检查；首次真实 Hosted 通过前不登记夜间执行。此范围不包含增量 upsert、CDC、空间能力或分区表页面认证。

HDFS 与 Spark 的 `hdfs-spark-consumer-flow` 使用手动 GitHub Hosted Linux x86_64 disposable profile。入口 `scripts/test/online-hosted-hdfs-gate.sh` 复用共享 Hosted 生命周期；Business 只管理当次 Hadoop、Spark Master/Thrift、Worker、PostgreSQL、MinIO 与样例，System owner helper 管最小权限普通 User 及独立登记身份。HDFS 存储 Engine 与 Spark general Engine 分别通过 System API 创建并连接测试，Spark Workflow 经共享 Hosted 产品入口 `scripts/utils/hosted-online.sh` 调用根 `make build-images` 构建镜像并使用镜像默认入口启动，禁止覆盖 command 或启动原生 Spark Workflow 服务。验收记录镜像 ID、默认入口、当前提交及运行容器匹配，检查自动注册与正式执行后同一 Runtime execution 的状态。Runtime 与临时镜像仓库由验收入口持有独立所有权标签，Business 残留检查只覆盖 Hadoop/Spark/PostgreSQL/MinIO 业务容器。Linux 夹具统一使用宿主网络及回环消费地址，Worker 必须真实执行分布式任务；不得以 `local[*]` 或直接 Runtime 调用代替 Develop 正式执行。普通 User 经 Meta 深度扫描、Manager CSV/JSON/Parquet 与中文百分号根文件预览，以资源 locator 创建八节点工作流，验证每格式 20 行、合计 2100，以及 region 聚合一致。API、Console 的真实登录身份和执行结果必须一致；Master Application 与该 Application 的 Worker 已完成任务日志必须关联。退出停止应用，保存 Runtime 日志，并销毁当次 Runtime 容器及镜像、业务容器、Infra 及仓库外凭据，核对零残留。确定性脚本、身份与 CI 登记通过 `make test-hdfs-online-runner`、`make test-online-runner` 和平台 T0，真实验收通过 `make test-online ONLINE_SUITE=hdfs-spark-consumer-flow`。首次真实 Hosted 通过前不计为 T4 通过，也不增加定时调度。

MinIO Parquet 发布复用 `make test-common-hdfs` 与手动 Hosted `hdfs-spark-consumer-flow`，不新增平行门禁。T2 覆盖 Magic Committer、提交后 footer 行数、空成果、同名 bucket 的 endpoint/凭据隔离、失败目录清理、按 pending 记录精确取消已知 upload ID、兄弟上传保留，以及记录缺失／损坏时保留证据并报告清理未核验；目录前缀 multipart 空列表不能作为清理证据。T4 连续发布两个独立成果目录，核对 Meta `table/parquet/whole`、自动扫描、精确血缘、Manager 预览、下游 Spark 读取和前次成果不变；业务 MinIO 使用当次 owner-only 凭据并随夹具销毁。T1/T2 通过不替代当前提交的真实 T4。

### 5.1 专用环境

企业目录发布的 `enterprise-catalog-publishing` 沿用专用 macOS profile：正式 User 发起共享受理后，生产 Catalog 通过自己的 OAuth 服务身份自动签发。验收脚本不借用该身份，System owner 的现有 Online 测试夹具仅以只读可重复读事务核对精确 Tenant、Engine、请求编号的受理、Grant 和唯一签发成功审计；原参重试后再次核对签发时间与审计次数不变。该观察模式只允许回环 `addp_online`，禁止初始化身份、执行迁移或写入，不是人类 API、Catalog Grant 副本或访问裁决。确定性协议与准入测试归 `make test-online-runner`，真实 PostgreSQL 查询及只读拒写归现有 `make test-system-iam-postgres`，完整 OAuth 链路仍须在现有手工 T4 suite 真实执行后才能计为通过。

T4 只在隔离的 ADDP 测试部署执行，并按运行条件选择唯一部署 profile：

- 常规 Online suite 使用带 `self-hosted`、`macOS`、`addp-online` 标签的专用 Runner 和 `addp-online` GitHub Environment，复用专用部署中的稳定 Tenant、User 和 Engine Instance。
- 需要商业 License 的 Linux 引擎使用带 `self-hosted`、`Linux`、产品声明的 `X64` 或 `ARM64` 架构标签和 owner 标签的专用 Runner，通过受保护 GitHub Environment 审批。当前 KingbaseES 正式门禁仍固定 `X64`；DM8 ARM64 若进入正式门禁必须固定 `ARM64` 且匹配厂商认证环境。License 和受控介质不进入 checkout、日志或 Artifact，也不回退到 Hosted 或 macOS profile。
- DM8 在正式 ARM64 主机到位前允许本机 macOS ARM64 Docker Desktop 执行试用期技术 T2/T4：数据库以及所有真正加载官方 Go 驱动的测试/模块进程必须运行于 `linux/arm64` 容器，System 可保持 macOS 开发模式仅承担 Engine Instance 控制面登记。该 profile 是非认证 ABI、非长期且不可调度的本地证据，不登记 GitHub Hosted 或 schedule；试用到期、Docker 架构不符、驱动摘要不符或官方介质不可得时必须失败，不能 Skip 或切换镜像/驱动。
- SGM 允许通过 `make test-manager-sgm-local` 人工执行本机隔离技术页面验收，限定 macOS/Linux ARM64 与本地 Linux ARM64 Docker。入口从当前已提交源码建立仓库外临时副本，复用正式 Compose 服务定义和产品构建入口，使用本轮独占平台、双 Runtime、PostgreSQL、Redis、Infra/Business MinIO 与普通测试 User；不连接或停止个人开发服务。容器、网络、卷、凭据及入口端口必须独立，退出逐项核验清理。Runtime 和对象存储只使用内部数据网络，Nginx、System、Gateway 与 PostgreSQL 另接本轮独立入口网络，宿主机端口仅绑定回环；正式构建前须通过实际端口映射及 HTTP 准入检查。SDK、许可和厂商样例只在私有本机存在；报告仅保留哈希与断言摘要，不归档原始日志、截图或样例。System 身份夹具仅允许该本机标记用于既有 Manager 十项权限夹具，且与 GitHub Hosted/owner-managed 标记互斥；它不授予个人或生产账号权限。该入口验证任务生成、GLB 页面加载、缓存复用、原文件保持及领域/物理清理，不计为正式 Linux Runner T4、不进入 GitHub Actions 或 schedule，也不自动推广为其他商业引擎的部署例外。确定性生命周期与准入回归纳入既有 `make test-manager-online-runner`，平台登记检查必须拒绝公共 workflow 调用真实私有入口。正式商业许可 T4 继续遵循上一条 Linux Runner 要求。
- 明确声明 Linux/CPU 限制，或能够从零建立完整确定性夹具且无需永久测试身份的 suite，可登记 GitHub Hosted profile。该 profile 每轮必须在干净 `ubuntu-24.04` x86_64 Runner 上从零启动 disposable Infra、Tenant、User、Engine Instance 和业务引擎，退出时全部销毁；不使用 GitHub Environment 或仓库 Secret。
- self-hosted Runner 使用独立账号和独立 checkout；Hosted Runner 使用 Actions 当次临时 checkout。两者都不复用个人开发工作区或开发服务进程。
- 公开仓库的 self-hosted Runner 必须是可独立重置的专用测试设备，不得登录个人 Apple ID、保存个人 SSH Key、浏览器会话或访问个人与生产网络。只允许受保护 GitHub Environment 的 `workflow_dispatch` 和已毕业 suite 的固定 `schedule` 调度；`pull_request`、`pull_request_target`、`push`、Issue 事件或可由外部输入改写的动态 workflow 不得选择该 Runner。
- self-hosted Online Job 的 `GITHUB_TOKEN` 只授予 `contents: read`，第三方 Action 必须固定不可变 commit SHA，`addp-online` Environment 必须在 Job 接单前执行必要的人工批准。Runner 不接收仓库 Secret 中的长期业务凭据；永久测试部署的 Secret 只存在专用设备上的 owner-only 仓库外环境文件和 owner 加密存储中。
- 服务只绑定 Runner 可访问的回环地址；通用预检拒绝外部服务地址。
- 仓库根不得保存 T4 `.env`。self-hosted profile 的 Tenant、数据库连接和凭据由仓库外绝对路径环境文件注入；Hosted profile 只能使用当次 disposable 部署产生、位于 `runner.temp` 的 owner-only 凭据文件。
- `ADDP_ONLINE_HOST` 必须精确为 `1`；Hosted profile 还必须同时校验 `GITHUB_ACTIONS=true`、`RUNNER_OS=Linux` 和 `ADDP_ONLINE_HOSTED=1`。生命周期门禁必须在任何停止、启动或重启操作前完成只读准入检查。
- License 受控 owner-managed profile 必须同时校验 `GITHUB_ACTIONS=true`、`RUNNER_OS=Linux`、`ADDP_ONLINE_OWNER_MANAGED=1`、外部 owner-only 环境文件权限、官方介质 SHA-256 与 License SHA-256；Hosted 与 owner-managed 标记必须互斥。
- `POSTGRES_DB` 必须精确为 `addp_online`，并拒绝 `addp`、`addp_test`、`addp_iam_test`。该数据库只属于当前 T4 部署，不属于本地共享 PostgreSQL 测试清单。
- 证据目录必须位于仓库外；凭据目录不得被 artifact 归档。工作区必须干净，构建身份必须与当前 checkout 一致。

三种 profile 共用同一个 `make test-online` 分发器和 owner 业务断言，生命周期脚本只负责当次环境和夹具。编排只调用现有 Infra、开发生命周期脚本和 `make test-online`，不得在 workflow 中复制模块启动逻辑或业务断言。Hosted 原生 Linux 经开发入口启动的 PointCloud、Document 容器使用宿主网络，直接以配置端口监听，通过回环地址访问 System、Infra 与 Business MinIO；不得用 bridge 的 host gateway 访问仅发布回环端口的服务。该启动契约由 `make test-dev-lifecycle` 覆盖，真实对象存储读写仍由 Manager T4 验证。退出路径必须停止本次应用进程并报告清理结果；常规 macOS self-hosted Infra 可在专用 Runner 常驻，Hosted 与 License 受控 owner-managed profile 的 Infra 必须随 Job 销毁。

Security 四出口 T4 `security-mysql-owner-protection` 只使用 GitHub Hosted Linux x86_64 disposable profile，入口为 `scripts/test/online-hosted-security-gate.sh`，不再由 macOS self-hosted 分发。每轮创建独立 PostgreSQL/PostGIS 和 MySQL 业务容器，复用共享 Infra 与标准开发生命周期启动真实 System、Gateway、Meta、Security、Manager、Develop、Service、Transfer。Hosted 准备按 `make test-go` 的同一已跟踪模块发现规则，在仓库外当次凭据目录建立显式 Go workspace，并复制已跟踪的 `go.work.sum`，供启动、Swagger 生成和重启使用；上述步骤统一使用标准启动的仓库 `.gomodcache`，避免重复下载的正常日志被 Swagger 工具判为解析失败。不依赖未提交的本地 `go.work`，临时 workspace 退出随凭据目录删除。System owner helper 使用正式 IAM Service 创建当次 Tenant 和最小权限非管理员验收用户；独立引擎登记及治理初始化用户的令牌只用于准备阶段，初始化通过正式 API 完成。MySQL 邮箱移除、PostgreSQL 空间表三类普通属性算法和 Manager 字段独立配置沿用原有 owner 断言。所有退出路径停止应用、销毁业务容器及 Infra，并核对零残留和删除仓库外凭据。确定性回归进入 `make test-online-runner` 与平台 T0；当前保留手工调度，真实通过证据见下文。

同一 Security T4 同时验证保护与技术检索：每次空间表算法／字段策略更新收敛后，在下一次 Meta 扫描之前，经 Gateway 用同一普通用户和指定 Engine 检索受保护字段，核对同一 fingerprint、定位、字段类型和实际高亮命中；掩码、常量、SM3 的预览值仍按独立预期验证。三个 Manager 字段分别采用上述算法且策略仍有效时，必须通过标准生命周期重启 disposable Hosted 部署的 Security 和 Manager；重启前后 Ready 的实例身份必须变化、启动时间必须前进，构建身份保持一致。同一普通 User、Tenant、Baseline 和字段 Policy 必须保持一致，并在任何再次扫描或策略写入之前重新验证同一 DataItem 的技术字段检索、三种脱敏值、空值和空间坐标。未通过 Hosted 准入的入口不得执行重启；退出仍由既有 Hosted 生命周期销毁全部资源。MySQL 邮箱移除后，真实 Console 必须先确认预览不含邮箱字段，再通过数据检索页面找到同一 DataItem 的邮箱字段定义，并从检索结果定位回原预览完成导出与 Monitor 验收。报告及截图缺少任一步均失败，不以再次扫描修复索引、Mock 响应或管理员代验收替代。重启恢复覆盖已在 `8032cb7a3` 的 [Security Hosted T4 38041734925](https://github.com/pampa0629/addp/actions/runs/38041734925) 首次真实通过：v4 报告确认 Security、Manager 的实例 ID 和启动时间变化、构建身份不变，同一普通 User/Tenant 与 Baseline/Policy 保持一致；未重扫前的字段检索和 5 条数据的三算法预览通过，空值及坐标保持正确。原有四 Owner 和 Console 检索→定位→预览→CSV→Monitor 链路同时通过，临时策略、资源、容器及 Infra 零残留。浏览器验收前使用同一普通 User 有界等待 `manager_search_isolated` 解除，并要求准确命中受保护字段和同一 DataItem；规则变更及临时资源清理的索引任务未收敛、超时或其他服务错误仍判失败。共享 Hosted 入口接入原值访问后，在提交 `ee5913d75` 的 [2026-10-10 四出口回归 run 38059560352](https://github.com/pampa0629/addp/actions/runs/38059560352) 再次真实通过：v4 四出口、三算法、独立字段、重启恢复和 v3 Console 检索／预览／CSV／Monitor 证据完整；4 个临时资源全部删除，临时策略、业务容器与 Infra 零残留。此证据不声明全平台重启、已打开浏览器状态恢复或不确定外部写入故障已通过。

MongoDB 嵌套字段验收继续使用同一 Hosted Security T4 和生命周期，增加当次只读 MongoDB `Outdoor.Persons` 夹具及精确 `database/collection` Source Grant。正式 `phone_metadata/v2` Detector 必须从 Meta 识别 `userInfo.phone`，经 Finding 确认后读取正式 Assessment 当前修订，核对其冻结的字符串类型、嵌套 object/scalar 路径和结构指纹；不能从仅列出未评估字段的人工候选组件接口重新寻找已确认字段。Assessment 和四 Owner active ack 后，验收切换默认算法时必须同步遵守 Baseline 许可约束：只有前后掩码默认算法可以许可前后掩码，固定值替换和 SM3 默认算法仅许可固定值替换与 SM3，不得提交缺少字符保留上限的前后掩码许可。Manager 依次验证掩码、固定值替换、SM3 的独立预期，包括 Unicode、短字符串、空字符串、空值及缺失字段：输入 null 必须保留原字段及 null 值，非法短值／掩码空字符串按规则抑制，缺失字段或父对象不得补造；相邻属性和未命中的父对象必须保持不变。字段独立 Policy 有效时通过标准 Hosted 生命周期重启 Security、Manager，同一普通 User/Tenant、Baseline、Policy、DataItem 身份和未重扫前的字段检索／脱敏预览必须恢复。v5 报告必须包含 MongoDB 三算法及重启证据，退出前恢复 Baseline、撤销临时 Policy，退出时销毁三种业务容器。确定性回归沿用 `make test-online-runner`，真实验收沿用 `security-mysql-owner-protection`；2026-10-11 已确认将精确普通 collection 纳入 System 现有独立读取 Grant 主路径；Provider 核验不得采样。目录命令及拒绝视图／时序集合的确定性测试由 `make test-go` 自动发现；集合 Grant 的正式签发、撤销与历史恢复由既有 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package engineaccess"` 覆盖，不新增数据库或并行授权路线。本场景已在 `1a5a24020` 的 [Security Hosted T4 38101001354](https://github.com/pampa0629/addp/actions/runs/38101001354) 首次真实通过：v5 报告核对正式嵌套字符串组件、四 Owner active ack、7 条 MongoDB 数据的三算法独立预期、null 与缺失结构及相邻属性保持、Manager 独立 SM3 Policy 和撤销后的基线回落；Security、Manager 重启后实例身份和启动时间变化，构建身份、同一普通 User/Tenant 及 Baseline/Policy 保持一致，未重扫前的字段检索与保护预览通过。原有 MySQL 四出口和 Console 检索／定位／预览／CSV／Monitor 链路、PostgreSQL 空间表三算法与坐标保持同时通过；4 个临时资源全部删除，Baseline 恢复、临时 Policy 撤销，三种业务容器、Infra 及凭据清理通过。此证据仅将 MongoDB 的 Manager 预览和检索记为真实输出验收，四 Owner ack 不替代 MongoDB 其他出口的独立运行验收。

MongoDB Develop 补充验收沿用上述 suite、只读夹具和普通用户。夹具初始化必须在任何保护规则变更前完成 MongoDB 首次深度扫描及真实字段检索；Meta 扫描成功不代表 Manager 索引投递成功，不能在其他资源的保护清理屏障期间首次提交集合技术快照。后续算法与重启检查复用该 DataItem 和扫描证据，不通过补扫掩盖重启后的检索缺失。纳管前先验证同一敏感字段拼接 MQL 能成功返回夹具预期，以证明拒绝用例本身有效；正式 Assessment 生效后，在掩码、固定值替换、SM3 三种 Baseline 下分别执行原结构 find，核对全部 7 条记录、null、缺失父对象和相邻属性，再验证 count 正常返回 7、同一派生表达式执行失败且没有结果。Manager 独立 SM3 Policy 有效时，Develop 仍须执行默认 Baseline。v6 报告单列这些真实输出及拒绝边界；本扩展已在 `b4161f5a5` 的 [Security Hosted T4 38113570834](https://github.com/pampa0629/addp/actions/runs/38113570834) 真实通过，初始字段检索、纳管前拼接对照、三算法 Develop find/count/派生拒绝及 Manager 策略独立性全部核对通过；重启后未重扫即可恢复检索与保护预览，Baseline 恢复、临时 Policy 撤销，4 个临时资源全部删除，业务容器及 Infra 零残留。Service 查询服务当前只接受 SQL Runtime，MongoDB 仅声明 MQL，本轮不将四 Owner ack 或 SQL 引擎的 Service 验收记为 MongoDB 查询服务通过。确定性回归继续使用 `make test-online-runner`，现有 Makefile 自动发现和 Hosted workflow 已覆盖同一脚本；报告版本与平台 CI 注册检查同步更新。

MongoDB 原始记录导出补充验收复用同一 Hosted Security suite、普通用户及 7 条只读 Persons 夹具。每种 Baseline 算法均经 Manager 正式导出会话调用 Transfer，再从同一用户的会话文件接口下载 `mongodb_extended_jsonl`；直接核对文件内全部嵌套值、null、缺失字段／父对象、相邻属性及 BSON int64 的 Canonical Extended JSON 无损表示，不以预览、四 Owner ack 或文件存在替代。Manager 独立 SM3 预览 Policy 有效时再导出一次，结果仍须遵守 Transfer 默认 Baseline。Monitor 必须证明同一用户发起、Manager 来源、无任务定义和 7 条真实读写计数。文件只在内存解析，报告只保留摘要；导出会话及暂存文件随当次 Hosted Infra 销毁，不新增删除 API 或修改保留期。v7 报告单列原始记录导出证据；该扩展已在 `4196a84c4` 的 [Security Hosted T4 38127430328](https://github.com/pampa0629/addp/actions/runs/38127430328) 真实通过，三算法及 Manager 独立预览策略生效期间共完成 4 次下载，每次均核对 7 条记录、嵌套值、null／缺失结构、相邻属性及 BSON int64 无损表示，Monitor 的发起用户、来源和真实读写计数一致。原有 MySQL 四出口、PostgreSQL 空间表、MongoDB Manager／Develop、重启恢复及 Console 链路同时通过，Baseline 还原、临时 Policy 撤销、临时资源和 Hosted Infra 清理零残留。确定性反例、报告登记与夹具检查沿用 `make test-online-runner` 和平台 T0。

MongoDB 查询搬运三算法扩展沿用同一 Hosted Security T4、普通 User 和 `bounded + snapshot` 主链路，报告升级为 v8。当次 PostgreSQL 夹具只增加三个固定隔离目标表及精确读取 Grant，不修改真实业务数据或扩大到 Engine 级授权。先在源集合未纳管时，将包含敏感字段 `$concat` 的合法派生查询写入独立对照表并核对 7 条源值；正式纳管后，透明 aggregate 的嵌套字段别名与 `$ifNull` 投影分别验证掩码、固定值替换、SM3，核对目标 7 条数据、Unicode、空字符串、null、缺失字段落表后的 SQL NULL，以及相邻非敏感字段。相同派生查询在每种算法下必须在保护准备阶段因输出血缘不可证明而拒绝，不能把投影未激活的通用拒绝或源连接故障计为通过；执行写入条数及字节数为零，独立拒绝目标在执行前后均为空。Manager 独立 SM3 Policy 有效时，查询搬运仍使用 Transfer 的默认 Baseline。执行身份、任务归属和真实目标预览缺一不可；所有本轮任务按捕获 ID 删除并核对 404，Baseline 与 Policy 按既有路径恢复，目标表及对照值随 Hosted 夹具销毁，报告不保存原值。确定性反例由既有 `make test-online-runner`、CI 登记检查及 Platform CI 自动覆盖。2026-10-11，提交 `41bd12252` 的 [Security Hosted T4 38133692022](https://github.com/pampa0629/addp/actions/runs/38133692022) 首次真实通过 v8 查询搬运扩展：三算法分别写入 7 条目标记录，三次派生查询均因输出血缘不可证明在保护准备阶段拒绝，写入条数和字节数均为零，独立拒绝目标保持为空；未纳管派生查询对照与 Manager 独立策略期间的 Transfer 均真实写入 7 条。8 个任务及执行身份逐一核对，任务全部删除并确认 404；原有三算法原始记录导出、MySQL／PostgreSQL 出口、MongoDB Manager／Develop、重启恢复与 Console 链路同时通过，Baseline 还原、临时 Policy 撤销及 Hosted Infra 清理零残留。上述结果仅证明该 Hosted suite；同提交 Platform consistency 仍因 Ontology Transfer 契约来源漂移失败，不计为全仓 CI 通过。

四出口 Hosted T4 的 v10 扩展沿 `security-mysql-owner-protection` 的现有普通用户和 MySQL 夹具，通过正式重新发现命令创建有界执行。先冻结四个 Owner 当前投影 ID、revision 与 published cursor，再核对本次 discovery execution、递增纳管版本、最新发现时间和相同源结构；四个 Owner 必须保持投影 ID、递增 revision、更换 published cursor；时间按 API 规范接受带时区 ISO 8601 值，统一换算为 UTC 比较，拒绝无时区值及过旧回执。当前修订齐备后重新验证 Manager 预览、Develop 查询、Service 查询和 Transfer 新执行的实际抑制输出。旧修订回执、错误执行编号、未递增修订或非敏感字段损坏均失败。复用既有资源清理、900 秒预算和 Hosted 登记；仅在该套件的临时普通账号权限清单增加既有 `security.enrollment.update` 命令资格，不授予其他套件、不修改产品角色或源数据 Grant。不新增 API、自动续期或本地时钟改写。该扩展运行的源码尚未暴露治理投影有效期，365 天有效期及截止时间延长由 Security 编译器 T1 验证；该扩展不计为等待投影自然到期的 T4。新增确定性反例由 `make test-online-runner` 自动发现。

提交 `fabac4bcb8` 的 [2026-10-13 Hosted T4 38335798164](https://github.com/pampa0629/addp/actions/runs/38335798164) 已真实通过 v10，并完成下载报告的独立核对：正式重新发现保持源结构和四个投影 ID 不变，四 Owner 均安装更高 revision 与新 cursor，当前回执覆盖本次发现完成时间；Manager、Develop、Service 及 Transfer 新执行均输出五行，邮箱字段移除、客户标识保持，Transfer 实际读写均为五行且目标预览一致。既有两个 Security 停机窗口、14 个拒绝连接时间点、MongoDB 三算法及十个任务清理同时通过；Console 检索、定位、预览、CSV 和 Monitor 页面报告通过，零浏览器告警、零业务请求错误，四张截图已目视核对。领域资源、Hosted 应用、业务容器、Infra 和凭据清理均通过，零残留。该结果不覆盖后续提交 `b8ce75038` 新增的治理有效期展示、资源筛选和银行卡号识别，也不计为自然到期或全平台高可用验收。

保护投影自身到期的验证沿现有 Go T1 和 PostgreSQL T2：Common 的 `TestPreparedOutputRejectsExpiredProjection` 与 `TestProjectionStoreResultExpiryAgainstPostgres` 共用一组四 Owner／native 与 query 断言，通过正式变化流安装带 365 天有效期的历史夹具，在明确时点验证截止前、截止时及截止后门禁，拒绝到期后的非空与空结果并保留纳管标记和 cursor。随后经同一变化流安装相同投影 ID 的更高 revision，验证旧读取绑定的保护器仍拒绝输出；另一个 Owner 存储实例刷新持久 cursor 后，新读取执行新保护决策，空结果和 null 保持正确，重新打开存储后仍能恢复同一修订及保护输出。此恢复验证不触发自动续期，也不取消纳管。Manager 的响应边界回归验证无数据和字段结构泄露；Develop、Service 验证已过期查询在执行前拒绝且不遗留在途计数；Transfer 既有 PostgreSQL 导出门禁验证 native 与 query 的过期拒绝、零新增写入和既有目标数据保持。输出保护必须重新校验全部绑定投影，不能只在读取开始时校验。既有 Go 自动发现和 Common／Transfer PostgreSQL test pattern 已覆盖新增场景，无新入口或外部依赖；本组不调整正式有效期，不改变系统时钟，不改写业务投影，不计为等待真实 365 天的 T4 验收。

Transfer bounded 执行失败的诊断沿既有 `error_details.code` 输出：不可证明查询输出血缘的类型化 `plugin.ErrQueryOutputLineageUnresolved`（含逐层 `%w` 包装）在唯一完成事务中保存 `query_output_lineage_unresolved`。执行完成内部接口传递 error，不先转字符串再猜测原因；私有 message 仍仅用于内部诊断，专业响应继续通过 Common Observation 隐藏原始文本。Hosted 派生查询拒绝验收只匹配该稳定 code、failed 终态、本次执行身份、零写入与空目标前后事实，不消费已删除的 error_msg，也不把连接错误或同文案字符串当作保护拒绝。T1 覆盖类型化／包装／无关错误、当前租约下完成与专业响应隐藏文本；现有 Transfer PostgreSQL 读取隔离门禁复验诊断字段及权限，已有门禁登记承接，不新增数据库或测试入口。

四出口 Hosted T4 的 v9 扩展复用唯一 Security stop/start 与拒绝连接探针，增加控制面实际不可达期间的 Develop、Service、Transfer 输出验收。确认四 Owner 当前投影同步后，在临时 Hosted 环境停止 Security；MySQL 验证 Develop 查询和 Service REST Query 仍移除邮箱，Transfer 从空目标表执行 bounded snapshot 后核对五行非敏感字段及执行身份。MongoDB 在 SM3 默认保护下验证 Develop 嵌套字段、计数与派生查询拒绝，以及 Transfer 可证明字段血缘的查询落表与不可证明查询零写入；SM3 目标结果必须区别于上一轮固定值，不能用旧输出冒充本次执行。每个数据面探针前后均确认 Security 拒绝连接，Manager、Develop、Service、Transfer Backend 实例与构建身份保持不变，不调用 Security 治理 API、不改时钟或刷新保护投影。所有停止尝试均在 finally 经标准 start.sh 恢复 Security，并核对新的进程身份、相同构建、普通 User/Tenant/权限不变。原有三算法、重启恢复及 Console 验收保留，900 秒 Hosted 总预算、精确 Source Grant 和清理入口不变；共享生命周期能力只由既有 security-transfer-protection-online.py 拥有。新增确定性回归与登记负例由 make test-online-runner 自动发现，真实结果必须由 security-mysql-owner-protection 手工 Hosted 验证；未执行前不计为 T4 通过。临时原值授权仍仅适用于 manager/preview，本扩展不开放其他出口原值授权，也不宣称投影自身到期或全平台高可用已验收。

本扩展已在提交 `c322167b8` 的 [2026-10-12 Hosted T4 38219829612](https://github.com/pampa0629/addp/actions/runs/38219829612) 首次真实通过 v9：两个 Security 停机窗口、14 个前后拒绝连接时间点、四 Owner Backend 实例和构建身份保持、Security 新实例恢复及普通 User/Tenant 核验均通过。MySQL 五行的 Develop 查询、Service REST Query 与从空表开始的 Transfer bounded snapshot 均移除邮箱，目标客户标识保持；MongoDB 七行在 SM3 下完成 Develop 嵌套字段、空值与相邻属性校验，计数仍为七、派生查询不返回结果，Transfer 可证明查询落表与不可证明查询零条／零字节写入均通过，拒绝使用安全诊断 code 而非原始错误文本。MongoDB 十个任务及执行逐一核对、全部删除，Baseline 恢复、临时 Policy 撤销；既有三算法、重启恢复与 Console 检索／定位／预览／CSV／Monitor 链路同时通过，浏览器零告警及零业务请求错误，四张截图已目视核对。独立 v9 报告核对与 Hosted 应用、业务容器、Infra、凭据清理均通过，零残留；[同提交 Platform CI 38219801688](https://github.com/pampa0629/addp/actions/runs/38219801688) 全部 33 项通过。此证据只覆盖上述有效投影下的实际出口，不扩张为其他出口原值授权、投影自身到期或全平台高可用验收。

Manager 内部产物 T4 使用 `manager-internal-artifact-lineage` 单一 suite 和 GitHub Hosted Linux x86_64 disposable profile，不再由 macOS self-hosted 分发。`online-hosted-manager-gate.sh` 复用共享 Hosted 生命周期，从零建立 Infra、最小权限 User、Tenant 和 Business MinIO Engine；浏览器登录凭据只保存到当次仓库外 owner-only Secret 目录。通过正式构建入口构建 Linux amd64 Model3D 转换器及 Runtime 产品镜像，以镜像默认入口启动 Model3D Runtime；标准开发生命周期启动 System、Gateway、Meta、Manager、Monitor、Console、PointCloud 和 Document；不启动无关模块或模拟转换器。退出时停止应用并销毁 Business MinIO 容器、网络、数据卷及当次 Infra，检查零残留。Business MinIO 的确定性生成 LAS、仓库已跟踪的 3 页 PPTX、带 PNG 贴图的 DAE 与 3DS、仓库自有 IFC4 米制立方体及 zlib 压缩并内嵌 DXT5 红色贴图的单体 OSGB 必须经过同一次真实 Meta 深度扫描，再通过对应 Manager owner action/task 与真实 Runtime 生成 COPC、PDF、GLB。模型断言覆盖四种格式的 `model_3d + single` 分类；DAE/3DS 核对贴图资源引用及 GLB 内嵌 PNG，IFC 核对 STEP schema、实体摘要及米制立方体几何，OSGB 核对 GLB 内嵌 JPEG 与带 UV 的三角形。全部产物核对自包含 GLB 和非空网格、Manager/Monitor 血缘一致，以及 Console iframe 中 GLB 请求成功、模型加载完成和截图。Online Chromium 统一显式使用 SwiftShader 软件 WebGL，在无独立 GPU 的 Hosted Runner 上仍执行真实模型绘制；不屏蔽业务警告或用截图占位。浏览器报告 v5 仅将 Chromium WebGL Driver 的精确 `GPU stall due to ReadPixels` 性能诊断单列，记录来源、观测阶段及各阶段次数（包括栅格重投影/预览与截图），不将预览期间的诊断归因为截图。次数表示浏览器输出的诊断条数，浏览器可能限流，不能推断实际 GPU 读取次数或诊断未再次出现即无读取。共享前端测试工具独占精确消息分类；其余页面警告、错误及失败的业务请求仍使验收失败。重投影 T3 必须核对真实输出像元并断言无 Canvas getImageData 读取，不以诊断分类替代渲染验收。临时产物及任务按本轮捕获 ID 删除，内容和任务分别确认 404，模型源的用户预览模式恢复原值；执行尚未终结或无法确认资源归属时不得报告零残留。确定性脚本测试沿用 `make test-online-runner`，真实运行沿用 `make test-online ONLINE_SUITE=manager-internal-artifact-lineage`；本次模型扩展首次在 Hosted 环境通过前不计为已完成 T4，也不增加定时调度。

生产 Compose 单入口 T4 使用 Hosted disposable profile：在全新 Runner 上用正式构建入口生成 System Backend、Gateway、Meta Backend、Manager Backend、Transfer Backend、Orchestrator Backend、Console、System Frontend、Meta Frontend、Manager Frontend、Transfer Frontend、Orchestrator Frontend 和 Nginx 镜像，以 `addp-platform` project 启动这些真实容器及当次 Infra。Meta、Manager、Transfer 与 Orchestrator Backend 向 System 自注册，Gateway 从有效 Backend 实例发现对应的 `/api/v1/:module/*`；模块 Frontend 由 Nginx `/module-ui/{frontend}/` 转发，Console 的 `/{module}/` 地址继续由根入口处理。内置 Runtime 属于独立 `addp-runtimes` project，不参与公开入口的业务断言；同一 Runner 额外启动一个真实 GeoPython Runtime 和一个真实 Business MinIO，验证 `addp-runtimes`、`business` 的独立生命周期、Compose project 标签、网络归属和宿主机端口边界。

对外 Nginx 使用非默认回环端口；仅 T4 Compose 覆盖文件允许 System Backend 和 Gateway 额外发布回环诊断端口供通用构建身份预检，正式 Compose 保持只有 Nginx 对外发布。未参与验收的 Nginx 静态 upstream 只提供当次网络名称占位，不模拟参与断言的服务。业务断言必须从公开入口检查 Console、System、Meta、Manager、Transfer、Orchestrator Frontend 及其构建资源，并使用当次一次性 Tenant 的零权限、只读、仅创建 User 验证 System AuthContext 的精确 Permission 集合；System、Meta、Manager、Transfer、Orchestrator 的 API 分别验证未认证 401、无权 403，以及已授权读取。Meta、Transfer、Orchestrator 的写入入口还需用无效请求体证明仅创建权限可越过功能守卫但被参数校验拒绝，且只读账号不能写入；System Role Assignment 详情需证明本租户授权可读、另一租户的授权 ID 返回 404。无效请求体不得创建业务资源。测试还需核对 System 对外 origin、Meta、Manager、Transfer 和 Orchestrator Backend 的就绪与内部地址以及容器实际端口；全部退出路径分别销毁 Runtime、Business MinIO、平台应用、占位容器、镜像仓库和 Infra，核对四个 project 的容器、网络、卷零残留。Transfer Worker 的数据执行和 Orchestrator 的 DAG 执行分别由 owner T4 验收，不以本套入口检查代替。[Hosted T4 run 36141592135](https://github.com/pampa0629/addp/actions/runs/36141592135) 已通过此前的 Transfer 覆盖验收；扩展后的权限矩阵及 Orchestrator 覆盖须在更新提交上手工运行成功后才能计为通过。

同一 `compose-public-origin` suite 必须通过真实生产构建的 Console、Manager iframe 和 `/module-ui/manager/` 独立界面验证 Browser AuthSession。System owner helper 只把既有只读 User 的登录凭据输出到 owner-only 凭据目录；浏览器通过正式登录 UI 建立会话。测试在浏览器中暂时持有共享刷新锁以确定性地制造两个顶层页面同时请求续期，不拦截或伪造任何 HTTP 响应，必须证明只有一次真实 Refresh 请求、HttpOnly Cookie 轮换、三个界面令牌收敛、独立页刷新不额外续期，以及独立页退出后 Cookie 清除和 Console 同步退出。报告和截图不得包含凭据；浏览器证据缺失、身份不匹配或多次续期均使该 suite 失败。确定性验证沿用 `make test-online-runner`，真实部署沿用该 suite 的 Hosted T4 Job。 报告另行记录续期是否保留原 iframe 实例；该诊断不代替未保存业务状态的验收，出现重建时必须在交付中明确说明。

质量动态绑定 T4 `quality-dynamic-binding` 使用 GitHub Hosted Linux 一次性部署，唯一生命周期入口为 `scripts/test/online-hosted-quality-gate.sh`，移除自托管永久夹具路线。System owner helper 分别准备普通执行 User、夹具准备 User 和引擎注册 User；准备身份经正式 API 创建和审批两个 Model 逻辑表，通过 Model 物化接口创建带所有权标记的 PostgreSQL 目标，再在当次业务源写入固定数据并扫描 Meta。业务套件运行期间 Model 定义和物理数据不变；经正式 API 创建带 Run ID 的规则、方案和编排，验证 Model 默认版本输入、上游 `target_locator` 动态绑定、失败阻断、重复失败工单去重、两个目标的结果与工单隔离，以及显式升级规则修订后逐目标恢复。执行身份不拥有 Model 定义管理和引擎管理权限，准备凭据在业务验收前移除。套件先清理自己的编排、方案和规则，Hosted 退出再销毁本轮应用、业务源、Infra 和凭据目录；源数据变化、执行未终结、创建响应丢失或清理不可核验均不得报告通过。入口为 `make test-online ONLINE_SUITE=quality-dynamic-binding`，确定性最小门禁 `make test-quality-online-runner` 由既有 `make test-online-runner` 与 Platform CI 覆盖；首次真实通过前只允许手工 `workflow_dispatch`。

Quality 过程事件扩展复用同一套件和普通 User，经 Gateway 读取 Monitor 的 execution events，核对单条启用规则对应的 started、progress 与真实终态、attempt、百分比、顺序、游标和保留窗口；闭合事件字段之外的参数、业务值和授权信息均使验收失败。Online 分发器要求 Monitor 就绪，Hosted 生命周期显式配置 MONITOR_URL。确定性反例进入既有 make test-online-runner，新增 Quality PostgreSQL 用例按 TestIntegrationPostgres 前缀自动发现；本扩展未经 Hosted 真实执行时不计为 T4 通过。

Orchestrator 执行 T4 `orchestrator-execution` 复用 Hosted 生命周期、System owner 身份夹具和既有一次性 PostgreSQL 样例源，正式启动 System、Gateway、Meta、Orchestrator、Monitor。每轮经 API 创建扫描任务和嵌套串行 DAG，核对父子执行、成功进度、真实数据源中断后的失败阻断、安全步骤/事件以及跨 Tenant 与缺少 Owner 权限时的不可见性。故障操作必须核对当次源容器标签，恢复后删除本轮任务；原始执行历史由 Hosted Infra 销毁，凭据不归档。入口为 `make test-online ONLINE_SUITE=orchestrator-execution`，准备和退出由 `scripts/test/online-hosted-orchestrator-gate.sh` 负责，确定性测试进入 `make test-online-runner` 与 T0。首次真实通过前只允许手动 `workflow_dispatch`。该首轮只覆盖业务数据源中断；后端进程崩溃和 POST 响应丢失由同一 suite 的故障扩展验收，续租故障仍待后续真实验证。

权限边界扩展沿用同一手动 Hosted T4，不新增认证旁路或生产故障开关。System IAM 夹具提供同 Tenant 的普通读者，其 Orchestrator 读取与 Monitor 读取分别赋权；父执行及嵌套编排可读，Meta 子执行不授予读取权，树仅显示两层编排，子执行详情、树和事件返回 404。独立 Tenant 管理员仅通过正式 IAM 接口撤销该读者的 Orchestrator 角色分配；先验证旧令牌失效为 401，再经正式登录取得同 User、Membership 和 Tenant 的新令牌，确认 Monitor 权限仍在、父执行详情、树及事件均为 404。外部代理只中断本轮 Meta 的 execution-read-scope GET 连接，由真实 Monitor 返回 503 execution_owner_unavailable 且不含执行数据；恢复连接后核对完整执行树不变。凭据与代理控制只存于不归档的 owner-only 临时目录，业务报告不保存角色分配编号，未知撤权结果不重试，失败路径释放故障并由标准生命周期销毁部署。确定性验证归入现有 make test-orchestrator-online-runner 与 make test-online-runner / 平台 CI；扩展首次真实通过前不计为 T4 已通过。

权限扩展已在提交 `759ca7736` 的 [Hosted T4 37126213434](https://github.com/pampa0629/addp/actions/runs/37126213434) 真实通过，本轮共 20 项检查。普通读者可读取父执行与嵌套编排，Meta 子执行的详情、树和事件均不可见；正式撤销 Owner 角色分配后，旧令牌返回 401，重新登录仍保留 Monitor 权限时父执行详情、树和事件返回 404。中断 Meta 读取范围连接时，真实 Monitor 返回无执行数据的 503 `execution_owner_unavailable`，恢复后完整执行树一致。此前的业务源中断、响应丢失与 Backend 崩溃检查同时通过，两类派发故障均无重放。System、Gateway、Meta、Orchestrator、Monitor 的构建身份均匹配该提交；四个临时任务定义经正式 API 删除，生命周期清理通过，Infra 容器、网络及数据卷零残留。61 个归档文件未包含身份凭据及代理控制文件，也未检出标准令牌或夹具密码。此结果不包含续租故障、一次性执行及已删除任务历史的真实验收。

已删除任务历史扩展沿用同一 `orchestrator-execution` 手动 Hosted T4。所有已知父子执行收敛为终态后，经正式 API 删除本轮任务定义；Orchestrator 用任务详情确认删除前存在、删除后 404，Meta 用正式扫描任务列表确认删除前存在、删除后消失。删除前后逐项比较成功与失败执行的 Monitor 概览、安全步骤、错误类别、父子树和根执行事件，以及 Owner 已保存的步骤事实，禁止用当前定义重建历史。删除后再次验证跨 Tenant、缺少 Owner 权限、父可读而 Meta 子不可读；随后正式撤销父读取分配并重新登录，确认保留的历史仍受当前权限限制。删除请求结果未知时不重试，不报告清理成功；定义删除后任一历史或权限断言失败，整套验收仍失败，执行历史由既有 Hosted 生命周期销毁。报告只记录检查名、定义删除数量及已有安全执行身份，不保存前后快照或原始步骤结果。共享 Online HTTP 客户端按调用方声明校验对象或数组响应，Meta 列表明确采用数组契约，不修改公开 API。确定性用例复用 `make test-orchestrator-online-runner`，共享客户端通过 `make test-online-runner` 扩散验证，现有平台 CI 自动覆盖，无需新增登记；实现变更后须重新运行真实 T4 才能计为通过。

已删除任务历史扩展已在提交 `a5d8428ad` 的 [Hosted T4 37129222858](https://github.com/pampa0629/addp/actions/runs/37129222858) 真实通过，共 24 项检查。四个临时任务定义经正式 API 确认删除；删除前后，成功与失败执行共七条记录的安全概览、步骤、错误和父子树保持一致，两个根执行的事件及 Owner 保存的步骤事实也保持一致。删除后，跨 Tenant 和缺少 Owner 权限的读者对七条记录的详情、树和事件均不可见；父读取者仍可读取两层编排，Meta 子执行仍不可见。随后正式撤销 Owner 读取分配，旧令牌为 401，重新登录保留 Monitor 权限时，已保留的父执行详情、树和事件为 404。业务源中断、响应丢失、Backend 崩溃及 Owner 不可用检查同时通过；两个派发故障仍各只有一次请求和一个真实子执行，无重放。System、Gateway、Meta、Orchestrator、Monitor 的构建身份均匹配该提交，生命周期清理通过，Infra 容器、网络和数据卷零残留。55 个归档文件未包含凭据环境文件、代理控制或诊断快照，未检出标准令牌和夹具密码模式。同一提交的 [Platform CI 37129088307](https://github.com/pampa0629/addp/actions/runs/37129088307) 全部通过，其中所有者测试 45 项、Online 聚合测试 333 项；Orchestrator PostgreSQL 可靠性门禁也已通过。此结果不包含一次性执行的真实权限验收与续租故障。

一次性执行权限扩展沿用同一 `orchestrator-execution` 手动 Hosted T4。通过现有 Meta 手动扫描 API 创建无任务定义的真实执行，核对 `source_task_id` 为空、发起主体与当前普通 User 一致、执行收敛成功，Monitor 详情、树和过程事件可读且只含安全投影。System 正式 IAM 夹具增加同 Tenant 的另一普通 User，仅授予 Meta 扫描历史与 Monitor 执行读取权限；先证明该读者能读取已有任务定义的扫描执行，再确认其列表及总数不包含其他 User 的一次性执行，详情、树和事件返回无执行数据的 404。发起人的列表仅增加该执行；跨 Tenant 与缺少 Owner 权限时仍不可见。未知创建结果不重试，已知一次性执行必须收敛为终态，历史随 Hosted Infra 销毁。报告仅增加安全执行 UUID、事件数量、身份权限摘要和闭合检查名，不保存配置、主体编号或响应快照。复用既有 `make test-orchestrator-online-runner`、`make test-online-runner` 与 Platform CI 登记；公开 API、平台进程日志和审计路径不变，真实验收证据见下段。

一次性执行权限扩展已在提交 `d2af33d7b` 的 [Hosted T4 37131353509](https://github.com/pampa0629/addp/actions/runs/37131353509) 真实通过，共 28 项检查。Meta 手动扫描创建无任务定义、主体与发起 User 一致的真实执行并收敛成功；发起人可读取安全详情、单节点树及七条过程事件，执行列表仅增加该记录。同 Tenant 另一普通 User 的 Meta 与 Monitor 读取权限经真实任务历史正向证明后，对该一次性执行的详情、树和事件仍返回无诊断数据的 404，列表及总数保持不变；跨 Tenant 和缺少 Owner 权限时同样不可见。此前的嵌套编排、业务源中断、响应丢失、Backend 崩溃、Owner 不可用、已删除任务历史及正式撤权检查同时通过。四个任务定义确认删除，两类派发故障仍各只有一次请求，无重放。五个服务构建身份均匹配该提交，生命周期清理通过，Infra 容器、网络和数据卷零残留；61 个归档文件未包含凭据环境或代理控制文件，未检出标准令牌及夹具密码模式。同一提交的 [Platform CI 37131346153](https://github.com/pampa0629/addp/actions/runs/37131346153) 全部 29 个任务通过，包括 53 项所有者测试、333 项 Online 聚合测试和正式身份夹具测试；Orchestrator PostgreSQL 可靠性门禁也已通过。本轮不包含续租故障。

[System IAM PostgreSQL 门禁 37131346068](https://github.com/pampa0629/addp/actions/runs/37131346068) 首轮及单独重跑均被共享 main 的后续推送取消，日志未显示测试断言失败，两次取消本身不计为通过。后续 [Release and T2 运行 37164414672](https://github.com/pampa0629/addp/actions/runs/37164414672) 的[实际 IAM 验证任务 111324425990](https://github.com/pampa0629/addp/actions/runs/37164414672/job/111324425990) 已在包含上述实现的提交 `2eb927f37393088e628a8e1e3a087ea525b8d63b` 上成功完成，日志统计 319 项测试通过、0 失败、0 跳过，补齐该历史 T2 验证缺口，无需因早先取消再次重跑。该结果不替代后续 IAM 实现、迁移或门禁变更的验证，也不扩张上述执行读取隔离的验收范围。

范围限制：上述一次性执行隔离结论针对 Monitor 通用诊断，不能替代 Meta 专业入口的直接隔离证据。Meta 专业详情与列表的模块归属、任务历史及本人一次性执行读取已由独立回归与 Hosted 验收覆盖，见本文“Meta 专业扫描执行读取”。TaskProvider 服务状态读取仍按专用机器身份与 Permission 独立裁决，不套用用户本人读取规则。

首轮已通过 [Hosted T4 37106631401](https://github.com/pampa0629/addp/actions/runs/37106631401)。故障扩展使用同一手动 suite：Hosted 包装器为真实 Meta 配置只解析到本机的临时网络别名，将 Orchestrator 与 Monitor 的 Meta HTTP 请求交给本轮代理；代理转交真实请求并保存安全身份与计数，可丢弃已接受请求的响应或暂缓状态读取。进程崩溃必须用 pidfd 锁定本轮 Backend，在 UID、可执行文件和部署 Secret 目录核验通过后发送 SIGKILL；替代实例通过 `scripts/dev/start.sh -orchestrator` 启动。断言必须证明父失败原因、依赖未派发、已接受请求只有一次、子执行身份真实、Monitor 投影安全及父终态下步骤仅为最后记录。所有退出路径清理测试代理和本轮网络别名，再由既有生命周期销毁应用与 Infra；新增检查只有在更新提交的 Hosted T4 真实通过后才能计为通过，不自动加入夜间任务。确定性入口为 `make test-orchestrator-online-runner`，由 `make test-online-runner` 和平台 T0/CI 唯一聚合；登记检查同时核对三个 owner 测试文件，禁止遗漏。故障扩展已在提交 `f3d676f5a` 的 [Hosted T4 37122385397](https://github.com/pampa0629/addp/actions/runs/37122385397) 完成 15 项真实检查，并确认构建身份一致、任务定义删除、生命周期清理通过和 Infra 零残留；两类故障均为一次派发、一个真实子执行，父失败且子执行正常完成，等待步骤保持最后记录。续租故障不在此结果的覆盖范围。

续租故障扩展仍归同一手动 Hosted suite：HTTP 代理等待真实子执行状态请求，仅在当次 Infra PostgreSQL 安装指定父 UUID 的续租拒绝触发器，以非事务序列核对真实续租错误。必须同时证明监督器取消在途请求、父 execution 以 coordinator_stopped 收敛且保留等待记录、依赖不派发、原 Backend 自行退出、真实子 execution 成功、一次提交及标准替代进程启动后的终态/事件不变。准入核对 Hosted 私有目录、当次容器 ID、Compose owner 和数据库凭据；禁止在个人或共享本地数据库安装触发器。临时触发器、函数、序列和 schema 清理失败即失败，所有退出路径仍销毁 Hosted Infra。T0/T1 沿用 `make test-platform`、`make test-orchestrator-online-runner` 与 `make test-online-runner`，现有 Online/Platform CI 自动覆盖，无新增 workflow 或生产接口；[Hosted T4 37169854989](https://github.com/pampa0629/addp/actions/runs/37169854989) 已在包含实现 `7dc121432` 的 `c821b4760` 上通过 34 项检查，证明一次真实续租拒绝、请求取消、原 Backend 自行退出、替代进程启动后不重放、终态稳定与临时数据库对象清理；五个服务构建身份一致，Infra 零残留。[同版本 Platform CI](https://github.com/pampa0629/addp/actions/runs/37169830927) 全部 29 个 Job 通过，T0 直接执行 owner 回归 60 项及 Online 聚合 339 项，无跳过。

### 5.2 开关、身份与拓扑预检

本体修订投影 T4 `ontology-revision-lifecycle` 使用 GitHub Hosted `ubuntu-24.04` 临时部署，从零启动 System、Gateway、Ontology 与 Infra FalkorDB。System owner helper 创建非默认 Tenant 和仅含 `ontology.revision.read/update/publish`、`system.execution_authorization.create` 的 User，不创建业务 Engine 或 Engine Provisioner。北京 Outdoor 只作为合成本体定义，通过 Gateway 验证草稿、审核、发布准入、最新投影发现、异步激活、旧 version 拒绝、第二修订替换与撤回不回退。202 不作为激活成功证据，必须读取确定 generation 的 ready 状态和本体 active 指针。该 suite 没有业务实例读取或推理 API 断言，不宣称 Agent 能力验收。

Ontology 修订、审计与历史图在验收期间保留，不增加测试删除接口或 SQL 清理旁路；所有退出路径由 Hosted 生命周期销毁当次 Infra（含 PG/FalkorDB 数据卷）和凭据，并核验容器、网络、卷零残留。业务报告将历史标为待部署销毁，只有生命周期清理成功才可报告整体通过。首次真实通过前仅登记手工 `workflow_dispatch`。

每次 T4 运行至少满足：

- `ADDP_ONLINE_TEST=1`。
- 显式非默认 `ADDP_ONLINE_TEST_TENANT_ID`，禁止 Tenant 1。
- 全局唯一的 `ADDP_ONLINE_TEST_RUN_ID`；未提供时由分发器生成并贯穿全部子测试。
- 每个参与 HTTP 服务 `/health/live` 可用，且 Build ID、Git commit 或源码指纹与当前 checkout 匹配；进入业务断言前 `/health/ready` 必须返回 200。
- 除下述已确认的平台节点指标控制面隔离验收例外外，测试 User、Service Principal 和 OAuth Scope 只具备场景所需的最小权限；不得使用个人会话或扩大生产身份权限。平台管理员 Token 默认禁止，仅在该限定例外中允许。
- 通过 System AuthContext API 证明 principal、context、Tenant、client、token 与 Permission 事实后，才进入资源创建或注册拓扑操作。
- 同一 `suite + Run ID` 通过进程锁串行化，锁覆盖预检、业务断言和报告落盘全过程。

每个断言只验证规范定义的唯一身份链路，不在 User Token、Service Token 和直接 SQL 之间兼容回退。Repository / Migration 的直接数据库夹具属于 T2，不能替代 T4 API 授权验收。

平台节点指标控制面的隔离验收身份例外（2026-10-06 用户确认）：仅在干净 GitHub Hosted Linux 一次性部署中，允许通过 System 正式三员 Bootstrap 建立临时平台系统管理员，并通过真实密码/MFA 登录取得 Platform User Token，验收正式节点台账、监测目标管理、九项标量、五项文件系统字节容量、四项 inode、两项磁盘吞吐、三项磁盘 IO 时间及两项网络收发速率的节点资源即时/趋势查询（含一分钟 CPU 忙碌率、精确挂载维度和非特权可用容量使用率）及资源查询预算配置 API（资源查询与预算读写范围于 2026-10-06 再次经用户确认；第九项 CPU 忙碌率、五项文件系统容量及四项 inode 读取分别于 2026-10-07 经用户继续推进确认；两项磁盘吞吐、三项磁盘 IO 时间及两项网络收发速率读取与原生 Linux 页面验收于 2026-10-08 经用户继续推进确认）。账号保留既有角色的完整权限，测试脚本只执行同一 platform-node-metrics suite 中该链路的节点/目标管理、当次节点资源查询和当次测试库的查询预算 CAS/热生效验证；不借用个人或生产身份、不新增测试专用生产角色、不以管理员读取租户业务数据。Prometheus 仍使用独立 `addp-prometheus` Platform Service 身份和唯一 `monitor.metrics_discovery.read`；User、其他 Client、Tenant 与无权限身份须分别验证拒绝。所有退出路径销毁当次部署、身份、卷与仓库外凭据并核验零残留。同一 suite 的真实 Console/Monitor iframe 验收沿用这些身份及资源读取范围，通过密码/MFA 登录验证实际页面；不得注入 Token 或拦截业务 API 冒充真实链路。本例外不扩大其他 T4 suite 的身份范围；实际运行继续严格按 Platform/Tenant Context、对应 Permission 和 Owner 对象策略隔离。具体链路与实施状态见 `docs/next/ADDP平台运行监控与可观测性设计.md` 第 3.3、10.19、10.22、10.25、10.27、10.28、10.29、10.36、10.37、10.38 节；未实现或未真实执行的 suite 不计为已通过。

同一 `platform-node-metrics` Hosted 隔离验收的身份例外于 2026-10-10 经用户确认扩展到服务实例监控：临时平台系统管理员可沿真实密码/MFA 读取当次 System 平台实例列表（含 `ids` 精确过滤）、Monitor 三项进程资源摘要，并验收 Console「服务监控」页面。两个真实 Go SDK 实例须证明自动声明/发现、未关联主机时的自身资源、CPU 完整窗口、RSS/时长单位、实例隔离及停止/重启后旧实例不借用当前值；来源或中心失效不得破坏其他业务运行边界，Tenant、Service、Delegated 和缺权限主体仍须拒绝。此扩展不增加预算配置操作、不读取 Tenant 业务数据、不修改生产 IAM；沿用原最小权限 Prometheus 身份和全退出路径销毁/零残留要求。具体实施与证据见上述设计文档 10.49 节，未执行项不计通过。

同一 suite 的进程趋势限定测试用途于 2026-10-12 经用户明确确认：上述临时平台系统管理员可新增读取当次当前运行实例的 `GET /api/v1/monitor/platform/process_resource_trends`，请求仅含 `instance_id/start/end`，只验收 CPU 核占用与 RSS 两条趋势，以及 Console 服务趋势的单位、缺值断点、启动边界、当前实例绑定和刷新恢复。沿用真实密码/MFA、原授权与采集身份链路，不读取已退出实例历史，不新增预算配置操作，不读取 Tenant 业务数据，不修改生产 IAM 或 Prometheus 权限；所有退出路径继续销毁当次部署、临时身份及凭据并核验零残留。具体实现、个人复验与 Hosted 状态见设计文档 10.55 节，未实际执行的 Hosted 不计通过。

同一 suite 的单真实主机资源总览限定测试用途于 2026-10-13 经用户明确确认：上述临时平台系统管理员可沿真实密码/MFA 新增读取 `GET /api/v1/monitor/platform/resource_overview`，请求不携带 query 或 body，范围只限当次一次性 Hosted 部署内已纳管的完整授权主机集合。当前拓扑仅有一台真实 Linux 主机，验收该范围的 CPU/内存榜单、覆盖率、当前身份与版本、查询预算整份拒绝、自然刷新与关闭恢复、主机详情导航及已有故障/停采/恢复阶段；保留原节点与进程验收、原权限和独立 Prometheus 采集身份。该确认不包含受控多源夹具、角色权限修改或其他 suite，不读取 Tenant 业务数据、不修改生产 IAM，不将单主机通过计为多主机排序、分别撤销两个 Owner 权限、完整资源总览 T4 或生产 T5 通过。所有退出路径继续销毁当次部署、临时身份和凭据并核验零残留；具体场景及逐项证据见设计文档 10.58、10.59 节。

API 消费方数据面边界由 `workbench-service-consumption` 复用同一 Business MySQL Fixture 和临时 Query Service 验收，不另建平行 suite。该场景必须分离两个真实 User Token：非管理员 Token 只负责 Service / Workbench 发布与消费，专用 Tenant Administrator Token 只负责 API 消费方与凭据生命周期。suite 通过正式 API 创建一个只授权主 Query Service 的 API 消费方，并必须同时证明：已授权服务返回 200；同 Tenant 未授权 Query Service 返回 `403 api_consumer_service_denied`；任一 `/api/v1/*` 控制面路由返回 `401 api_consumer_control_plane_denied`；删除 API 消费方后，旧凭据在 Gateway 30 秒正缓存对应的 45 秒有界收敛窗口内返回 `401 api_consumer_credential_invalid`。报告、日志和错误不得包含完整 API Key，每轮必须按捕获 ID 删除 API 消费方、其级联凭据、Data Application 和两个临时 Query Service，并验证零残留。

模块注册与恢复类 T4 必须使用正式进程入口至少证明：

1. 业务模块先启动、System 后启动时，模块先为 Alive/Not Ready，不能处理业务；System 恢复后使用同一 `instance_id` 自动进入 Ready。
2. System 与业务模块先启动、Gateway 后启动时，Gateway 在首个完整快照后进入 Ready 并建立路由。
3. System 运行中断后，Backend 在心跳失败被观测后转为 Not Ready，Worker/Scheduler 停止领取新工作，Gateway 在已有租约过期后返回 503。
4. System 恢复后所有实例无需重启即重新 Ready，Gateway 自动恢复路由，不需要前端人工刷新或二次点击。

仅启动 Probe Server 并直接调用 System 注册 API 可以验证租约与路由算法，但不能替代上述进程级就绪与恢复证据。

专用 Runner 可以通过受 `ADDP_ONLINE_HOST=1` 强制保护的精确进程参数复用正式开发构建与进程入口，以控制业务 Backend、System 和 Gateway 的真实启动顺序；该入口不得成为日常开发的第二套模块依赖路径。精确停止必须先验证受管 PID 的进程身份。每个进程阶段都必须把 Liveness、Readiness、业务门禁、实例身份和 Gateway 路由投影写入仓库外证据目录。

消费方与 Engine Instance 动态恢复类 T4 必须另外证明：全量重启后真实 User 在同一 Browser Context 首次打开 Console 原生页与模块 iframe 不需要 reload、手工刷新或二次点击；Engine Instance 经正式连接检测 API 从 `online` 转为 `offline` 再恢复 `online` 时，已打开页面自动更新，相关 Backend 与 Frontend PID 保持不变。Engine Instance 是永久身份，此类 suite 使用专用 Runner 预置的长期 Engine Fixture，只控制隔离物理端点的连通性，不得按 Run ID 创建、删除 Engine Instance 或清理墓碑。物理 Fixture 必须由 owner 的 Online 专用生命周期入口管理，要求 `ADDP_ONLINE_HOST=1`、不读取仓库内 `.env`、不复用平台 `POSTGRES_DB`，并在成功、失败和中断路径恢复连接观测后停止。

企业资源目录发布类 T4 使用同一永久 PostgreSQL Engine Instance 及其 owner 生命周期入口创建的稳定表 `public.addp_online_catalog_fixture`。专用环境必须预置可引用的 Standard Domain 和 Department；首次运行可将该永久数据源对应的 `discovered` CatalogEntry 初始化为稳定 `curated` fixture，后续运行必须在验收后恢复其编目聚合。同一 suite 必须重复执行真实 Meta 扫描并证明 fingerprint 与 CatalogEntry UUID 幂等，验证 `inventory` / `governance` 视图、治理覆盖率固定维度和精确来源身份解析；真实浏览器使用同一专用 User 登录 Console，验证覆盖率页、目录详情与 Domain / Department / Engine 名称选择器，并将浏览器 warning/error 计入失败。每轮创建的 Asset 和 AssetCategory 必须经正式 API 下架、删除并证明零残留；不得直接 SQL 清理。

该 suite 同时在上述专用 Catalog fixture 上验证 `curated → deprecated → curated`，以及已弃用阶段的责任子资源更新。使用现有专用 User 增加或移除可选技术负责人，使责任集合发生实际变化；不为验收新增账号、部门或赋权。断言移交仍保持弃用、业务定义、语义、可见范围设置和来源身份，撤销弃用不恢复认证，旧版本请求返回 `409 catalog_entry_version_conflict` 且无副作用。成功和失败路径都经正式 API 恢复完整编目聚合并重新读取核对；写响应丢失时先读当前状态，不重试原命令，恢复失败使整个门禁失败。审计和并发版本不回滚，跨部门移交后的可发现范围及权限交集继续由 Catalog T2 验证，不以单账号 T4 冒充。

该 suite 还验证普通只读共享的正式准备、真实双服务 OAuth 反查与首次受理。专用 User 必须是 fixture 的当前业务负责人，显式具备共享确认、独立办理和批准要求初始化／读取 Permission，并由管理员预置当前有效的引擎管理委派；测试不自动赋权或创建委派。专用部署须配置独立 `SYSTEM_SERVICE_CLIENT_SECRET`，未配置在生命周期启动前拒绝验收，不改变普通部署允许留空的 Ready 边界。只为上述精确稳定表通过正式 API 初始化尚不存在的 Catalog 批准要求，已有独立批准模式拒绝而不覆盖；不可变业务决定、已核清的办理记录与 System 回执属于专用 Tenant 的长期审计事实，单列证据，不按临时资源删除。正式请求显式限时十分钟，办理窗口仍从首次受理起最多五分钟。同编号换用途或批准要求版本须返回 `409 catalog_sharing_decision_conflict`，拒绝后原决定／原回执必须不变；不新增决定或办理编号。此拒绝允许产生操作审计，不能以场景预期数量代替数据库计数验证。同参重放必须保持完整绑定、受理时间及截止时间不变；调用方丢弃已提交响应后恢复只证明调用方边界，不冒充 Catalog→System 网络中断或进程重启。人类 Token 访问机器反查／受理接口必须拒绝；`pending`、绑定错误或恢复失败不能计为通过。受理不等于 Grant 写入或实际可读，本 suite 不验证或创建源访问 Grant。

Manager 平台内部产物类 T4 使用专用 Business MinIO Fixture 和永久 MinIO Engine Instance。Fixture owner 幂等写入仓库内确定性小型 LAS 与多页 PPTX 样本；suite 经 Gateway 触发真实 Meta scan，并使用扫描所得的 ResourceLocator、item ID 和 fingerprint 验证两条正式链路：`point_cloud_copc_generation` 必须由 PointCloud Runtime 从业务对象存储读取源文件、向 Manager infra MinIO 发布 COPC，并由 Manager execution 写入 `addp.lineage-facts/v1`；PPTX 预览必须由 Quick View Capability 声明 `generate_pptx_pdf`，并通过统一 action 入口首次触发 `pptx_pdf_generation`，由 Document Workflow / LibreOffice 发布多页静态 PDF，同源再次读取 Capability 只复用同一任务与结果，不创建第二次 execution。Monitor API 与真实浏览器必须展示同一业务输入和 `addp-infra://` 输出；浏览器还必须在 Data Explorer 翻到后续 PDF 页，跨过 Engine 状态周期刷新后仍保持当前页。退出路径通过 Manager 正式 API 删除两类结果与任务、验证对象不可再读取及临时资源零残留；不得直接操作数据库或 infra MinIO 清理。

MAX 的 Manager Hosted T4 扩展复用 `manager-internal-artifact-lineage`，从专用 Business MinIO 读取未修改的 CC-BY-SA-3.0 马模型及 ADDP 自建 4 × 4 RGB `(220,30,60)` 贴图。页面验证默认米、手选毫米和显式 bitmap 映射；正式生成声明 `source_unit=mm`，核对转换审计、五个静态网格、十二个 primitive、444 个顶点、米制场景尺寸和实际内嵌贴图像素，不固定跨 Blender 版本的 PNG 编码哈希。Meta 仅识别 MAX 格式身份；六种模型与点云、PPTX、栅格共同核对九项输入／输出血缘、缓存加载和零残留清理。确定性入口仍为 `make test-manager-online-runner` 与 `make test-online-runner`；真实验收仍为现有 Hosted suite，实际运行通过前不计为 T4 通过。

Manager 混合检索类 T4 使用同一专用 Business MinIO Fixture 和永久 MinIO Engine Instance，Fixture owner 额外幂等写入仓库内确定性 JPG。专用 Tenant 必须通过正式 Inference 与 Manager 配置 API 预置支持文本和图片、维度为 2560 的 Model Profile 及 `semantic_search_embedding` 场景绑定；Provider Credential 只保存在 Inference 加密存储，Online 环境只声明预期 Model Profile ID。suite 不创建、更新或删除 Provider、Deployment、Model Profile、Credential 和场景绑定，而经 Gateway 触发真实 Meta scan 与 Manager ad-hoc embedding，验证 Inference、pgvector、Meilisearch 和 Manager 对称 RRF 融合的完整链路：同一 `document_id` 的关键词与向量候选只能返回一次，纯向量候选必须在融合后参与统一分页，响应不得包含旧 `vector_hits`。每轮 embedding 结果必须通过 Manager 正式 API 删除并确认零残留；新增 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得加入夜间调度。

指标修订生命周期 T4 `metric-service-revision-lifecycle` 唯一使用 GitHub Hosted `ubuntu-24.04` 临时部署，手工调度时明确选择 PostgreSQL 或 TiDB 业务引擎，每次只运行一个引擎变体；每日夜间调度在独立 Job 中运行两个变体。每轮自动创建独立 Infra、非默认 Tenant、最小权限 User、所选业务数据源，并通过正式 Meta、Standard 和 Model API 建立扫描结构、已发布定义、已审批逻辑表及已发布指标夹具。两个引擎变体复用同一组 `count_distinct` 与 `sum_decimal_by_group` 指标契约、确定性数据和业务断言；各自使用符合其引擎目录模型的命名空间和独立物理夹具。按城市汇总面积的夹具使用 `DECIMAL(38,18)`，断言同城高精度求和、空字符串分组和 NULL 分组排除后的精确字符串结果。两个指标分别使用带 Run ID 的独立实现，验证首次查询的非空确定结果、后续发布不会自动切换服务、撤回绑定修订后服务仍 active 但查询被拒绝，以及显式重绑后恢复。实现和修订历史在验收期间保持完整，禁止 SQL 删除或绕过 Model 生命周期；临时 Query Service 通过正式 API 删除并验证 404。退出时销毁整套临时数据库、业务容器和凭据，仅将无密钥的引擎类型、ID、状态、校验摘要、日志与清理结果归档为 Actions Artifact。不得复用个人环境、永久测试租户或自托管 profile；两个变体须各自首次真实通过并确认清理零残留后才可进入夜间调度。

限时原值访问 T4 `security-plaintext-access` 唯一使用 GitHub Hosted Linux x86_64 disposable profile，复用 `scripts/test/online-hosted-security-gate.sh`，删除旧 macOS self-hosted 路径和个人环境审批 Token 配置。System-owned IAM helper 在非默认 Tenant 创建两名不同的最小权限 User：申请用户持有原值申请权限，审批用户持有审批与撤销权限；两者都不取得准备身份权限。独立准备身份通过正式 Security API 建立手机号元数据识别和默认遮盖规则，并通过 System 精确表 Grant 命令为两名用户分别授予当次 `addp_online_security.exemption_source` 的读取权；同一目标的独立批准要求仅随首个 Grant 原子初始化一次。准备 Token 在进入场景前移除。套件通过正式 Gateway API 覆盖 `manager/preview` 的完整流程：申请、另一用户从 `scope=pending` 审批、批准后仅申请用户在有效期内看到原值，审批用户仍看到遮盖值；到期后不得刷新投影或调用 Security 判定，Manager 必须根据本地投影直接恢复遮盖。再次申请和审批后验证提前撤销恢复保护，并在 `scope=history` 分页查询中核对已批准记录的 `expired|revoked` 当前授权状态。Enrollment、Assessment、AccessRequest、Exemption 及不可变修订在业务断言期间完整保留，退出随 disposable Infra 整体销毁，不篡改或删除审计；所有退出路径核对清理零残留。此套件验证真实 API 和 Manager 数据面，不代替申请与审批页面的完整浏览器验收，也不声明 Security 实际停机故障已通过。确定性回归由既有 `make test-online-runner` 自动发现；2026-10-10 的手工 Hosted [run 38059348726](https://github.com/pampa0629/addp/actions/runs/38059348726) 在提交 `b57a8149a` 首次真实通过：`addp.security-plaintext-access-online/v2` 报告覆盖 3 行（含空值）、40 秒到期、再次批准后提前撤销、两用户权限隔离及两种审批历史状态；应用、业务容器、Infra 和仓库外凭据清理通过，零残留。运行日志显示第一次有效原值预览至到期遮盖预览之间没有新投影安装。当前仅手工调度。

同一 suite 的页面扩展使用两个隔离 Chromium Context，分别通过真实 Console 登录申请人与审批人。申请、另一人批准、再次申请及提前撤销只能通过 Manager／Security 正式页面操作，不以 API 写入代替点击；浏览器身份必须与 API 已验证的两名普通 User 和 Tenant 一致。审批夹具只补页面准入必需的 `security.enrollment.read` 和 `meta.catalog.read`，不增加申请或准备权限。两次批准后的预览必须由页面在当前用户有效授权对应的 `projection_acknowledged=true` 后自动刷新；未同步时继续展示脱敏值和等待同步状态，不以重新导航替代批准后的自动刷新。确定性 Manager 浏览器回归固定推进待审批、已批准未同步、已同步三个状态，证明旧状态不触发刷新且确认后只刷新一次。首次授权真实等待到期后重新加载 Manager 预览，第二次批准后从审批历史进入既有授权详情并提前撤销；核对三行含 null、另一主体始终脱敏，以及两次批准记录的当前授权状态。两名用户的浏览器登录凭据仅写入仓库外 owner-only 文件并随 Hosted 清理；报告不保存原值或令牌，截图仅保留脱敏预览、审批和撤销界面。v3 总报告必须同时通过原有 API 到期本地判定和页面报告校验，缺少报告、截图、身份或任一业务断言均失败。已有 `make test-online-runner`、Platform CI 登记和手工 Hosted workflow 同次覆盖。该扩展已在提交 `2e9d37e8b` 的 [2026-10-12 Hosted T4 38191967691](https://github.com/pampa0629/addp/actions/runs/38191967691) 首次真实通过：v3 总报告与 v1 页面报告核对同一 Tenant 的两名不同普通 User、两次批准后的同步确认和自动原值刷新、真实到期及提前撤销后的脱敏回落、三行数据与 null 保留、另一主体始终脱敏，以及审批历史的 `expired|revoked` 当前授权状态；浏览器告警与业务请求错误均为零。待审批、到期和撤销三张安全截图已目视核对，应用、业务容器、Infra 及凭据清理通过，零残留。同一提交的 [Platform CI 38191930626](https://github.com/pampa0629/addp/actions/runs/38191930626) 完整通过。此证据不声明 Security 实际停机故障已通过；suite 仍只手工调度。

同一 `security-plaintext-access` suite 的 v4 总报告追加 Security 实际不可达期间的限时原值访问验证，保留已有 API 流程和完整双用户页面验收。使用同一 Tenant、两名普通 User、原 DataItem 和 Assessment 再申请一份 90 秒临时授权；确认 Manager 已返回原值并收到当前投影同步确认后，仅在通过 Hosted 准入的临时仓库中运行标准 `scripts/dev/stop.sh -security`。四次 Manager 预览分别验证有效期内申请人看到原值、另一用户看到脱敏值，以及真实授权到期后两人都看到脱敏值；每次预览前后必须独立确认 Security Backend 的固定回环监听端口拒绝连接，并保持 Manager 实例与构建身份不变。停机窗口不调用 Security 治理 API、不重启 Manager、不写入或刷新保护投影、不调整时钟或放宽业务断言。所有停止尝试都在 `finally` 经标准 `scripts/dev/start.sh -security` 恢复；恢复必须验证 Security 实例变化、启动时间前进、构建身份不变，两用户身份及到期审批历史不变，再继续原有页面验收。生命周期子进程环境移除两用户登录凭据和准备身份令牌，报告只记录过程身份、时间、拒绝连接证据及输出校验结果，不保存原值或令牌。恢复、报告或清理失败均判失败，由既有 Hosted 退出路径销毁整套临时部署；现有 900 秒总预算不变，标准 `make test-online-runner` 与 CI 登记同次覆盖。此扩展只验证未过期投影上的临时授权到期，不代表长周期投影本身到期、控制面页面在故障中可用或全平台高可用已通过。扩展已在提交 `96432fd38` 的 [2026-10-12 Hosted T4 38196037099](https://github.com/pampa0629/addp/actions/runs/38196037099) 首次真实通过：v4 报告核对 90 秒授权、四次预览前后的八个拒绝连接时间点，确认有效期内仅申请人看到原值、到期后两人均脱敏；Manager 实例和构建身份保持不变，Security 恢复后的实例及启动时间变化、构建身份不变，两用户身份及到期历史核验通过。原有 API 流程与 v1 双用户页面报告同时通过，浏览器告警与业务请求错误均为零，三张安全截图已目视核对；应用、业务容器、Infra 及凭据清理通过，零残留。此 Hosted 结果与 Platform CI 分别记录，不将其他门禁的失败计为通过。

MySQL 邮箱四出口保护类 T4 使用当次 Hosted MySQL Fixture 的 `customers.email` 和当次 Hosted PostgreSQL Fixture 的固定目标表，由独立初始化用户经正式 API 为当次 Tenant 创建 `email` 敏感类型、`addp.detector.email_metadata/v1` 检测绑定和 `suppress` 默认保护规则。suite 必须经 Meta 真实扫描发现字段，经 Security 正式 API 形成 Enrollment 与 Assessment，并等待 `manager/preview`、`develop/query`、`service/service_execute` 和 `transfer/export` 投影全部确认。四个 Owner 都必须继续返回 5 条非敏感数据，同时在返回结构与每条记录中完全移除 `email`；返回空邮箱、原文邮箱、整个请求被拒绝或整个结果为空都不算通过。每轮临时 Query Service 和 Transfer 任务必须按捕获 ID 删除并确认零残留；Enrollment、Assessment 及不可变审计在业务断言期间保留，退出时随 disposable Infra 整体销毁，不通过 SQL 修改治理事实。

OceanBase 消费链路 T4 使用专用 OceanBase CE MySQL 模式 Fixture 和永久 `engine_type=oceanbase` Engine Instance。Fixture 固定维护一个 5 行非空间 InnoDB watermark 源表和一个同构空目标表；suite 必须经 Meta 扫描取得两个 ResourceLocator，以同一条 bounded watermark Transfer 任务依次验证首批 5 行、源表确定性更新/新增后的 2 行增量，以及再次执行的 0 行空增量。目标采用 `upsert` 和唯一稳定键 `id`，不得把 OceanBase 降格注册为 MySQL 或为模块增加类型分支。首批与增量完成后，Manager 表预览、Develop SQL 和临时 Query Service 必须通过各自正式数据出口读取同一目标 ResourceLocator，并得到一致 checksum，最终固定为 6 行且无重复键；Manager 预览还必须确认扫描结构中的完整列顺序。临时 Transfer 任务和 Query Service 必须按捕获 ID 删除并确认 404；物理 Fixture 在成功、失败和中断退出路径恢复源表基线与空目标表后停止。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

TiDB 消费链路 T4 复用同一个关系引擎 owner 断言，在带 `self-hosted`、`macOS`、`addp-online` 标签的专用 Runner 上使用固定官方 digest、无数据卷的 PD/TiKV/TiDB 三组件 Fixture 和永久 `engine_type=tidb` Engine Instance。Fixture 固定重建 5 行非空间 watermark 源表和同构空目标表；suite 必须经 Meta 扫描取得 ResourceLocator，以同一条 bounded watermark + upsert Transfer 任务验证 5/2/0 三次读取计数，并由 Manager、Develop 与临时 Query Service 对最终 6 行结果计算相同 checksum。上层模块只消费通用 ResourceLocator、SQLDialect 和 Provider，不得把 TiDB 登记为 MySQL 或增加 `tidb` 类型分支。临时任务和服务必须按捕获 ID 删除并确认 404；成功、失败和中断退出路径都必须执行 `down --volumes --remove-orphans` 并验证三组件容器零残留。该 suite 首次真实通过前只登记手工 `workflow_dispatch`，不得增加定时触发。

KingbaseES 消费链路 T4 复用同一个关系引擎 owner 断言，仅在带 `self-hosted`、`Linux`、`X64`、`addp-kingbase` 标签的受保护 Runner 上使用固定 SHA-256 的 V9R1C10 `V009R001C010B0004` 官方介质和 owner 正规 License。Fixture 每轮启动无卷 `DB_MODE=pg` disposable 容器，固定重建 5 行非空间 watermark 源表和同构空目标表，并输出 owner-only Engine 描述；通用注册器以最小权限 Provisioner Token 通过正式 System API 创建当次 `engine_type=kingbase` Engine Instance。suite 经 Meta 扫描取得 ResourceLocator，以 bounded watermark + `ON CONFLICT` upsert 验证 5/2/0 和最终 6 行 checksum，并由 Manager、Transfer、Develop、Service 经各自正式出口取得一致结果。上层模块只消费通用 ResourceLocator、SQLDialect 和 Provider，不得增加 `kingbase` 分支。成功、失败和中断路径都必须删除本轮平台库、Engine Instance、容器、镜像、临时任务、服务和 owner-only 凭据目录，验证零残留；License 和镜像 tar 不归档。首次真实通过前只允许手工 `workflow_dispatch`。

openGauss 消费链路 T4 复用上述关系引擎 owner 断言，但只在 GitHub Hosted `ubuntu-24.04` x86_64 profile 上运行。生命周期从固定 SHA-256 的 openGauss 6.0.6 LTS 官方介质启动独占容器，创建 `addp_online` 平台库、非默认 Tenant、最小权限消费 User、只含 `system.engine.create/read/execute` 的 Engine Provisioner 和临时 `engine_type=opengauss` Engine Instance。Business Fixture 只管理数据库并输出 owner-only Engine 描述，不调用 System API；System IAM owner helper 只创建身份和 Token，通用 Online 注册器使用 Engine Provisioner Token 通过正式 System API 注册并验证 Engine。Fixture 以 `public` schema 作为 namespace，使用 openGauss 原生 `MERGE` 推进相同的 5/2/0 watermark 数据集；Manager、Transfer、Develop 和 Service 必须通过与 OceanBase 相同的通用 ResourceLocator、SQLDialect 和 Provider 路径得到同一 6 行 checksum，上层模块不得新增 `opengauss` 分支。当次 Engine Instance 只随 disposable 平台库销毁，Token 和连接凭据只保存在不归档的 owner-only `runner.temp` 目录；Engine 注册后必须在进入业务断言前从进程环境清除 Provisioner Token 与数据库凭据，业务证据不得包含凭据。该 suite 在首次真实通过并确认构建身份、清理和零残留证据后，同时保留手工 `workflow_dispatch` 并登记每日夜间 `schedule`；定时事件只能选择 openGauss Hosted Job，其他 Online Job 必须显式排除定时事件。

Transfer 仅新增 MySQL T4 使用永久 PostgreSQL 与 MySQL Engine Instance，并由专属复合 Fixture 管理两端物理表。真实 User 必须从 Console 页面选择源表和目标库、进入字段映射、通过正式“分析源数据并推荐精度”接口把无声明精度的 PostgreSQL `numeric` 收敛为不会截断当前值的 MySQL `DECIMAL(6,2)`，再以主键 `id` 作为唯一 watermark 创建任务。页面自动执行的首轮必须读写 6 行；Fixture 同时修改 `id=1` 并新增 `id=7` 后，用户从任务详情再次执行必须只读写 1 行。最终 MySQL 目标必须共 7 行、`id=1` 保持首轮值、`id=7` 为新增值且无重复键，以证明单字段水位只覆盖严格递增新增、不承诺旧记录更新。浏览器还必须断言任务配置的 `tie_breaker=[]`、目标 `upsert` 键为 `id`，捕获并删除临时任务且确认 404；Fixture 在全部退出路径删除两端固定表并停止两个容器。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

Transfer 关系型 SQL ETL T4 `transfer-relational-sql-etl` 唯一使用 GitHub Hosted `ubuntu-24.04` x86_64 临时部署，不保留 macOS 专用 Runner 路径。生命周期复用 `scripts/utils/hosted-online.sh`，从零建立独立 Infra、非默认 Tenant 和最小权限测试 User；System-owned IAM helper 创建身份，独立 Engine Provisioner 通过正式 System API 注册当次 PostgreSQL 与 MongoDB Engine Instance，注册后清除 Provisioner Token；业务 User 不持有 `system.engine.read/execute`，只从 Meta 读取同 Tenant 引擎投影。Business Fixture 只管理独占无持久卷 PostgreSQL 与 MongoDB 容器、固定源表、源集合和目标表，并输出 owner-only Engine 描述，不调用 System API。数据库用户只能读取源表并在指定 schema 创建、管理自己的目标表。真实 User 必须从 Console 选择源表，确认单语言引擎只显示固定 `SQL` 而不提供 MQL、语言下拉或高级 SQL 编辑入口，通过基础构造器选择输出字段并配置两个参数化行过滤，再选择 PostgreSQL 目标位置创建 snapshot 任务。浏览器必须断言保存事实只有标准 `source.query` SQL statement 与类型化 parameters，其中 decimal 过滤值以字符串保存而不是 JavaScript Number；下一步字段映射只包含实际投影字段，并等待 Worker 读写恰好 2 行。Fixture 必须核对目标行集合、金额汇总和列顺序，以同时证明过滤、投影和精确参数绑定语义。临时任务必须通过正式 API 删除并确认 404。成功、失败和中断退出路径均销毁当次业务容器、Infra、平台数据卷及 owner-only 凭据目录，检查零残留；Engine Instance 与身份随平台库销毁。凭据不进入日志或 Artifact。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

同一个 `transfer-relational-sql-etl` suite 同时覆盖字段血缘首期：使用无管理员角色、具备 `meta.lineage.read`、`manager.data_item.read` 和 `manager.content.read` 的同一 User，经正式 API 执行原生表 snapshot 的字段改名、decimal 精度转换、同目标 replace 映射更新和两跳搬运。Meta 必须从真实成功执行自动采集，断言 direct/derived、精确字段与结构快照、旧入边关闭及两跳查询不串入其他字段；不得用手工 collect 请求代替自动消费。Console 的 Manager iframe 必须通过 `subject_kind=data_item&granularity=field` 一次请求整表字段图，选择真实目标字段时在本地图中聚焦，不重复查询。该普通 User 不授予 `catalog.entry.read`，字段图不得触发 Catalog 摘要请求。浏览器先完成登录并核对同一 User 与 Tenant，再监听所有受保护业务请求的错误；登录前无会话的 refresh 401 不计入业务请求，但登录后的 401、403、503 均不得忽略。Provider 能完整证明单来源输出映射的 SQL 查询源必须记录字段血缘，浏览器核对目标字段来源、转换语义和同一次 execution 的证据；opaque、unresolved 或不完整查询证据仍声明 unavailable，不能伪造同名字段来源。同一套件还必须覆盖 MongoDB 原生集合经透明 MQL `$project` 投影到 PostgreSQL ODS 的六个显式映射，查询输出别名只用于后续字段映射，冻结源结构仍保留 `title.date`、`title.level`、`leader.personid` 和 `leader.userInfo.nickName` 等精确点号字段名。同一个任务连续执行两次，每次均须在不手工扫描目标、不调用 collect 的情况下等待自动元数据与字段图，核对冻结源/目标结构和当次 execution 证据；第二次只允许最新成功执行的活跃入边，目标 DataItem 身份保持稳定。Manager 整表字段图必须展示跨引擎来源，并验证日期、昵称字段在本地切换聚焦。Hosted Fixture 同时管理独占 tmpfs MongoDB 7.0 与 PostgreSQL 容器，MongoDB 登记用户只读源库，核对 ODS 六列的完整行集合并销毁两个容器。新增外部依赖登记到统一 Hosted 清理清单，不访问本地业务容器、不授予消费 User 基础设施管理权限。编排扩展沿用同一 suite，新增 Develop、Orchestrator 与 Monitor 服务预检，消费 User 仅补充两个模块的任务创建、读取、执行、删除权限，以及 Develop 数据读写、Monitor 执行读取与 System 执行授权权限。独立夹具预建空 DIM、DWD 正式表，先扫描结构，再由一个真实编排连续执行两次 ODS Transfer、DIM relation 查询及 DWD JOIN 查询。每轮通过既有 Monitor 安全投影核对三条子执行的父 UUID、实际 owner 与任务身份，Develop 专业执行详情仅核对安全资源引用、顶层 outputs 和实际行数，精确字段映射及结构哈希通过 Meta 字段图核对，不能从安全投影读取被移除的 operations 和 schema_snapshot；Develop 冻结读取 URI 按不带选择器 item_id 的规范资源路径比较；日期字段必须从 MongoDB 经 ODS、DIM 三跳追溯到 DWD，昵称改名与强度表达式分别证明 direct、derived，第二轮不允许第一轮活跃入边或编排父执行伪造资源边。整表字段图展示 DWD 的最新证据，独立物理验证 DIM 三行和 DWD 两行的完整集合、真实日期类型。两个 Develop 任务及一个编排定义通过正式 API 删除并确认 404，目标正式表随 Business 容器销毁。三个 PostgreSQL 原生任务、一个 MongoDB 查询任务与一个 SQL 任务均由本轮 Run ID 命名并通过正式 API 删除、确认 404；源集合、源表和六个目标表由同一个 Business Fixture 在退出时清理。结构演进阶段由同一个 Fixture 在独占 PostgreSQL 中真实改名 DWD 昵称列并将 date 改为 timestamp without time zone，通过正式 Meta 扫描验证旧关系失效、新结构暂不可用，再第三次执行原编排；不增加定义或消费权限。当前图核对新列名、日期 derived、新 DWD hash 与当次子执行，旧 hash + 第二轮父执行完成时间的历史图核对原字段、原 direct 日期与第二轮证据，写入前后分别检查 stale、closed；未变更的源、ODS、DIM hash 与 DataItem 身份保持稳定。浏览器只展示第三轮新结构，物理夹具独立验证新列、timestamp 类型和完整行集合，v5 报告保存两代结构及历史取证。确定性验证沿用 `make test-online-runner` 和 `make test-platform`，真实 T4 沿用 `make test-online ONLINE_SUITE=transfer-relational-sql-etl`，首次真实通过前不增加 schedule。 同一 suite 还须用独占 PostgreSQL 的 500 列、2 行宽表执行两跳原生 Transfer；只扫描初始源，目标元数据与血缘须自动产生。整表图省略 limit 参数，核对 1500 个字段节点、1000 条活跃关系及两次真实 execution 的精确结构证据。真实 Manager iframe 只发起一次整表字段查询，验证首、中、末字段搜索与聚焦、全图适应窗口、卡片拖拽后的曲线端点和自动布局，保存请求／绘制耗时及截图。数据库 owner 独立核对两级目标的全部列、类型和完整行集合；任务经 API 删除，物理表随同一 disposable 容器销毁。本扩展通过 Hosted T4 前不计为已完成真实宽表验收。

### 5.3 数据、超时与清理

GeoPython 本地 T1 使用独立 Python 3.12 venv 与匹配的原生 GDAL，`make test-geopython-workflow` 复用开发入口的 GDAL/PROJ 和 Runtime 专属 ODBC 配置；CI 安装 GDAL、MDBTools ODBC、unixODBC 和 pkg-config，再安装匹配绑定。生命周期 T1 覆盖健康 GDAL 绑定复用、缺失绑定、同版本动态库失效及版本不匹配后的无缓存源码重建、重建失败和重建后仍不可用时拒绝启动，以及依赖及导入失败、端口归属、原生 PID 清理和先预检后停止；Runtime T1 使用真实 Gunicorn，验证健康就绪后注册以及跨请求状态。`make test-arcgis-open-formats ARCGIS_OPEN_FORMATS_ARGS=--runtime-only` 只覆盖真实 MDB/PGeo 读取，不能计为 Transfer/Oracle Spatial 往返通过。

栅格工作流 T4 `raster-workflow` 只在 GitHub Hosted `ubuntu-24.04` x86_64 独占部署运行，复用 `scripts/utils/hosted-online.sh`，由根 `make build-images` 构建 GeoPython 产品镜像并以默认入口启动独立 `addp-raster-online-runtime`，不调用 GeoPython 开发启动。共享入口核验实际镜像 ID、默认 command 与提交并保留 Runtime 日志，所有退出路径按当次所有权清理容器和镜像。样例生成与成果物理校验通过 `ADDP_ONLINE_RASTER_RUNTIME_IMAGE` 使用入口本次构建并核验的同一个产品镜像；未传入镜像时在启动 Docker 前拒绝执行，不引用开发镜像。手工栅格场景的 preflight 与业务执行总预算为 1740 秒；该场景预算由栅格入口设置，不改变其他场景或夜间调度预算。Business Fixture 管理两套无持久卷、凭据不同的 MinIO，生成含 EPSG:4326、确定性双波段和 NoData 的 TIFF；System-owned IAM helper 创建非默认 Tenant 与普通 User，独立 Provisioner 经正式 API 注册两个业务 Engine。User 经 Develop 提交 `raster_load → raster_band_math → raster_save(profile=cog)`，必须验证 create、重复 create 失败和 replace，并独立读取成果像元、CRS、transform、NoData 与完整 COG 布局。Meta 的轻量 COG profile hint 与完整布局校验分别验证：物理夹具记录成果是否具有 overview，Meta 的 `is_tiled`、`has_overviews` 与 profile hint 须与该真实结构一致；小尺寸、无 overview 的合法 COG 可被轻量探测表达为 `geotiff`，不能据此拒绝已经通过完整布局校验的成果，也不能用 hint 替代完整校验或人为新增 overview 掩盖断言错误。场景还需重投影确定性源到 EPSG:3857 的 1 米网格，裁剪两个重叠区域，分别验证 first/last 镶嵌 COG 的全部像元、NoData、空间属性和原成果未改变；每个成果均验证自动扫描、派生血缘和同一普通 User 的浏览器展示。多波段组合验收需使用各数据波段缺值位置不同、含合法零值与部分 alpha 的确定性 TIFF，并分别覆盖 NaN 与有限 NoData；有限 NoData 的平均插值成果须逐波段保留有限声明与缺值，保存重读的波段计算成果须声明 NaN 并保留独立有效性，尺寸重采样后保存 COG，再经正式 ResourceLocator 重新读取并计算 `b1+b2`；独立物理夹具逐波段验证全部像元、NoData 声明、alpha 覆盖和重读后的联合有效性，同时核对先前全部成果及源对象哈希未改变。平均插值组合还需以非均匀源像元、波段独立缺值和透明干扰值验证有效贡献像元的面积均值，同时覆盖整倍数与非整数缩放比例；非整数网格须以源、目标像元矩形的面积交集独立验证边界贡献权重，不能使用等权均值；保存重读后的相加应复用各波段自己的平均结果，不能提前合并源有效性。双线性插值组合还需按各波段自己的有效中心与邻域权重验证，中心无效时不得用有效邻域填补；放大后 COG 的 overview 事实须与 Meta 轻量探测一致，保存重读后相加仍分别保留波段有效性。目标 DataItem 只来自 Develop 自动提交的 Meta scan，血缘只来自 owner 自动采集，禁止测试手工扫描目标或调用 collect 掩盖自动链路缺陷。Monitor API 与 Console 必须展示同一成功执行及源、目标血缘，浏览器登录身份与 API User 相同。凭据仅存 owner-only runner.temp，不进入业务进程环境或证据；全部退出路径销毁业务容器、临时 registry、平台容器和数据卷并检查零残留。首次真实通过前只登记手工 workflow_dispatch。 发布确认后清理故障复用该 suite：Business-owned 回环存储代理仅针对首片／末片两个明确对象，在真实 MinIO PUT 成功后、回复 Runtime 前，按上传长度与 SHA-256 唯一定位私有临时 COG，在 sidecar 路径创建目录触发真实 unlink 失败；原 COG 字节和大小保持不变。代理以 Runner 的 UID 和实际 Docker socket 所属 GID 运行，使 owner-only 故障事件的生产者与读取者身份一致；由已有 Hosted 生命周期独占创建和销毁，仅内部持有 Docker socket 与故障事件，不新增产品故障参数。部分确认时下一片为 not_attempted，全部确认时不返回 failed_tile/publication；两者父执行仍 failed，逐一核对确认产物、严格整数大小、新自动扫描、先前成果哈希和执行工作目录清理，未真实通过前不计为 T4。 min/max 聚合扩展复用已有选择第 2 数据波段、3×5 分组及 nearest 金字塔的单一 DAG，逐像元独立验证源 NoData 排除、内部和边缘不足整组的数值、CRS／完整仿射网格及金字塔；发布后的 artifact、物理对象、Meta DataItem 大小均为严格正整数且相等。新增两个成果及两个浏览器场景，原成功与失败场景完整保留，48 轮浏览器及业务总预算仍为 1740 秒；扩展真实通过前不计为 T4。

多来源栅格验收继续使用同一 suite：分别加载源 Engine 的 EPSG:4326 TIFF 和目标 Engine 已自动扫描的 EPSG:3857 COG，显式参考网格对齐、选波段及组合后，验证跨文件加权表达式和 RGB 通道合成。Develop 与 Monitor 输入事实必须绑定完整的两个 ResourceLocator，Meta 必须存在同一执行从两个源 DataItem 到目标的 derive 边；Console 按完整输入集合验证卡片及链接，并分别绑定来源证据。物理夹具以独立像元公式验证所有波段、NaN、mask、Red/Green/Blue 解释、网格、CRS 和 nearest 金字塔，核对源及所有已接受成果哈希、精确对象集合；新成果必须保持 `size_bytes == physical stat size == Meta DataItem size > 0`。两个新执行增加四份浏览器来源证据，总数为 29；总预算扩展不改变 Runtime 执行、物理校验和单次浏览器的超时，也不放宽大小、像元或血缘断言。RGB 通道生成不代表显示拉伸、自动最佳波段推荐或 Manager RGB 渲染已经验收。

多选构图的真实浏览器贯通继续复用同一 Raster Hosted T4：保留全部原 API 场景，新增一条普通 User 经 Console／Develop 页面多选三个独立登记 TIFF、配置镶嵌及 COG 保存、首次保存、重载后运行的场景。仅 `raster-workflow` 身份夹具增加 `develop.task.create` 和 `develop.task.delete`，容量 suite 不继承这两项权限；不增加更新、引擎管理或管理员权限，不改变产品权限规则。真实页面负责创建和执行，测试不得通过 API 或图对象注入代替构图；读取真实保存定义和执行回执，核对运行时、来源顺序、公开参数、目标及父执行绑定。成功及可确认归属的失败路径删除本轮任务并确认 404，未知创建／执行结果不得自动重发。新成果沿既有独立全像元／mask／金字塔／COG、严格三方正整数大小、自动扫描、逐源血缘、同一 User 浏览器及零残留断言验收，原 1740 秒预算保持不变；首次真实通过前不计为用户操作链路已完成。

重分类沿用原 suite，在多来源计算后追加 `reclassify-nodata` 和 `reclassify-keep`：经正式源 ResourceLocator 加载确定性双波段 TIFF，选择第二数据波段，分别验证省略 `unmatched` 时的默认 NoData 和显式保留原值。规则必须涵盖精确值、合法零类别、左闭右开区间、未命中间隙和无界端点。独立物理公式验证完整输出像元、NaN、mask 与 nearest overview，且源 NoData 不能因 `keep` 恢复有效；仍核对原全部成果的物理字节和对象集合。两个成果继续采用严格三方大小相等，自动 Meta scan、资源级 derive、Monitor 和同一普通 User 的 Console 全部验证。增加两份浏览器证据，总数为 31，最终保留 22 份 COG；不调整现有 1740 秒 suite 预算或单请求超时，不放宽原 29 份场景的断言。

整数分组聚合继续追加到原 suite 的重分类场景之后：`aggregate-mean` 和 `aggregate-sum` 选择源第二波段，以 `factors=[3,5]` 生成 86×52 单波段 COG，并显式构建 43×26 nearest 金字塔。独立公式验证全部像元、源 NoData 的有效计数，以及右侧、底侧和右下角不足整块的分组；均值仅允许 Float64 运算所需的局部容差，旧场景和求和仍保持原精确断言。继续逐次核对先前 22/23 份成果哈希、全部源字节、精确对象集合、自动 Meta scan、资源级 derive、Monitor 和同一普通 User 的 Console；严格保持三方正大小相等。浏览器总数增至 33，最终 24 份 COG，1740 秒总预算和各单项超时不变。

区域外裁剪在聚合之后追加 `clip-outside`：正式源 ResourceLocator 加载原双数据波段 TIFF，`raster_clip(mode=outside)` 使用带孔 Polygon，保留完整 256×256 源网格并直接保存双数据波段及 alpha，不通过选波段省略 alpha 验收。独立行列公式检查外围、孔洞、区域内 NaN、源 NoData 不恢复、255/0 alpha、逐波段 mask、颜色解释及 128×128 nearest 金字塔。每个数据波段有效像元为 53247；先前 24 份成果 SHA、全部源字节和精确对象集合继续核对。严格保持 `size_bytes == physical stat size == Meta DataItem size > 0`，沿用自动扫描、资源级 derive、Monitor 和同一普通 User 的 Console。浏览器总数为 34，最终 25 份 COG；原预算、单项超时和旧场景断言不变。新增测试由既有 `make test-raster-online-runner`、`make test-geopython-workflow`、Platform CI 及手工 `online-t4-gates.yml` 自动覆盖，不新增入口、依赖或 suite。

有效像元轮廓在外裁剪之后追加 `footprint-any` 和 `footprint-all` 只读执行：正式加载双数据波段及 alpha 的确定性 TIFF，省略 validity 验证默认并集，显式 all 验证交集。独立整数像元公式核对完整面覆盖、边界网格、面积和波段独立缺值，不依赖 Polygonize 或生产算子生成期望，不限定合法环的起点和方向。Develop/Monitor/Console 展示同一普通 User 的 GeoJSON 预览，不能生成持久输出、derive 或目标扫描；物理夹具继续保护源和先前 25 份 COG，原严格三方大小规则不变。浏览器为 36 份，COG 仍 25 份；预算、超时、原场景门禁及 CI 登记路径不变。该扩展已在提交 `89e77da9a1bc72ec6c7e1791f2172edbbfdefb28` 的 [Hosted T4 37741228176](https://github.com/pampa0629/addp/actions/runs/37741228176) 通过并独立核对归档；并/交集覆盖分别为 65532/65524 像元，36 份浏览器、25 份 COG 及零残留清理通过。该结果不将同提交 Platform CI 中已取消的 GeoPython 全量 Job 计为通过。

局部更新追加到原 suite 的外裁剪之后：正式加载基底与双数据波段及 alpha 的更新源，裁剪更新源局部范围后显式对齐基底，执行 `raster_update` 并保存带 nearest 金字塔的双波段 COG。独立行列公式验证更新覆盖、区域外和无效更新像元保留基底、合法零、部分 alpha、逐数据波段有效性与源不可变；严格三方大小、原前置成果 SHA 和自动扫描/资源级 derive 保持。两个输入分别绑定来源浏览器证据；浏览器为 38 份、COG 为 26 份，后续只读轮廓查询继续保护全部 26 份成果。入口、预算、单项超时和自动登记路径不变。该扩展已在提交 `53591eba7e749c84849fe2e1ebff4a8ee7a525e9` 的 [Hosted T4 37795049439](https://github.com/pampa0629/addp/actions/runs/37795049439) 通过并独立审计；更新成果 182565 字节、两波段有效数 65535/65536，38 份浏览器、26 份 COG、双来源 derive 及零残留清理通过。同提交 [Platform CI 37794921834](https://github.com/pampa0629/addp/actions/runs/37794921834) 实际执行标准 GeoPython 全量 697 项通过；同提交另一取消运行不计通过。

Transfer continuous 过程事件 T4 `transfer-continuous-events` 使用独立 GitHub Hosted Linux x86_64 部署，复用共享 Hosted 生命周期；只登记手工调度，真实通过前不加入夜间运行。Business owner 创建独立业务 Kafka、开启 logical WAL 的 PostgreSQL 源／目标和确定性数据；不把 Infra Kafka 冒充业务源，不复用个人容器。普通 User 经 Gateway 创建和控制业务 Kafka 与 PostgreSQL CDC 任务，正式 Worker 执行，Business 夹具独立验证 snapshot、upsert 和 delete 的目标事实。通过 Transfer 与 Monitor 的既有授权入口核对 started、累计提交进展、唯一终态和恢复链；强制退出只允许 pin 当次部署的 Worker PID，恢复使用标准启动入口，不增加产品故障参数，不以测试内 Supervisor 或受控授权 Gate 代替。诊断读取验证同租户仅 Monitor 权限、跨租户及正式撤销 Owner 读取授权，Owner 不可用时失败关闭；事件与通用响应不能回显原始配置、业务样本、原始 checkpoint、内部 Worker 或租约信息。User／Role／Session 只由 System-owned 测试 helper 经正式 IAM Service 创建，不改生产 IAM 或权限规则；业务值和凭据不进入事件证据，全部退出路径核对应用、业务夹具、Infra 和私有凭据清理。第一切片证明真实 API／Worker 链路，沿用共享过程组件的 T1/T3，不宣称 Console 浏览器或 Kafka HA 已经过本 suite 验收。

T4 临时夹具优先通过 owner 正式 API 创建；正式 API 无法建立必要前置状态时，才允许 owner 提供专用测试 helper。Hosted profile 的全新平台库在尚无可登录 User 时，可由 System-owned helper 调用正式 IAM Service 创建当次 Tenant、User、Role 和 Session；helper 必须限定 GitHub Hosted Linux 及 `addp_online`，不得通过 SQL 写入 Principal、Role、Assignment 或 Token。Engine Instance 等永久身份按上一节使用预置专用 Fixture，不适用“每轮创建后删除”；Hosted disposable profile 的当次 Engine Instance 随平台数据卷整体销毁。跨模块 Online 场景不得以直接 SQL 作为常规夹具路线。

每个 suite 必须：

- 让全部资源名称可追踪到 Run ID。
- 为总场景、单次 HTTP 请求、路由收敛和租约收敛设置明确超时。
- 只对临时传输失败或规范允许的异步收敛执行有界重试；业务冲突、权限失败和 409 不自动重试。
- 在成功、失败、超时与中断路径执行 owner 清理并检查错误。
- 结束时查询并断言残留为零；清理失败时，即使业务断言通过，门禁仍失败。
- 将故障注入放在测试 Transport、测试 Provider 或明确测试 hook，不在生产代码中保留调试分支。

### 5.4 报告

统一分发器必须在仓库外证据目录生成 `addp.online-gate/v1` 报告，成功与失败采用同一结构。报告至少包含：

- suite、scenario、Run ID、Git commit 和工作区状态。
- 参与服务的脱敏地址与构建身份。
- Tenant、数据库类别、阶段耗时和稳定错误码。
- owner suite 创建、清理和零残留证据。

报告、日志和 CI artifact 不得包含 Token、Client Secret、完整敏感响应或可复用凭据，也不得提交到仓库。

### 5.5 触发策略

新增 T4 suite 先通过手工 `workflow_dispatch` 执行。专用 Runner 至少完成一次真实通过，并确认构建身份、清理和零残留证据后，才允许增加夜间 schedule。环境未就绪必须报告环境失败，不得 Skip 为通过。

## 六、T5 发布认证协议

T5 按产品或 Runtime 独立准备真实前置条件，例如 macOS Keychain、安装包生命周期、HA、故障切换或在线厂商证据。统一 `test-release` 分发器只选择 owner 门禁并生成 `addp.release-gate/v1` 报告，不合并不同产品的运行条件。

厂商只提供离线 Docker tar、且 Runtime 受 OS/CPU 架构约束时，官方介质认证归 T5：owner 脚本必须在一处固定官方 HTTPS 地址和 SHA-256，负责下载、校验、`docker load`、原生启动、真实驱动/SQL 断言及所有退出路径清理；workflow 只选择匹配架构的 Runner 并调用统一 `test-release` 入口。此类 suite 不声明 `ADDP_T2_SERVICES`、不进入 `test-integration` 或辅助 macOS 巡检，也不能替代插件实现后的 T2 Provider 集成与 T4 跨模块验收。

官方介质包含限时试用 License 时，T5 只允许将其用于首次准入评估，不得以重新下载介质、重建容器、替换主机或清除状态的方式重置试用期。长期门禁必须由 owner 提供来源合法、当前有效且 SHA-256 明确的 License，门禁报告只记录授权元数据与摘要，不归档 License 原文。

发布 workflow 只准备环境、调用标准 Make 入口、归档证据和执行发布动作，不在 YAML 内重写业务测试。System IAM PostgreSQL 属于 T2，不因与发布流程共用 workflow 而变成 T5。

未具备真实凭据或 Runtime 的 T5 suite 不得登记占位实现；需要人工认证时必须明确记录未验证项和后续责任门禁。

KingbaseES 的 T2 Provider 与 T5 官方介质认证暂不接入 GitHub Actions，保留标准命令与确定性回归，由持有正规 License 的 owner 在受控 Linux x86_64 主机人工执行。T5 未声明 `workflow_job` 表示不由 GitHub Actions 编排，登记检查拒绝 workflow 调用其 suite 或内部 owner 目标；恢复自动执行前需另行确认 Runner 隔离与生命周期方案。此调整不改变 Online T4 的现有协议，脚本回归通过不代表真实授权认证通过。

## 七、CI 编排与登记

按路径选择门禁的托管工作流（Platform CI、Release/T2、Quality Smoke）使用独立 Runner 和 Job 独占的 disposable 资源，不设置 workflow 或 job 的 `concurrency` 分组。每次运行保留自己的提交范围与选择结果，由 GitHub Runner 配额排队；后续无关提交不能取消正在执行的门禁，也不能替换待执行门禁。仅将 `cancel-in-progress` 改为 `false` 不足以满足此要求：GitHub 默认仍以新运行替换同组已有的 pending 运行。专用 T4 主机的资源互斥遵循其既有 Job 锁，不套用托管门禁的调度规则。

Push 使用事件的 `before...sha`，PR 使用 base 与本次 SHA 的 merge-base 差异；共享依赖和 owner 声明的测试输入继续由 `changed-gate.py` 统一扩散。文档运行跳过测试只表示该提交未命中路径，不能替代前序代码提交的验收证据。`make test-platform` 的 CI 登记与选择回归约束上述调度，并验证连续相关提交后追加文档、共享依赖、取消/失败/意外跳过的判定；原有超时、owner 清理、零残留核验及 required check 汇总必须保留。

历史案例：[37171513445 的首次运行](https://github.com/pampa0629/addp/actions/runs/37171513445/attempts/1) 在同分支文档 Push 后取消正在执行的 System IAM 和日志 T2；后续[文档运行 37171988879](https://github.com/pampa0629/addp/actions/runs/37171988879) 将这两项测试跳过。旧运行的 IAM 汇总正确判为失败，但文档运行成功并未补齐被取消的测试。验证调度修复时，应在相关门禁执行和排队期间追加无关文档提交，再核对前序 Job 继续完成、排队 Job 实际启动，以及文档运行只跳过自身未命中的门禁。

产品镜像构建继续由 `make build-images` 唯一入口负责。构建调度最多同时执行两个任务，基础镜像预热先于任务启动；单路和两路使用同一实现。前端 Node 版本来自根 `.node-version`，仅使用锁文件安装依赖。`make test-platform` 校验这些契约，并验证并发上限、失败汇总、日志隔离及中断时的进程和临时目录清理。现有 Platform CI 产品构建 Job 复用该入口验证实际镜像；Node 版本变更必须通过影响选择命中全部前端镜像，不缩减受影响服务范围。

发布工作流安全审计统一由 `make test-workflow-security` 执行，并纳入本地 `test-platform` 与 CI 现有 System IAM required Job。唯一脚本固定 zizmor 1.28.0，只扫描 `.github/workflows/release-and-t2-gates.yml`，使用 auditor、medium 最低严重级别、关闭在线审计，不读取自定义配置或忽略标记；安装、扫描或输入收集失败必须阻断。首次运行需联网取得固定版本的二进制 wheel，使用临时虚拟环境且退出时清理；不依赖 ADDP 服务或数据库。

Workflow 只负责：

- 触发条件与 owner 影响选择。
- Runner、语言版本和 disposable Service 准备。
- 调用唯一 Make / script 入口。
- 超时、并发、Artifact、Step Summary 和 required check 名称。

不得在 workflow 中复制 SQL、业务夹具、测试选择表达式、模块启动逻辑或清理逻辑。能通过 Git 和依赖声明自动发现的事实不维护手写清单；必须手工登记的门禁由 `make test-platform` 的一致性检查验证完整性。凡通过 `ADDP_T2_SERVICES` 声明 PostgreSQL、MongoDB、MySQL、OceanBase、TiDB 或后续数据库 Service 的 T2 门禁，都必须同时登记根 Make 入口、`test-integration` 串行聚合、共享模块变更选择和带摘要的 CI Job；每个声明的 Service 必须存在，并使用显式版本 tag 与镜像 sha256 digest。T5 suite 如声明 `workflow_job`，登记检查必须通用验证该 Job 只经 `test-release` 分发、配置仓库外证据目录并挂接统一摘要，不按产品或数据库类型增加检查分支。

新增或修改模块、测试入口、基础设施依赖、构建方式或 suite 时，必须在同一次变更中同步：

1. owner 测试与安全检查。
2. 根 `Makefile` 标准入口。
3. 自动发现、影响选择或显式 suite 登记。
4. `.github/workflows/` 编排与报告。
5. 对应规范、模块说明或运行指南。

不得提交一个永久排队、连接开发环境或始终 Skip 的占位 workflow。

### 7.1 辅助 macOS 定时巡检

日常使用的 macOS 可以在独立 checkout 中定时运行 `make local-ci`，作为 GitHub Actions 之外的辅助反馈机制。该入口只复用已有 T0-T3 标准门禁，不产生第二套测试事实，也不替代 GitHub required checks。

- checkout 必须专用于自动巡检，处于 `main`，且不得包含已跟踪或未跟踪改动；脚本只使用 `git fetch` 与 fast-forward，不使用 reset 或 clean 覆盖现场。
- 首次运行执行全部确定性门禁和已登记基础设施集成门禁；之后以上次成功 SHA 为 `BASE_REF` 运行 `make test-changed`。每个新 SHA 同时执行根 `make build BUILD_ARGS=--force`，复验全部 Linux 产品二进制。失败不推进成功基线，后续调度必须重试同一提交。
- 测试范围与远端同步是两个正交选择：默认执行“同步 `origin/main` + 增量”，`--full` 强制全量，`--no-fetch` 跳过 `fetch` 与 fast-forward；专用机需要在当前干净 `main` 上执行不拉取的完整巡检时，统一使用 `make local-ci LOCAL_CI_ARGS="--no-fetch --full"`。`--check-only` 是独立的就绪检查，不与其他选项组合。运行日志和 `latest-summary.txt` 必须分别记录实际 `scope` 与 `remote_sync`，不能把“不拉取”误报成“增量”。
- Python 与前端依赖准备只是环境编排；真实断言仍由根 `Makefile` 与 owner 门禁拥有。
- 本地 PostgreSQL 继续只使用 `addp_test` 与 `addp_iam_test`，集成门禁严格串行。持久基础设施只启停 `addp-infra` Compose 项目；巡检专属 MySQL 和 OceanBase 由唯一的 `addp-local-macos-ci` Compose 项目管理，不操作其他 Compose 项目。脚本不执行 Docker 全局 prune，也不删除 `addp-infra` 数据卷。
- MySQL owner 门禁使用本地巡检专属、固定版本且无数据卷的 MySQL 8 服务。该服务只绑定 `127.0.0.1:13306`，必须在确定性门禁和编译前通过健康检查；外层聚合门禁只携带本地作用域标记，真实连接参数只在 `test-common-mysql-data-protection` 进程内生成，不能写入仓库根 `.env`、传给其他 owner 门禁或连接 Business/生产 MySQL。
- OceanBase owner 门禁使用本地巡检专属、固定 digest 且无数据卷的 OceanBase CE 4.4.2 LTS 服务。该服务只绑定 `127.0.0.1:12881`，并且必须在确定性门禁和编译前通过健康检查；外层聚合门禁只携带本地作用域标记，`common-oceanbase-gate.sh` 在该标记下固定生成 `addp_oceanbase_disposable` 连接参数。巡检启动前必须清除继承的 `ADDP_TEST_OCEANBASE_*`，不读取根 `.env`，不连接 Business/生产 OceanBase。
- 两个巡检专属数据库服务都必须在巡检成功、失败和中断的统一退出清理中删除。
- 辅助巡检不运行 T4 Online 或 T5 发布认证；两者继续遵循各自的专用环境、身份与报告协议。
- 日志、上次成功 SHA 与运行锁位于 checkout 的 Git 内部状态目录，不进入工作树；日志不得包含 Token 或可复用凭据。

## 八、交付与完成标准

实施前识别受影响的 T0-T5 层级。交付前优先运行：

```bash
make test-changed
```

验证指定 owner 或已提交区间时分别使用 `make test-module MODULE=<module>`、`make test-changed BASE_REF=<ref>`。受环境限制无法运行时，必须列出未验证项、原因以及负责验证的现有 CI 门禁。

测试体系变更完成必须满足：

1. 新逻辑只有一条标准入口，旧入口、变量和旁路已删除。
2. owner、测试层级、数据与身份边界明确。
3. 本地标准入口、CI 编排、登记检查和文档同步。
4. 最小充分门禁真实执行，未以 Skip、旧进程或错误环境冒充通过。
5. 运行产物位于操作系统临时目录或 CI artifact，仓库无残留。

### Business Redis 门禁

`make test-business-config` 覆盖脚本语法、固定官方镜像、回环绑定及实际端口首次避让与重启复用。`make test-business-redis` 属于 Business owner 的独占 Compose T2：随机密码、随机回环端口、私有数据卷，实际验证 ACL、string/hash/list/set/zset/stream、TTL、二进制 key/value、大整数文本、幂等初始化、类型冲突拒绝和 AOF 重启持久化，退出核对容器、卷、网络零残留。新 owner 从 `ADDP_T2_OWNED_SERVICES` 声明自动发现，不要求为了测试创建新的服务模块或维护第二份 owner 清单。该门禁已登记 `make test-integration` 和 `release-and-t2-gates.yml`；它不证明 Redis Engine Plugin、Meta/Manager/Develop 或 T4 业务链路已经实现。

### Elasticsearch 门禁

- T1：`make test-common-elasticsearch-unit` 验证受限 DSL、精确读集、HTTPS CA/认证、PIT 部分失败/取消清理及通用记录预览。
- T2：`make test-common-elasticsearch` 使用固定官方镜像、独占 Compose project 与随机回环端口，创建只读用户和幂等样例，实际验证 Common、Manager、Meta，完成后检查容器和卷零残留。该脚本已注册根 Makefile、模块/变更自动发现及 `release-and-t2-gates.yml`。
- T4：`make test-online ONLINE_SUITE=elasticsearch-consumer-flow` 唯一使用 GitHub Hosted Ubuntu x86_64 临时部署，由 `online-hosted-elasticsearch-gate.sh` 复用共享 Hosted 生命周期。Business 夹具复用固定官方镜像及样例初始化，创建独占 ES 容器、随机回环端口和只读账号；System owner helper 创建非默认 Tenant、普通消费 User 与独立登记身份，通过正式 Engine API 注册引擎，登记凭据在验收前移除。普通用户只取得 System 目录、Meta 扫描/目录、Manager 预览和 Develop 查询权限，不取得基础设施管理权限。API 及同一身份的真实 Console 验证 Meta 重扫和稳定 item 身份、Manager 25 条文档与空索引、Develop ES DSL 查询，并核对 long 精度、对象/nested 结构、来源投影和排序。任何浏览器失败、报告身份不符、截图缺失或清理残留均不能通过。退出销毁业务容器、平台 Infra 和凭据，检查零残留；不使用个人开发部署或本地共享 PostgreSQL。原 macOS profile 删除，仅登记人工 `workflow_dispatch`；首次真实通过前不计为 T4 通过、不登记 schedule。确定性验证纳入 `make test-online-runner` 与现有 Online CI 自动登记检查。

### Model3D Runtime 确定性门禁

`make test-model3d-workflow` 覆盖 DAE / 3DS 首期源引用和动画边界、GLB 自包含结构、真实 PNG/JPEG 解码以及校验失败时不发布或替换已有 artifact。该入口由 `make test`、`make test-module MODULE=engines` 自动发现和 Platform CI 的 `model3d-workflow-tests` 执行；可通过 `MODEL3D_WORKFLOW_PYTHON` 指定已安装 requirements-dev 的隔离解释器。测试使用受控 converter runner，不计为专业转换器或平台真实链路验收。已登记 Make 入口的 Python engine runtime 由 Python CI 登记检查统一核对根聚合、标准环境准备、共享模块影响选择与 CI Job。

Redis 键值数据集门禁：`make test-common-redis-unit` 验证配置、认证、取消、TLS、规范键名、RESP 解码前预算、原生类型读取、Manager 路由与保护边界；`make test-common-redis` 使用独占真实 Redis 验证 Common 插件的 ACL、样本预算、精度及 stream 重复字段，System 登记与加密脱敏，Meta 的 keyspace 数据集身份、旧逐 key 目录清理、重扫稳定与失败保留目录，以及 Manager 原生预览。与 `make test-business-redis` 共用 `redis-owned-fixture.sh` 生命周期和固定官方镜像；均使用随机密码、随机回环端口并检查容器/卷/网络清理，不访问已有 Business 或 Infra Redis。消费 T2 由 Common/System/Meta/Manager/Business 变更触发，登记根 Makefile、模块/变更发现和 `release-and-t2-gates.yml`。共享生命周期通过 `ADDP_T2_LIFECYCLE_SCRIPT` 显式登记，CI 检查必须同时确认 owner 脚本调用、变更输入覆盖、Compose 归属和真实启动/清理逻辑，不能以未调用的 helper 冒充门禁。Manager 浏览器门禁验证 `data_type=key_value` 数据集中的键浏览和原生详情，并保留二进制、大整数、score 字符串和 stream 字段顺序；不得将其计为真实部署 T4。

Redis 跨模块 T4 门禁为 `make test-online ONLINE_SUITE=redis-consumer-flow`，仅由 `online-t4-gates.yml` 的人工 `redis-hosted-t4` Job 调用 `online-hosted-redis-gate.sh`，复用 Hosted 一次性 Linux x86_64 平台生命周期。Business 物理夹具使用固定官方镜像、随机回环端口/密码和 tmpfs，复用九个原生样例，不访问开发环境。System owner helper 创建非默认 Tenant、最小权限普通用户及独立登记身份；普通用户只取得目录阅读、Meta 扫描/目录和 Manager 阅读权限，不取得基础设施管理权限。登记凭据在业务验收前移除。套件验证 System 实时 server → keyspace 目录/事实、Meta 单个 `data_type=key_value` 数据集身份、Manager 九个原生样例及 page=2 拒绝；真实 Console 浏览器从 Meta 页面发起重扫、确认 DataItem ID 稳定，打开 Manager 资源树和各类原生预览，核对同一 User/Tenant 并输出截图和报告。任何浏览器步骤失败、证据缺失/身份不符或清理残留都不能判为通过。脚本与断言测试纳入 `make test-online-runner`，现有 CI 自动发现登记该 Hosted profile；首次真实 Hosted 运行成功前不计为 T4 通过，也不进入 schedule。

Meta 专业扫描执行读取（2026-10-04 已确认）：直接 HTTP 回归覆盖当前 Owner Permission、合法 OAuth User、本人一次性执行、缺失发起人、同 Tenant 不同 User、跨 Tenant、跨模块/任务类型 UUID、已删除定义历史、分页总数及正确 404；TaskProvider 单独覆盖专用 Client、机器 Permission 与其他 User 发起执行。`make test-module MODULE=meta` 自动发现 API T1 与前端 T3；`make test-meta-postgres` 显式选择 `TestMetaScanExecutionReadAgainstPostgres`，事务回滚测试事实，不创建临时 database。现有 Release/T2 工作流自动登记此标准脚本；真实认证、Gateway 与 Worker 的直接 Meta 检查扩展既有 `orchestrator-execution` 手动 Hosted T4；[37166379368](https://github.com/pampa0629/addp/actions/runs/37166379368) 已在 `60faf18da` 上通过 30 项检查，包含专业入口的任务历史、本人一次性执行、列表总数、跨模块 404 与删除定义后详情保留。OAuth User 和 TaskProvider 权限矩阵由 T1/T2 直接 HTTP 回归覆盖，本轮 T4 不声明相应真实身份验收。

Manager mosaic whole 身份与完成边界（2026-10-16 确认）：父执行在原 lease／超时内等待原精确 Meta 扫描成功并取得真实 whole 身份，耗时包含扫描，不以 Runtime manifest 物理路径作为公开输出。`make test-manager-backend` 和 `make test-manager-postgres` 的 `TestIntegrationPostgresManagerMosaicWholeResultLifecycle` 覆盖等待期间保持 running、唯一正式输出、扫描失败／超时／取消、读取拒绝、错误目标／manifest 身份、缺失 item/fingerprint、截止时间后的受租约保护收尾及旧 lease 禁止成功。Meta 模块 T1 复用同一直接 HTTP 权限矩阵；`make test-meta-postgres` 同时运行专业读取和 `TestMosaicScanWholeResultAgainstPostgres`，验证专用 Client/Permission、跨租户、父子与 attempt/精确目标绑定、过期父 lease、闭合响应、S3/NFS 真实资源树身份、旧扫描事实拒绝、进度保留来源回执及扫描终态原子保存。Meta 专业读取的 PostgreSQL 夹具在事务内清空 Common 执行存储三表并调用生产 `execution.EnsureStore`，从空表运行正式 SQL 迁移；退出回滚并校验原表 OID 恢复，不借用本地残留约束、不自写约束替身。半套主体字段由正式 PostgreSQL 完整性约束拒绝，SQLite 验证接口拒绝，不移除或放宽归属断言。新增 Go 测试沿模块与 Platform Go workspace 自动发现，Meta T2 沿已有脚本登记进入 Release/T2，无新 workflow。共享 Client 扩散由 `make test-go` 覆盖；接口同步两模块 Swagger 并运行覆盖校验及 `make test-authorization`，新增夹具纳入 `make test-execution-fixtures`。真实容量复验仍由栅格会话同步计时说明并执行既有 `raster-workflow-capacity`，保留原预算、物理 oracle、扫描、Monitor 和清理断言；确定性门禁通过不代表完整 T4 通过。

Develop 产物自动扫描来源回归复用 Meta 专业读取的同一直接 HTTP T1/T2 夹具：覆盖 Develop 服务与 Permission、父执行及产物绑定、同租户原发起人读取、其他 User 列表总数不增加、跨租户拒绝、错误模块／任务类型／状态／主体来源和未声明目标拒绝、客户端自报主体或授权字段拒绝、失败请求不落子记录、安全投影不输出配置或授权。`make test-meta-postgres` 继续运行 `TestMetaScanExecutionReadAgainstPostgres`；Go 文件由 `make test-go`、Meta／Develop 标准模块入口和现有 CI 自动发现，无新增入口。Develop T1 验证产物先持久化再提交父 UUID，保存失败或父状态不可用时不发请求。真实 User／Gateway／Worker 的原故障复验使用已登记 `raster-workflow` Hosted T4，不以确定性测试代替。

Develop 自动扫描归属的原故障已在 `015dcb90a` 的 [Raster Hosted T4 37190920380](https://github.com/pampa0629/addp/actions/runs/37190920380) 复验闭合：归档中五次 `develop.workflow.produced_target` 扫描完成，八次扫描 execution 详情读取均为 200；既有首次创建、冲突保护、替换和两种 mosaic 链路已越过自动扫描读取检查。六个参与服务的构建提交均为该版本。本轮整体仍为失败：`resample-size` 的 Meta TIFF 元数据未满足 COG profile hint 断言，后续 grid 场景未完成，不计为完整 Raster T4 通过，也不宣称扫描范围的资源权限闭环完成。直接接口的同租户其他 User、跨租户、拒绝来源及安全投影矩阵由本轮 `make test-meta-postgres` 覆盖；包含实现的 `f2ae733d4` 的 [Platform CI Go workspace](https://github.com/pampa0629/addp/actions/runs/37189883711/job/111399672500) 已通过全部 22 个 Go 模块，但整次 Platform CI 因 GeoPython GDAL 测试失败，不计整体通过。

本轮本地验证分别记录：独立 `make test-meta-postgres` 与 Meta Swagger 路由覆盖通过；`make test-module MODULE=meta` 在共享平台 T0 的 Hosted 夹具超时及 Transfer 字段血缘 CI 登记回归失败处中止，后续模块 T1/T3 未运行。工作区 `make test-go` 被未提交 HDFS 插件的 `TestPluginSensitiveFields` 阻断；这与上述已提交版本的 22 模块 Go CI 结果不同，均不得混记为全工作区门禁通过。

Meta 扫描范围精确化门禁：直接 HTTP T1/T2 验证产物完整 locator 冻结到 execution config，非法／跨引擎目标返回本地化 400，失败请求不落记录；Go T1 验证配置 JSON 持久化、去重锁身份以及表、集合、图、直接叶子和 file/object 内容边界。`make test-meta-postgres` 增加 `TestPreciseCatalogScanAgainstPostgres`，复用同一运行时用例与事务回滚，验证局部扫描成功或失败均不改变祖先扫描事实、兄弟 item 保留，显式完整范围仍可清理缺失项。路径中的点号不得截断，空目标不能退回全引擎扫描。标准模块入口与 Release/T2 已自动覆盖 Meta Go 文件及此脚本，无第二套 CI 登记。

公共表格 Provider 的单表解析和事实读取统一使用必需的 `GetTable`，删除 `findTableInfo` 的父级枚举路径；PostgreSQL、MySQL、Oracle、ClickHouse、Doris、TiDB、OceanBase 同步实现目标名称谓词。T1 分别检查四类原生 SQL 的参数绑定、目标谓词、源目录过滤及错误直接传播，并检查解析不读取字段、缺失／不匹配目标不进入详情。Common PostgreSQL 门禁的 `TestIntegrationPostgresCatalogPreciseReadOnly` 使用真实只读源角色，验证单次目标查询、轻量摘要、主键详情、未授权／不存在目标 NotFound 及撤销 schema USAGE 后不可见。

Meta PostgreSQL 门禁登记 `TestPreciseNativePostgresScanAgainstPostgres`：使用真实 PostgreSQL Provider 和只读源角色扫描 A，显式禁止父级 `ListChildren`，同时核对 B 元数据及父级扫描事实保持不变；不可读 B、缺失目标、撤销源 schema USAGE 后的扫描必须失败。它属于源 Provider 与 Meta 持久化链路的 T2，不等于 HTTP／Worker 的 T4，也不证明其余厂商真实服务均已验收。System 当前资源授权、撤权后 Worker 重核及 metadata 可见性仍属于后续授权阶段，不把本期范围测试、此前 Monitor T4 或产物归属 T4 当作该权限闭环通过。

本期 Meta 工作区验证（2026-10-04）：修复首轮自身编译错误后，完整 `make test-module MODULE=meta` 返回 0，包含平台 T0、Meta 全量 Go T1、13 项前端测试与构建、既有 PostgreSQL 门禁及新增四类精确范围持久化回归；Swagger 生成与 Meta 43 个公开路由覆盖通过。共享 T0 的 Agent Swagger 投影因其他会话的 `deerflow` 依赖未就绪产生 `WARN_ONLY` 告警，不计为 Agent Swagger 验证通过。随后获准收紧公共 Provider 并补充真实源范围回归；后续 Common 与 Meta 门禁结果见本节下方，完整资源权限闭环仍待授权阶段完成。

公共 Provider 收敛后的工作区验证（2026-10-04）：`make test-go` 的全部 22 个 Go 模块通过；`make test-common-postgres`、独立 `make test-meta-postgres` 及完整 `make test-module MODULE=meta` 返回 0，后者包含平台 T0、Meta Go T1、13 项前端测试与构建和新增真实源贯通回归。`make test-changed` 因其他 Owner 的 PostgreSQL DSN、MySQL／OceanBase 测试环境缺失，在预检时中止，不计为通过。本地未运行其余厂商真实服务、其余消费者 T2 及真实 HTTP／Worker T4；已登记的 Platform CI Go workspace、Release/T2 各 Provider／Owner 门禁承接共享依赖扩散验证，T4 仍须相应 Hosted 验收。本轮没有重启开发服务。


### 平台指标设施独占门禁

`make test-monitor-metrics` 由 Monitor owner 自建 Prometheus、受控指标源与实际私有 TLS 转发入口，登记固定镜像 tag/digest、共享部署配置和生命周期输入，随机 Compose project、回环随机宿主端口及私有时序卷。原生 OAuth/HTTP SD 经正式转发模板运行，验证固定路径/方法/查询参数和匿名请求拒绝，四个 owned 服务均纳入退出清理。实际验证 mTLS、采样周期与超时、单端点样本越限、断开恢复、SIGKILL 后 WAL 样本重放及退出零残留；同一来源还重新部署为 Docker Desktop 受限层，验证七采集器、Engine CPU/内存/内核对照、不同 PID/挂载/网络命名空间的磁盘原始计数前后快照对照、禁用文件系统/网络族、五项磁盘即时值及精确设备趋势、mTLS 与唯一 HTTP SD 查询和恢复后新磁盘样本。本地 macOS 执行证明 Desktop VM 采集，Linux CI 证明受限层语义，不冒充 Desktop 环境或完整 Linux 节点验收；临时 CA/私钥仅存在于本轮临时目录。根 `test-integration`、owner T2 自动发现、变更输入选择与 `release-and-t2-gates.yml` 使用同一入口。`test-dev-lifecycle` 验证部署未选择、错误选择、证书缺失和可选启动故障的核心 Infra 隔离。此门禁不替代后续节点绑定、HTTP SD 授权、业务埋点、资源查询及规模验收，不宣称完成 7 天物理保留时间窗验证。

### Manager Raster Mosaic 原生 Runtime 门禁

`make test-raster-mosaic-runtime` 是 Manager-owned Python T1 入口，覆盖已有瓦片渲染单测及真实 GDAL/COG、NumPy、坐标系与瓦片输出。`make test-module MODULE=manager` 自动发现该具名 Runtime 入口；Platform CI 的 Raster Mosaic job 按 Manager 变更选择，使用独立 Python 3.12 环境和匹配原生版本的 GDAL 绑定。共享 GDAL 生命周期修复另由 `make test-dev-lifecycle` 覆盖来源选择、失效绑定重建、环境隔离及停止归属；涉及共享入口时同时运行 `make test-geopython-workflow`。

受保护资源有效期与筛选、银行卡号元数据识别扩展：`make test-module MODULE=security` 继续自动发现 Go T1、前端确定性/浏览器/构建及既有 Security PostgreSQL T2。`TestEnrollmentLifecycleAgainstPostgres` 增加与 SQLite 共用的字面子串转义、引擎/状态交集、分页总数、租户隔离以及有效期起点/截止点/退出状态回归；不新增测试数据库或入口。银行卡号能力 T1 验证元数据正例、模糊名称及非字符串反例，并沿正式发现执行将结果编译为四 Owner 规则。前端 T3 验证已同步投影的独立到期警示、日期展示、重新发现入口保留及服务端筛选/重置；使用隔离模拟 API，不计为真实部署 T4。API 字段与筛选参数由 Security Swagger 生成及路由覆盖验证。
