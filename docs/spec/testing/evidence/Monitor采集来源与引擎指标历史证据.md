# Monitor 采集来源与引擎指标历史证据

[返回 Monitor 历史证据](Monitor历史证据.md) · [返回验收证据索引](../验收证据索引.md)

本页只保留主题结论、实际验证范围、关键证据与待办。各结论绑定所列源码、私有快照或个人环境日期，不代表当前 HEAD 已重跑；确定性测试、独占实源 T2、个人 Safari 与正式身份 Hosted 分别认定。

[PostgreSQL](#pg-source-history) · [共享目录限制](#pg-directory-history) · [Redis](#redis-source-history) · [MinIO](#minio-source-history) · [引擎页面](#engine-ui-history) · [统一采集与恢复](#source-lifecycle-history) · [严格证书](#source-tls-conclusions) · [七来源联合核验](#seven-sources-live-conclusions) · [最新 MinIO 发现故障](#minio-helper-label-conclusions) · [待办](#source-evidence-todos)

现行契约由[运行观测规范](../../addp平台运行观测规范.md)维护，部署操作见[指标中心与来源](../../../../scripts/infra/docs/指标中心与来源.md)。本页不重复账号配方、固定查询、启动选择和私有卷规则；历史“尚未接入”“等待应用”等中间状态按后续实际结论归并。

<a id="pg-source-history"></a>

<!-- 保留对应主题旧链接。 -->
<a id="postgresql-来源身份投影与正式验收"></a><a id="postgresql-测试数据库初始化就绪竞态2026-11-07"></a><a id="infra与business-postgresql-自动接入"></a><a id="postgresql-固定指标来源与引擎身份投影"></a><a id="postgresql-采集账号认证边界"></a><a id="business-postgresql-受管认证目录"></a><a id="postgresql-采集账号安装与来源自动启停"></a><a id="postgresql-专业发现查询与页面"></a><a id="postgresql-来源准入协议修复"></a><a id="postgresql-专业观测正式-hosted-验收"></a><a id="postgresql-准入探针的构建与执行期限"></a><a id="postgresql-专业来源固定版本-linux-t2-复核2026-10-24"></a><a id="采集分组改名后的-postgresql-查询与来源复验2026-11-05"></a><a id="观测身份投影-postgresql-断言误报修复2026-10-23"></a><a id="postgresql-私有来源投影-t1-的短数字碰撞修复2026-10-30"></a>

## PostgreSQL：来源、身份与正式验收

来源与权限沿[固定来源契约](../../addp平台运行观测规范.md#47-postgresql-受保护的固定指标来源)：独立采集身份只读实例汇总及当前数据库容器段趋势，不读取业务表、SQL 正文或凭据，不把整库消耗归算租户。

| 代表性证据 | 已验范围与限制 |
| --- | --- |
| `b08f2331dc6c76b520b958dc5a3d79c054f80900`；[Hosted39683888149／Job119512936927](https://github.com/pampa0629/addp/actions/runs/39683888149/job/119512936927) | 正式密码／MFA、无权限对照、双域单容器接入／变更／回滚／退役、专业查询／趋势及停源恢复通过；业务与外层清理通过，Infra 零残留。不覆盖后续主机导航、其他集群、自然留存或 T5。同源码[来源T2](https://github.com/pampa0629/addp/actions/runs/39683857764/job/119520816486)曾在冷准备期限失败，不能借 Hosted 改判。 |
| `5d91fa31c326fc636c73810edb542ae7f95e4db0`；[首轮Job120735545333](https://github.com/pampa0629/addp/actions/runs/40048425338/job/120735545333)／[第二尝试Job120741235374](https://github.com/pampa0629/addp/actions/runs/40048425338/job/120741235374) | 首轮 Compose 超时、产品采集未执行；同输入第二尝试通过双域认证、mTLS、账号生命周期、查询／统计重置、失效清空恢复及目录原子更新还原，退出容器／网络／卷／私有文件清零。只计 Linux T2。 |

个人两库在数据库重启后曾因旧启动身份失配停采，用户重新应用后新鲜值与 Safari 查询恢复；不证明停采回填或无人值守恢复。后续实际采样见[七来源联合核验](#seven-sources-live-conclusions)。

<a id="pg-directory-history"></a>

<!-- 保留对应主题旧链接。 -->
<a id="postgresql-本机恢复与共享目录取证"></a><a id="个人环境-infra-postgresql-接通与跨模块入口修复2026-10-29"></a><a id="business-postgresql-认证文件幂等发布与-desktop-挂载故障2026-10-30"></a><a id="用户标准启动后的-business-实源接通"></a><a id="个人两库来源不可用的只读诊断2026-10-31"></a><a id="用户重新应用后的两库来源与-safari-恢复复验2026-11-01"></a><a id="完整开发启动的-postgresql-来源恢复2026-11-01"></a><a id="目录认证-hba-读取取证与夹具恢复版本修复2026-11-01"></a><a id="docker-desktop-实际共享设置与普通文件对照2026-11-01"></a><a id="已有文件名的替换缓存与目录枚举对照2026-11-01"></a><a id="同进程等时与正反枚举顺序对照2026-11-01"></a><a id="无数据库的共享目录最小复现与修复建议2026-11-01"></a>

## PostgreSQL：共享目录取证与后续复验

**未关闭的来源限制**：当时 Docker Desktop 4.49.0／Apple Virtualization／VirtioFS 下，原子替换旧文件名后容器可 stat 却读不到新内容，新文件名可读；无数据库的普通文件也复现，目录枚举顺序影响可见性。未确定唯一上游缺陷或恢复 TTL，后续成功不证明共享层问题根除，候选版本专项未完成。

同内容发布保留 inode，真实变更仍原子替换、不可读仍拒绝；没有用重试、原地覆盖或放宽认证绕过。早期失败项目按归属清零，**清理不等于目录认证通过**。现行处理见[共享目录故障说明](../../../guide/troubleshooting/开发服务与构建.md#docker-desktop-共享目录原子替换后容器读不到文件)。

**最小复验证据**：基线 `4d8a8cc2e3b391cdfe2cdc46707d131397b25dac` 叠加 Compose 修复，在一次性 Lima Linux ARM64 完整通过目录认证、原子更新／重载／还原，环境与专属 VM 清理完成；该基线不是修复提交身份。11 月 8 日本机严格证书实源整轮也通过目录恢复，原记录未另列当次 SHA。

<a id="redis-source-history"></a>

<!-- 保留对应主题旧链接。 -->
<a id="个人主机与-infra-redis-的真实指标及趋势复验2026-11-08"></a><a id="redis-无定义比率的就近说明2026-11-08"></a><a id="redis-账号来源与专业查询"></a><a id="redis-固定采集与最小-acl-基座2026-11-01"></a><a id="redis-账号持久化与来源生命周期2026-11-01"></a><a id="redis-自动发现与-system-精确来源关联2026-11-01"></a><a id="redis-专业指标查询与页面2026-11-01"></a><a id="redis-查询发布前的混合摘要与超限呈现复核2026-11-01"></a><a id="redis-正式全链路验收准备2026-11-01"></a><a id="redis-来源状态与恢复的受控页面验收2026-11-02"></a><a id="infra-redis-关闭-aof-时的快照和-safari-复验2026-11-06"></a><a id="business-redis-平台接入和-safari-复验2026-11-06"></a>

## Redis：最小身份、实源与页面

授权与采样沿[Redis 正式契约](../../addp平台运行观测规范.md#redis-专业观测首期边界2026-11-01-确认分阶段实施)。采集身份仅 HELLO／PING／固定 INFO，拒绝业务键、脚本和管理命令；INFO 可见较宽实例元数据，**不是服务端字段级 ACL**。

- **本地证据**：双 Redis 7.2.13 实源验证账号隔离、重启持久化、停用旧／新连接拒绝、mTLS、发现与恢复，最终项目／私有卷／临时文件清零。本地 T1、实源 T2 和受控页面各自认定，原因提示的定向 T3 不补成完整浏览器通过。
- **缺项与数值**：AOF 关闭时两项结果允许合法留空，开启时要求同次有效结果；零内存上限、无键查询的比率无定义。缺样不补零，有效比例超过 100% 不裁剪，也不由当前缺项反推历史原因。
- **个人页面**：11 月 6 日双域不同 AOF 状态下的纳管、VM 关联、真实采样与 Safari 值／五分钟趋势通过；11 月 8 日 Infra 修证重新应用后有严格握手、无证书拒绝、内存／命令趋势及手动／自然刷新证据。缩放、全指标与故障注入未复验；误点 FieldHelp 的失败不计整脚本通过。联合样本归[七来源核验](#seven-sources-live-conclusions)。

<a id="redis-hosted-首轮失败与验收脚本修复2026-11-07"></a>

### Redis Hosted：仍未完成

固定 `42c06354af54aabaca893426c1ed1a9fe1794a44` 的 [42242305588／Job127975454221](https://github.com/pampa0629/addp/actions/runs/42242305588/job/127975454221)在双域纳管、主机绑定和身份隔离后**采样等待超时**。专业页面、Business 停采恢复及 Infra 持续采集未执行；`cleanup=failed`、`infra_cleanup=zero_residuals` 分别认定，归档缺超时逐指标响应。

验收器随后修正合法 `no_data` 拒绝与清理凭据丢失，仍要求 Business 两项有值及完整窗口；标准 runner 本地回归通过，**原记录没有修复后的真实 Hosted 通过证据**。

<a id="minio-source-history"></a>

<!-- 保留对应主题旧链接。 -->
<a id="minio-账号采样与专业查询"></a><a id="minio-指标身份生命周期与隔离权限2026-11-02"></a><a id="minio-固定-v3-指标与-mtls-来源2026-11-02"></a><a id="minio-自动发现与-system-精确来源关联2026-11-02"></a><a id="minio-固定查询与专业监控页面2026-11-02"></a><a id="minio-专项-ci-回传2026-11-02dff267ed0"></a><a id="linux-完整前端产品步骤回传2026-11-022cd45ec2"></a><a id="minio-原生计时基准差异导致整批误判2026-11-03"></a><a id="infra-minio-修复应用后的真实页面截图2026-11-03"></a><a id="infra-minio-恢复的原镜像不可用诊断2026-11-04"></a><a id="用户重新应用后的-infra-minio-采集恢复2026-11-04"></a><a id="infra-minio-重新应用后的-safari-真实页面与趋势交互2026-11-04"></a><a id="本机-infra-minio-健康来源恢复实操2026-11-04"></a><a id="business-minio-本机采集配置与证书准备2026-11-08"></a><a id="business-minio-本机实际接入与-safari-复验2026-11-08"></a><a id="minio-缺项的原生响应与采集映射对照2026-11-08"></a>

## MinIO：权限、缺样与分层证据

授权与字段沿[MinIO 正式契约](../../addp平台运行观测规范.md#minio-专业观测首期边界2026-11-02-已确认)。双域独立 `admin:Prometheus` 身份、固定 v3／mTLS 来源已有真实 T2，S3／对象／管理及跨域访问拒绝，停用失效、保卷重建和账号策略保持通过；自有容器／网络／卷／私有材料清零。

| 代表性证据 | 结论与限制 |
| --- | --- |
| `dff267ed00aef78f009dcc54ac33fe62305c7aa0`；[41424138332／Job125206871137](https://github.com/pampa0629/addp/actions/runs/41424138332/job/125206871137) | Linux 来源／身份、固定查询及退出清理通过；不含 Safari 或 Hosted。 |
| `2cd45ec2af6de07feed8e61d2df1d01962d8c0eb`；[41429831469／Job125225339390](https://github.com/pampa0629/addp/actions/runs/41429831469/job/125225339390) | 受控前端步骤含构建通过，**整 Job 因期限 cancelled**，不记整门禁或真实来源页面通过。 |
| 11 月 3 日本机运行 `eb8af602ca8a494142cbbf201104884e92cd1433` | 移除错误的启动时刻加运行时长一致性要求后，双域实源通过，个人页面显示实际值和曲线；原生计时基准差异的底层原因仍未知。 |

**原生缺项**：11 月 8 日对照原生响应未见无法解释的采集遗漏。两域缺离线盘数及部分请求计数，Business 另缺扫描字段和接收字节；固定上游省略零值，HTTP 200／UP 不保证目录齐全。缺项不能补零或推成空库／扫描失败，不切换 v2／S3 枚举补数。

**个人实测与未完成范围**：Infra 的容量／请求曲线、悬停／拖动及刷新缩放保持通过；Business 在 AKI 修正和用户重新应用后，严格握手、无证书拒绝、平台登记／主机关联及页面通过，请求趋势仍有缺点、未重验缩放。滚动／DOM 身份、逐帧无闪、全指标／七天均未证明；`timeOrigin` 仅差 1ms 不能确证重载，Safari hidden 根因未明。

11 月 4 日健康 recover 的“未重建”依据为用户输出，缺命令前容器 ID 独立对照；后续唯一目标 UP、新鲜样本另已核验。该个人样本未证跨 Docker 停机恢复，**本页没有 MinIO 正式 Hosted 通过证据**；联合页面实测归[七来源核验](#seven-sources-live-conclusions)。

<a id="engine-ui-history"></a>

<!-- 保留对应主题旧链接。 -->
<a id="引擎页面导航与刷新"></a><a id="引擎监控的当前承载主机导航"></a><a id="个人环境-business-postgresql-专业页面与趋势复验2026-10-30"></a><a id="business-部署改名与专业趋势时间缩放2026-10-30"></a><a id="safari-原生拖动复验的独立进展2026-10-30"></a><a id="safari-刷新保持与指标切换重置复验2026-10-30"></a><a id="引擎监控原位刷新2026-10-30"></a><a id="用户重启后的个人-safari-复验2026-10-30"></a><a id="引擎监控就近提示2026-10-31"></a><a id="用户登录后的个人-safari-就近提示复验2026-10-31"></a><a id="console-语言切换与窄屏提示的退出同步2026-10-31"></a><a id="菜单退出同步修复的-console-ci-结果2026-10-31"></a><a id="引擎当前对象变化的原位刷新保护2026-11-02"></a><a id="来源分区应用后的-safari-真实值恢复与人工导航复验2026-11-03"></a>

## 引擎页面：导航、刷新与缩放

交互规则沿[趋势与导航契约](../../addp平台运行观测规范.md#63-趋势时段比较与总览)。本节只区分证据范围：

- **受控页面**：主机导航以基线 `1e59fda800edf9ff1e8d33ddccb891d123b765f6` 叠加本方输入验证；刷新、对象变化及迟到／撤权隔离另有本地完整与后续定向回归，不能合称末版全量复跑。主机名仍需 `platform.host_node.read`；同 canvas 和在途保持由受控 T3 证明，离散截图不证明零闪，也不计多宿主或 Hosted。
- **真实 Safari**：Business PostgreSQL 改名后同 UUID 导航、实际值、原生拖动、刷新相对视口保持、指标切换重置及重载恢复已验；11 月 3 日另验自动筛选和返回保留 MinIO。仅当次动作，鼠标提示、全部指标／时段和长期稳定性未覆盖；七来源的采样与页面收据统一见[联合核验](#seven-sources-live-conclusions)。

首次错误标签页刷新、未完成请求不计通过；隐藏状态、缓存差异和早期超时根因未因后续成功关闭。窄屏提示修复及固定 Linux Console 证据见[节点专题](Monitor节点与主机资源历史证据.md#菜单退出同步修复的-linux-console-门禁回传2026-10-31)。

<a id="source-lifecycle-history"></a>

<!-- 保留对应主题旧链接。 -->
<a id="主机恢复跨次设备号变化的判定修正2026-11-08"></a><a id="设备号修复后的个人主机恢复复验2026-11-08"></a><a id="统一采集管理入口首版2026-11-03"></a><a id="redisminio-已安装来源的无账号修改恢复2026-11-04"></a><a id="主机来源显式安全恢复2026-11-04"></a><a id="本机主机来源应用及健康恢复2026-11-04"></a><a id="主机恢复失败原因的安全诊断2026-11-04"></a><a id="引擎来源失败阶段的中文提示2026-11-04"></a><a id="采集来源统一-compose-分组2026-11-05"></a><a id="统一分组后的标准生命周期夹具修复2026-11-05"></a><a id="统一分组的独立仓库闭包与网络声明修复2026-11-05"></a><a id="统一分组的固定版本实源结果与语法入口阻断2026-11-05"></a><a id="个人-desktop-统一分组应用与恢复复验2026-11-05"></a><a id="全量开发启动的主机恢复与监控摘要2026-11-05"></a><a id="启动前序失败与末尾现况的提示归因2026-11-05"></a><a id="主机恢复的空dns表示差异误拒绝2026-11-05"></a><a id="redisminio-同输入镜像复用与恢复实操2026-11-11"></a><a id="redisminio-私有卷工具改用采集镜像2026-11-12"></a>

## 统一采集、主机恢复与部署分组

归属、来源选择、应用／恢复及清理规则归[统一部署分组](../../addp平台运行观测规范.md#采集来源统一部署分组2026-11-04)、[引擎恢复](../../addp平台运行观测规范.md#已安装引擎采集来源的统一恢复)和[主机恢复](../../addp平台运行观测规范.md#主机采集来源的显式安全恢复)。早期带工作区摘要的项目实测不证明旧布局迁移；启动汇总不替代[七来源实际采样](#seven-sources-live-conclusions)。

| 来源／最小证据 | 独有结论与验收边界 |
| --- | --- |
| 主机与部署分组：`f9a91928eaedffbe8a1627bd238b43bddfbb000f`；[41889180935／Job126782168039](https://github.com/pampa0629/addp/actions/runs/41889180935/job/126782168039) | Linux host-resources T2 验证网络归属、limited mount、六来源查询与告警，退出清零。主机 bridge 夹具不挂载真实宿主根／proc／sys，不能算原生全宿主恢复、整 Platform 或 Hosted。 |
| 主机健康恢复：`af08710844e13c79eab7cd45fa68974dba644491` | DNS 空表示、跨次有效设备号比较的误拒绝已修正。确定性／受限来源 T2 及本机健康 recover 通过，证明文件未改写、前后容器事实保持；未制造真实重新挂载故障。 |
| Redis／MinIO 恢复与工具镜像：[记录修订388707358](https://github.com/pampa0629/addp/commit/388707358d6c49b46dcf661a9d35a00f962d0349)、[记录修订1ddd5fe55](https://github.com/pampa0629/addp/commit/1ddd5fe55b48c3312df2424fb049240f0fab9501) | 镜像索引覆盖及私有卷工具依赖修复已有 T2 与夹具清零证据；本机仅重应用两项 Infra 来源后七项 recover 成功，四库及挂载保持，不算业务重启。两项记录未另列受验 SHA／Job；阻断引擎镜像的 T2 不替代个人应用与采样。 |

<a id="source-tls-conclusions"></a>

<!-- 保留对应主题旧链接。 -->
<a id="redisminio-来源严格证书预检2026-11-08"></a><a id="infra-redis-本机叶证书修正与应用交接2026-11-08"></a><a id="infra-redis-修证后来源应用与真实页面复验2026-11-08"></a><a id="redisminio-证书预检中文诊断2026-11-08"></a><a id="postgresql-采集来源统一严格证书校验与中文诊断2026-11-08"></a><a id="主机采集来源统一严格证书预检与恢复保护2026-11-08"></a>

## 严格证书：预检与实际载入分开

证书规则归[来源证书预检](../../../../scripts/infra/docs/指标中心与来源.md#证书预检)。本地 T1 通过证书负例、标准 check、材料保持与恢复拒绝；**预检通过不证明实际来源载入了该证书或已有采样**。

11 月 8 日 PostgreSQL 实源及清理通过，未另列当次 SHA，详见[共享目录与来源复验](#pg-directory-history)。主机 bridge 有 PASS／清零终端记录但**工具收据缺退出码**，不补计完整退出成功或原生宿主／Hosted。个人 Redis／MinIO 修证后旧卷仍失败，用户显式重新应用后才取得实际握手及页面证据，见[联合核验](#seven-sources-live-conclusions)。

<a id="seven-sources-live-conclusions"></a>

<!-- 保留对应主题旧链接。 -->
<a id="七项本机采集来源与真实-safari-联合核验2026-11-08"></a>

## 七项来源与 Safari 联合核验（2026-11-08）

**当次个人环境通过**：主机加上双域各三个引擎共七来源严格 mTLS 200、Prometheus 唯一目标 UP／无抓取错误；数据库完整容器身份、启动时刻、作用域与页面主体匹配，新鲜度取真实样本／观测时间。

Safari 两域六卡片／六详情实际值、主机关联及选中曲线通过：PostgreSQL 提交速率、Redis 内存、MinIO 空间占用率各五分钟，Desktop VM 的 CPU／内存等当前值及一小时内存三曲线。收据为 `/tmp/addp-seven-sources-live-20261108.json`、`/tmp/addp-seven-sources-safari-20261108.json`、`/tmp/addp-seven-sources-crosscheck-20261108.json`；**未另列固定运行 SHA**，不借其他版本补齐。

**未覆盖**：不是原生 macOS／Linux 全宿主、受控页面完整交互或 Hosted 验收；未验全部指标、七天、缩放、故障恢复及长期稳定性。文件系统／网卡／TCP 缺项不表示零，Redis 无上限比率和 MinIO 原生缺项仍沿各来源语义留空。

<a id="minio-helper-label-conclusions"></a>

<!-- 保留对应主题旧链接。 -->
<a id="infra-minio-恢复被隔离辅助容器标签干扰2026-11-14"></a>

## 最新 MinIO 发现故障：辅助容器标签（2026-11-14）

**首败与原因**：正式 `addp-minio` 健康，但本地 Trace 镜像自带正式 `addp-infra/minio` 标签，直接 run/create 的 verify／build／space 辅助容器继承标签，污染唯一部署发现，来源恢复被拒绝。原唯一性检查正确，不能通过放宽来源选择、账号或数据卷约束规避。

**修复范围**：本地 Trace 唯一 Docker 新建入口显式覆盖当轮 project 和 `service=local-helper`，保留原专属运行／role 标签及资源参数。旧实现反例失败，修复后 225 项确定性测试、Node 语法及离线 TLS 夹具通过；**完整 `make test-monitor-traces-local-runner` 仍非零**，全局登记阻断后 System fixture Go 未运行，新的真实隔离 Trace 未验。原记录未列该修复的固定 SHA／Job，不能借旧成功记录补齐。

**当前恢复依据**：旧辅助容器由其原生命周期自行清理后，只读部署核验及来源状态恢复；不是尚未进入新运行的修复已经生效。受限 mTLS 核验唯一 up=1、最近一分钟四样本、观测约 11.88 秒、目标无错误，收据 `/tmp/addp-infra-minio-sampling-20261114.json` 与 `/tmp/addp-infra-minio-target-20261114.json`。没有重跑 up/recover 或个人服务。

<a id="source-evidence-todos"></a>

## 后续待办与证据缺口

1. Redis 修复后的正式 Hosted 全链路仍需独立证据，补专业页面、双域停采／恢复、对等存活和清理；原采样超时、`cleanup=failed` 与缺少逐指标取证保留。MinIO 本页也没有正式 Hosted 通过结论。
2. 最新 Trace 辅助标签修复尚缺新真实隔离运行证明及固定源码／实际 Job；确定性通过和旧容器自行清理不能补成运行验收。本会话不处理该实现或无关登记问题。
3. Desktop 原子替换可见性的上游根因／候选版本、Safari hidden／缓存和早期超时仍有未明处；后续成功样本不等于根因关闭。按对应正式排查说明另行取证，不在文档整理中加重试或回退。
4. 原生 Linux 全宿主实机恢复、真实长期留存／删除、容量、其他集群与生产 T5 均按独立范围认定；既有个人页面或局部 CI 不扩张为这些能力已验收。
