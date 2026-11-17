# ADDP T4 Online 验收规范

[返回测试与验收总入口](../addp测试与验收规范.md)

部署 profile、Runner 与工具链版本以 `.github/workflows/online-t4-gates.yml`、Online registry 和当前 Host Gate 为准；Hosted 与 self-hosted 不能共用准备或清理假设。以下按场景归并已有行为契约，每项条款仅适用于其声明的 suite 与 profile；代码就绪不表示真实 T4 已通过。

## 专用环境

T4只在隔离部署执行，profile由 `.github/workflows/online-t4-gates.yml`、Online registry及Host Gate决定；Hosted／self-hosted不共用准备和清理假设。

| profile | 专用准入与退出范围 |
| --- | --- |
| 常规self-hosted | `self-hosted`／`macOS`／`addp-online` Runner及同名GitHub Environment，稳定Tenant／User／Engine夹具；独立设备账号／checkout。应用退出停止，专用Infra可常驻。 |
| 商业License owner-managed | 专用Linux＋owner＋声明架构标签、受保护Environment审批；KingbaseES固定X64，DM8 ARM64须匹配认证主机。`GITHUB_ACTIONS=true`、`RUNNER_OS=Linux`、`ADDP_ONLINE_OWNER_MANAGED=1`、仓库外owner-only配置及介质／License SHA-256均核验，与Hosted互斥。不回退macOS／Hosted，Job退出销毁Infra。 |
| GitHub Hosted | 仅已声明Linux/CPU限制或可从零建完整夹具且无永久身份的suite；干净 `ubuntu-24.04` x86_64临时checkout，逐轮创建并销毁Infra／身份／Engine／业务引擎，不用Environment或仓库Secret。额外核 `GITHUB_ACTIONS=true`、`RUNNER_OS=Linux`、`ADDP_ONLINE_HOSTED=1`。 |

三profile共用 `make test-online` 分发器与owner断言；workflow仅调用既有Infra／开发生命周期及标准入口，不复制启停／业务逻辑。全部要求：

- 不复用个人工作区、进程、会话或网络。公开仓库self-hosted须可独立重置，无个人Apple ID／SSH Key／浏览器凭据或个人／生产网络访问；只允许受保护Environment的 `workflow_dispatch` 及已毕业suite固定schedule，禁 `pull_request`／`pull_request_target`／push／Issue及外部可改动态workflow。`GITHUB_TOKEN` 仅 `contents: read`，第三方Action固定commit SHA，接单前人工批准。
- 服务只绑定Runner可达回环，预检拒外部地址；`ADDP_ONLINE_HOST=1`，任何启停重启前先只读准入。
- 根目录无T4 `.env`。self-hosted长期夹具凭据仅专用设备仓库外owner-only文件／加密存储；Hosted仅当次 `runner.temp` owner-only材料，均不接收长期仓库业务Secret。
- 标准T4的 `POSTGRES_DB=addp_online`，拒 `addp`／`addp_test`／`addp_iam_test`，不属本地共享PG清单。证据在仓库外且不归档凭据目录；构建身份与干净checkout一致。

下列本机技术例外均非正式Linux Runner T4，不能推广到其他引擎或伪造GitHub profile：

| 例外 | 独有约束与证据边界 |
| --- | --- |
| DM8试用 | 正式ARM64主机未就绪前，仅macOS ARM64 Docker Desktop技术T2/T4；数据库及所有加载官方Go驱动的进程在 `linux/arm64` 容器，System可原生只做登记。非认证ABI／非长期／不可调度；到期、架构／驱动摘要错误或介质缺失即失败，不Skip／换镜像驱动，不登Hosted／schedule。 |
| `make test-manager-sgm-local` | macOS/Linux ARM64＋本地Linux ARM64 Docker；已提交源码的仓外副本，正式Compose／产品构建，独占平台、双Runtime、PG／Redis／双MinIO、普通User。Runtime／存储仅内部网，Nginx／System／Gateway／PG另接独立入口网，宿主只回环；构建前核真实端口映射／HTTP准入。SDK／许可／样例仅本机私有，报告只留哈希／断言，不留原日志／截图／样例。System本机标记仅既有Manager十权限，与Hosted／owner-managed互斥、不授个人生产权。核生成／GLB页面／缓存／源不变及领域物理清理；容器网卷／凭据／端口独占且逐项核清。runner沿 `make test-manager-online-runner`，公共workflow禁止真实入口，正式商业T4仍走上表。 |

Hosted Manager经开发入口启动的PointCloud／Document为原生Python进程，按实际回环端口、PID／监听归属管理，访问宿主System／Infra／Business MinIO不改Docker host gateway。详见[显式Runtime](../../../scripts/docs/启动脚本使用指南.md#显式启动-runtime)、[端口／宿主发布绑定](../addp端口分配.md#system-addp-基础设施)及[点云与PPTX场景](scenarios/Manager文件与预览.md#点云与-pptx)。`make test-dev-lifecycle` 或依赖准备不代验真实对象／COPC／PDF／页面；产品容器仍按各profile，不据此统一host网络或用bridge host gateway替代回环实际可达路径。

各suite夹具和断言见[场景索引](../addp测试与验收规范.md#online-场景索引)，条款只适用于声明范围。

## 正式身份的本地 Trace 技术验收

`make test-monitor-traces-local` 仅人工本机一次性技术验收，验证正式密码／MFA、Orchestrator→Develop bounded query、Platform Trace与精确日志；不属GitHub profile、不进workflow／schedule、不代正式Runner T4。身份／资源／停止线／清理唯一沿[运行观测规范](../addp平台运行观测规范.md#正式身份的本机隔离闭环)，System夹具仅本机标记＋`execution-trace-local`，不借SGM例外／扩大生产权限。

直接新建的版本核对／构建探针／容量辅助容器也须显式覆盖本轮Compose project／辅助service标签，不能继承个人镜像标签而被Infra发现为正式引擎；业务容器仍由本轮项目管理，归属拒绝与清理不变。

## Catalog 本地隔离浏览器验收

`make test-catalog-browser-local` 仅本机一次性浏览器，不属GitHub T4、不调用 `make test-online`、不重跑既有API T4。已提交源码／正式产品构建和Compose，独占System／Gateway／Catalog／Standard／Model／Console及必需Infra；账号／样本／页面断言沿[授权与共享场景](scenarios/授权与共享.md#catalog-本地普通用户浏览器复验)。

| 边界 | 必需判定 |
| --- | --- |
| 数据／运行 | 唯一Run ID、容器网卷及回环端口；**独占PG容器仅addp_test**，不是共享addp-postgres。仅模板配置，拒外部连接／GitHub或他profile／日常.env；只读记录个人容器身份并核未变。 |
| 浏览器 | 每轮唯一 `*.localhost` 与精确Origin隔离Cookie／Storage／跨标签通信，不把端口不同当Cookie隔离，不改hosts／个人登录态。 |
| 身份 | `catalog-browser-local` 在任何写入前核库名、Run ID对应 `cluster_name`、私密目录及空User；正式IAM服务建租户／限时普通用户，禁SQL写IAM／直签Token。Owner API造样本，准备身份页面前回收，普通用户真实密码／AuthContext，禁mock／注Token／个人管理员。 |
| 租约／退出 | 主入口准备／等待／清理，默认页面60分钟、上限120分钟，独立看护处理父退出；成败／超时／信号均清本轮服务／PG及历史／网卷／镜像标签／凭据／副本，不全局prune或接管个人服务。未清即失败；故障恢复用 `make clean-catalog-browser-local CATALOG_BROWSER_RUN_ID=<id>`，不谎称零残留。 |
| 证据收尾 | `make finish-catalog-browser-local CATALOG_BROWSER_RUN_ID=<id> CATALOG_BROWSER_EVIDENCE=<证据文件绝对路径>` 接收观察，须同run_id／commit／Opera且[全部页面检查](../../../scripts/test/catalog-browser-local-gate.py)为布尔true；错轮／版本／浏览器或缺项拒绝。结束命令成功只表示接收，主入口另核源标准不变并清理，以其退出／最终报告判定。 |

`make test-catalog-browser-local-runner` 沿既有Catalog runner、System标准夹具测试验证准入／权限／生命周期／登记；真实Opera不进workflow／schedule。报告只留提交、身份权限／关系／页面／清理摘要，无凭据Token或认证原响应；就绪、收尾命令或本机通过不升级为Hosted API T4／全量跨域验收。

## 开关、身份与拓扑预检

标准T4业务前一次完成：`ADDP_ONLINE_TEST=1`、非默认 `ADDP_ONLINE_TEST_TENANT_ID`（禁Tenant1）、全局唯一 `ADDP_ONLINE_TEST_RUN_ID`（未提供由分发器生成）；全部参与HTTP服务 `/health/live` 可用、构建ID／commit／指纹同checkout，`/health/ready` 返回200。同suite＋Run ID的进程锁覆盖预检、业务及报告。

System AuthContext必须先证明principal／context／Tenant／client／token／Permission，再创建资源或改注册拓扑。User／Service Principal／OAuth Scope最小权限，无个人会话／生产扩权；平台管理员Token仅限[节点控制面例外](scenarios/平台观测与语义生命周期.md#platform-node-metrics)、`platform-administrator-governance`、`module-registry-recovery` 的一次性Hosted三员，不推广其他suite。

断言只走唯一合法身份链，不在User／Service Token／直接SQL间回退；Repository／Migration数据库夹具是T2，不代API授权。源授权／恢复／节点专用身份与拓扑沿[对应场景](../addp测试与验收规范.md#online-场景索引)。

## 数据、超时与清理

夹具优先Owner正式API，API无法建立前提才用Owner专用helper。Hosted空库无User时System helper仅在GitHub Hosted Linux＋`addp_online` 调正式IAM Service创建本轮Tenant／User／Role／Session，禁SQL写Principal／Role／Assignment／Token。跨模块不得常规SQL造夹具；self-hosted永久Engine用[预置夹具](#专用环境)，Hosted当次Engine随卷销毁。

- 资源名绑定Run ID，总场景／HTTP／路由和lease收敛均有明确期限；仅临时传输或规范允许的异步收敛可有界重试，业务冲突／权限／409不自动重试，专项更严要求优先。
- 成功、失败、超时及中断均执行owner清理并核零残留；业务通过但清理失败仍失败。profile允许保留的专用Infra／历史不能为凑零残留直接删除。
- 故障注入仅测试Transport／Provider／明确hook，不在生产留调试分支。场景内恢复、正式删除／登出、外层部署销毁分别取证，不互相替代。

## 报告

分发器在仓外生成同结构成败 `addp.online-gate/v1`：suite／scenario／Run ID／commit／工作区状态，脱敏服务地址／构建身份，Tenant／数据库类别、阶段耗时／稳定码、owner创建／清理／零残留证据。报告、日志、Artifact不得含Token／Client Secret／完整敏感响应／可复用凭据，不提交仓库；专项私有材料及禁截图规定另行遵守。

## 触发策略

新增 T4 suite 先通过手工 `workflow_dispatch` 执行。专用 Runner 至少完成一次真实通过，并确认构建身份、清理和零残留证据后，才允许增加夜间 schedule。环境未就绪必须报告环境失败，不得 Skip 为通过。

## 专用场景

各场景的部署选择、夹具和固定断言见[总入口场景索引](../addp测试与验收规范.md#online-场景索引)。

## Graph 与 Cleanup 执行生命周期专项

仅人工Hosted Linux一次性部署，实际运行须单独授权，不以登记／runner／夜间或重跑代替。通用身份、同SHA健康／ready、私有目录和退出沿前文；固定源码结果与缺口沿[执行管理专题](../../next/ADDP任务执行管理统一收口专题.md#六剩余工作与共享环境)，最小断言沿[Owner矩阵](Owner补充门禁.md#graphcleanup-执行管理最小验证矩阵)。

| suite／范围 | 专项断言 |
| --- | --- |
| Graph：`ONLINE_SUITE=graph-protection`、`ADDP_ONLINE_GRAPH_CASE=execution-lifecycle`，分别FalkorDB／Neo4j | 同产品镜像，主副不同runtime instance及 `127.0.0.1:8186`／`127.0.0.1:18186`，lease30秒／heartbeat5秒；副实例仅本case，受控抽取器绑定Run ID／case。运行中崩溃绑定execution／task／attempt=1、真实started观测、不重放、unknown holding、迟到不改终态；非真实模型，Provider身份不足拒reopen。 |
| `ONLINE_SUITE=cleanup-execution` | System／Gateway及Meta、Manager、Transfer、Develop、Service、Quality、Graph、Asset、Model、Standard十Owner，Graph／Asset单实例，共享正式runtime服务身份、不扩业务权限。十Owner×scan/execute共20唯一UUID，核回执一致、他人拒绝、GET零写；`physical_effect_acceptance=not_run` 不得称原生效果通过。 |

`graph-execution.json`／`cleanup-execution.json` 须与stdout一致，schema正确、`result=passed`、`control_cleanup=passed`且必需checks齐全。仅控制本轮准确容器，不暂停PG；两Graph实例的地址及身份均须不同。

`hosted_destruction=required` 是外层义务：成败／超时／信号先在同私有环境执行 `python3 -m scripts.test.execution-online-support resume`，核精确暂停实例和空pins，再销毁当次应用／夹具／Infra／卷／凭据。恢复失败仍精确销毁且Job失败；无记录只表示无暂停意图，非法／他轮／替换容器不能按名恢复。场景成功不代外层退出／清零，恢复原记录／Token／PG探针／控制日志不归档。

Cleanup浏览器 `browser_revocation_refresh_pagination_late_response` 须有独立 `cleanup-browser.json` 与汇总browser完全一致；刷新及翻页分别核真实先读、精确撤权拒绝、清旧及迟到隔离，绑定身份／Run ID／request／execution。准备真实Console／System前端、生产Nginx入口及Chromium，口令／原Playwright输出仅私有。

本组不证明原生写图回包丢失、Manager／Transfer未知原生效果、同物理目标positive reopen或全故障矩阵，未执行／拒绝路径不充当其他成功能力。

## 平台三员治理隔离验收

`platform-administrator-governance` 仅人工Hosted Linux x86_64，沿前文一次性准入／销毁，不支持个人部署、self-hosted、夜间或本机生命周期替代；独占Infra／addp_online／System／Gateway／Console及System页面。平台身份只用于此suite，不扩业务suite。

正式能力沿[IAM治理与恢复规范](../../../system/docs/IAM数据模型与迁移规范.md#七bootstrap密码重置与灾难恢复)、[多标签协调](../addp登录认证的统一要求.md#四多标签页协调)；本节保留真实链路的特殊取证要求。

| 场景 | 必须实际证明与未覆盖边界 |
| --- | --- |
| 三员／治理 | System fixture正式Bootstrap建三个不同三员，正式账号／成员／MFA服务准备候选及无平台权限租户用户；公开密码／TOTP／AuthContext证明，不直签管理会话。真实Console iframe核岗位申请复核、自批／错岗拒绝、版本冲突、暂停恢复／旧会话失效、角色原子交接并交回、一次密码／MFA登记及暂停保留。退出前无待办、原岗位恢复，历史审批／审计不物理删，随当次Infra销毁。 |
| 跨标签迟到凭据 | 审计员原页批准恢复，先取得真实Gateway成功再暂扣浏览器响应；同会话第二标签退出，核旧Token失效／原页离开／旧请求取消，再以同审计员新会话登录后投旧回执，不显示旧凭据或重复批准。申请人取消并核凭据不可用；同会话正常恢复作对照。原页仍挂载时接收迟到响应由System隔离前端另验，不借本轮取代。 |
| 单会话应急撤销 | 显式权限租户申请人提交精确普通User会话族，系统／安全两员有效MFA独立批准，申请人MFA确认消费；10分钟审批／15分钟执行、一次消费原回执。浏览器执行原样送Gateway，取得真实成功后截断浏览器连接；页面保编号／提示待核清／禁止新写。确认至核清仅一次POST＋一次详情GET；独立核目标失效、同人另一会话及授权不变，审计员按原编号核申请／双批准／执行，审计无凭据／额外审批权。分项与整轮Hosted分开。 |
| 同岗多人准备／回收 | 正式申请复核给已登MFA候选增授系统岗，形成系统2人、安全／审计各1人；按Principal ID／账号／岗位绑定四人，不按岗覆盖凭据。恢复后逐人核新旧凭据／会话、授权版本／审计，再按原矩阵撤新增岗位，原三岗保留且被撤平台会话失效。 |
| Apply前中断 | 前三人完成真实双TOTP窗口，第四人输密码前SIGTERM本轮私有CLI进程组并确认退出；四人原密码／MFA／会话／角色版本不变、收集的新凭据不可用、仅prepared审计。原未消费Secret重新收齐四人完成恢复；不证明提交中或DB故障。 |
| 正式离线CLI | `make build-iam-recovery build-iam-bootstrap` 构建同部署CLI，真实PTY执行prepare/apply、隐藏密码、每人两个连续真实TOTP窗口，不加非交互参数／固定码／测试时钟。全部持有人替换凭据、旧会话失效、角色与暂停保留、Secret不可复用／Bootstrap不能重建、完整脱敏审计；暂停恢复仍按原审批。 |
| 退出后跨进程核清 | 中断后新进程仅prepared、错编号unknown；四人Apply完成且原进程退出，另进程 `status <attempt-id> <submission-id>` 不输Secret／密码／码，须 `completed_this_submission`、同提交号／四历史Principal岗位／恢复时版本，异提交号只 `completed_other_submission`。后来恢复暂停／撤岗不改旧回执，重复查询不改审计完整行／当前会话。仅证明CLI退出后核清，不代已提交成功响应截断的PG T2或跨进程网络确认丢失T4。 |

凭据／PTY内容仅进程内存，子进程退出关终端并清引用，禁参数／环境／文件／URL／日志／报告；浏览器禁截图／Trace／视频／Storage State。报告只留固定布尔／脱敏身份，恢复非敏感编号也不作为凭据副本保存。准入、业务或清理任一失败均失败，未实际Hosted不计通过。

### 原生 Cleanup 的独立有限验收

`cleanup-native-effects` 为独立人工Hosted，逻辑回执／浏览器 `cleanup-execution` 不代原生效果。六个基础case：`manager-success`、`manager-object-unknown`、`manager-index-unknown`、`transfer-engine`、`transfer-dlq-unknown`、`transfer-capture-unknown`，由 `ADDP_ONLINE_NATIVE_CASE` 显式选一个，无默认／全矩阵，每case新部署；新运行／切环境另行确认。额外晚写case单列于下。

`make test-cleanup-native-online-runner` 仅验证编排；普通User权限及case checks唯一见[机器契约](../../../scripts/test/execution-online-contract.json)，缺失／重复／跨case／伪造通过拒绝。产品归属与删除语义见[Cleanup职责](../addp-cleanup体系规范.md#三职责边界)；登记不表示Hosted已跑。

#### 身份、来源准备与实际效果

| Owner／路径 | 独有授权、事实与失败断言 |
| --- | --- |
| Manager扫描／Grant | 按当前权限生成本轮Meta，独立办理员凭规范指纹读System精确DataItem并办既定源Grant；普通消费者有Grant后才枚举、核full_name／fingerprint并准备Model3D。指纹不证明权限／身份／存在，冲突／拒绝／错对象即停，不扩权或借机器身份。 |
| `manager_protection_readiness` | 原User以 `manager.derived_artifact.read` 查本轮指纹GLB列表，仅403／`security_protection_required` 可继续等；其他拒绝即失败，200后只发一次扫描。非内容读取，不代Grant／库存。 |
| 原件与真实GLB | Meta仅结构身份，不要求size_bytes／网格／内容派生。原User下载OBJ／FBX核固定长度摘要，OBJ完整模型／MTL／纹理ZIP，目录大小不代字节；共用GatewayClient的raw／headers及Content-Type JSON，无新身份／旁路／重试，ZIP/GLB不走控制面JSON客户端。正式Grant／Model3D后清理，独立核输出对象／索引及原件不变。 |
| Manager unknown | 核proxy回执／对象与索引hold／原生索引缺失及前后事实。`manager_unknown_engine_read`／`manager_repeated_delete_refusal`／`manager_new_generation_refusal` 分开记录，快显生命周期冲突409。索引隔离精确绑定Tenant／Engine／物理索引、原execution/attempt、出口代次端点、prepared操作／唯一删除投递目标及hold。原只读历史可推进，但不表示完成或允许重发；前后全事实仅排除瞬时unknown诊断计数，原11断言及顺序不变。 |
| Transfer Engine／Capture | Engine删除保留并禁用定义／DLQ，显式删任务才清DLQ，业务行不变。Capture case核准确Tenant／任务、无调度continuous在创建及留存读回均boolean `enabled=false`、原生运行体已停、unknown持久隔离及新producer拒绝；禁用不等清理成功／运行预期。 |
| Transfer拒绝／授权范围 | producer围栏必须409／`transfer_cleanup_unresolved`，报告含 `producer_refusal_http_status=409`、`producer_refusal_error_code=transfer_cleanup_unresolved`，error为安全本地化文案，500／他码不算；DELETE外部清理失败仍503。Engine／Capture两case按现有Tenant／任务权限，不准备无消费链源Grant或调Manager同步检查；CDC靠正式runtime／独立行探针，不认证当前Grant/Deny／撤权／运行中收权闭环。 |

#### 期限、阶段与读取约束

所有新增诊断只复用下表允许的原窗口内请求，禁止扩权／加窗／补读隐藏Owner或私表，失败不改变原物理成功谓词。

| 环节 | 保留预算、绑定及失败条件 |
| --- | --- |
| 构建／整轮 | 构建 `--jobs 1`，实时产品及委托Runtime日志，固定编译／镜像／启动／身份／业务阶段；原退出码进入销毁链，无额外秘密。workflow120分钟／业务900秒，缺终态／清理不通过。 |
| Manager准备 | 保护就绪／精确扫描／唯一MinIO active＋online共180秒绝对截止；原Engine User仅GET当次实例，单请求min(60秒,余额)，禁连接测试／扩权／加窗；晚返／耗尽不扫描或准备模型，失败只用已取安全readiness。 |
| CDC准备 | 两case扫描＋PG/Kafka active／online共180秒，原User GET核Tenant／ID／类型后才建任务，无连接测试／扩权／重试创建；失败留最后闭集身份状态，创建400留准确源目标／拒绝，启动仍120秒。 |
| 初始行 | 原SQL／精确行值／120秒不变，非零仍false；只不匹配且有余额时原Transfer User GET同execution，≤60秒及余额，耗时含原窗；失败不补读／重发或改物理成功。 |
| 评估／删除阶段 | 原请求标评估受理读取／删除确认／执行观察／墓碑；Transfer另核停止、定义DLQ保留、专业结果／删任务／复核／业务行，原七项齐全才判，不以空checks充部分通过，禁原URL／请求体／确认名／错误正文。 |
| 评估观察 | 120秒仅原GET最后观察；请求／Engine／原User／scan／Engine分支完整七Owner全匹配后，仅投影授权可见Manager／Transfer子项，不补读专业结果。 |
| execute观察 | 360秒及原Engine GET／成功谓词不变；无execute引用且有合法validation scan与余额时，同User才读Engine发布的validation投影，单次限原HTTP及余额。有execute只读execute；validation终态不代execute，读失败不覆盖已有安全观察或改等待判定。 |

#### 失败诊断的唯一投影

字段仅相应阶段失败时允许，成功报告即使空值/null也拒绝；只分类既有异常／回执及上表明确许可的窗内读取，不加请求／身份／重试／等待，不覆盖首败。禁止原错误正文、URL/query/路径、连接配置／凭据／敏感标识；未知只闭集归一。

| 字段／阶段 | 唯一投影与白名单 |
| --- | --- |
| `model_prepare_diagnostic`：仅 `manager_model_prepare` HTTP异常 | 仅operation＋error_code。操作：`meta_items_read`、`manager_model_results_read`、`manager_model_tasks_read`、`manager_source_download`、`unrecognized`；错误：`permission_denied`、`security_protection_required`、`source_authorization_required`、`source_composition_incomplete`、`engine_unavailable`、`authorization_service_unavailable`、`download_bundle_invalid`、`download_source_changed`、`unknown`、`unrecognized`。缺码unknown，其它unrecognized。 |
| `manager_inventory_diagnostic`：`manager_native_inventory` | probe限storage_reference/native_object/index_projection/artifact_state；reason限private_read_failed/storage_reference_missing/storage_reference_invalid/storage_reference_outside_scope/object_empty/object_unproven/index_response_incomplete/index_scope_mismatch/index_not_observed/artifact_not_live/http_error/unrecognized。HTTP异常可含数字http_status，读取序／范围／等待不变，无私有引用。 |
| `manager_unknown_diagnostic` | probe限proxy_receipt/object_hold/index_hold/index_absence/engine_read/repeated_delete/new_generation/facts_recheck/proxy_recheck。原index_unknown/index_targets/index_holds及forwarded仅zero/one/multiple/unrecognized，binding_matches仅布尔，upstream_response仅true/false/unrecognized。可复用原execute已得engine_cleanup_diagnostic，不补读／存绑定正文。 |
| `initial_records_diagnostic` | CDC/Kafka类别、物理查询失败／行值不符／夹具失败及窗内最后execution；核任务绑定、闭集状态／安全码／停止恢复扫描元信息，最近读失败另列不盖已得观察。无SQL／行值／stderr，不证明Capture、行或删除通过。 |
| `engine_assessment_diagnostic` | 本轮请求ID、父闭集状态、请求／Engine／原User／scan／七Owner匹配布尔；完整匹配才给可见子闭集状态、attempt／开始、回执状态／安全原因及准入授权拒绝／不可用。读前失败记未观测，非法结构／错绑定另分，无时间戳／Metadata／统计。 |
| `engine_cleanup_diagnostic` | 最后同Engine闭集lifecycle、四固定deletion_error／空／未识别、两引用合法存在；最后validation/execute安全观察及最近读失败。沿assessment绑定且action准确，无名字／时间戳／Metadata，原六权限／七断言不变。Manager分 `manager_cleanup_engine_facts`／`manager_cleanup_inventory`；Transfer仅计划连接／停止／Capture原生步骤／DLQ提交，须同Owner／当前attempt1／已开始failed／唯一step与failed/terminal一致，阶段集合沿Cleanup规范。 |

#### 故障代理与退出清理

四个一次性代理在应用前固定指向当次Infra，只对准确关联操作真实变更一次后丢回包并阻重发；控制端点仅容器loopback。Kafka内部advertised listener过代理，外部listener给独立只读探针。秘密manifest／描述符／proxy配置／展开Compose保持私有0600、不归档。

退出先停产品／Worker／Runtime，再销毁proxy／夹具／Infra；恢复、销毁或清零失败均令Job失败。六基础case不证明旧writer晚写、写图丢回包或positive reopen，保留 `not_run`。

#### 额外显式专项：Manager 晚写

`manager-late-writer` 仅证 `manager_glb_upload_after_owner_crash`，不扩六基础case。计划直接将 `MINIO_ENDPOINT` 给Model3D原生S3，proxy仅对当次pending StorageRef精确PUT设私有有界barrier，无产品路由。仅本case设 `MANAGER_EXECUTION_LEASE_DURATION=30s`／`MANAGER_EXECUTION_HEARTBEAT_INTERVAL=5s`。

完整上传已进入且尚未发送后，才终止／补回当次Manager并等旧lease失败；Engine评估因未决writer阻断、禁DELETE。释放**同一PUT**后独立原生读核迟到对象出现，原failed attempt、pending审计及新generation围栏不被覆盖；不外推Graph／positive reopen。

同次 `engine_task_observation` GET须准确请求／Engine／原User／scan／完整Owner，子项唯一UUID、精确父execution／cleanup_executor。Manager冻结计划拒未决产物时必须attempt1、已开始failed、唯一 `manager_cleanup_plan` 失败阶段且无伪造回执；父原30秒截止后timeout。他Owner失败、未开始／错attempt／阶段父子缺证或授权拒绝不可用均不替代；失败只用原GET assessment，不补隐藏Owner／私错。

原11项目标、请求读次数／权限、120分钟部署／900秒业务、180秒barrier／90秒租约收敛及其他等待不变；barrier到期失败销毁，不续期重发。退出恢复当次暂停实例并释放barrier，控制状态／body／私有计划不归档。实际Hosted仍须单独确认。

## Redis R1 固定产品隔离验收

`infra-redis-core` 仅人工 Hosted Linux x86_64，固定产品 `690754af4890f493e5ec3fe560e7a5d12ba60c9d` 与验收检出分离；编排、Owner identity 补丁和观察器不得进入产品服务。详细阶段、35 秒原窗口截止、权限和拒绝谓词见[解耦设计第 4.6 节](../../next/平台数据库与对象存储解耦设计.md#r1-固定版本隔离-http-t4-编排已审核编排实施中未实跑)。

System 专用准备夹具调用固定产品现有领域服务创建零 Role／Permission 的 Tenant Service Account 和精确 Query Consumer；不发行管理员 Token、不 SQL 写 IAM。Query 普通准备用户沿既有源 Grant、Meta 和 Security 链路；C 配额2、原固定60秒窗口、Gateway与System分别断Redis、原Token复用与窗口到期不变均须真实HTTP证明。HTTP透传只计实际request_id／方法／路径，不模拟Backend或放宽Ready；后台无ID请求与必需探针分开，重复相同响应ID只核成同一身份，不接受不同ID。

`make test-redis-core-online-runner` 为确定性编排／固定身份编译门禁；`make test-redis-core-wiring` 为复用独占Redis和标准IAM PG锁的局部T2，完整门禁归 `make test-common-redis-core`。T2在独立子进程运行固定源码的System／Gateway生产Router，证明身份与代理接线，不代表产品main启动、真实Service Query或完整T4通过。新Hosted workflow仅手工派发，实际运行须另行批准，不登记夜间计划。业务清理、历史随自有Infra销毁及外层零残留分别报告；不得接管个人环境或执行keepalive。
