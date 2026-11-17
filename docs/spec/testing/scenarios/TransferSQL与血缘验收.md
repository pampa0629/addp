# ADDP Online 场景：Transfer SQL 与血缘验收

[返回同步与引擎消费](同步与引擎消费.md) · [T4 环境与清理](../T4-Online验收规范.md)

下列业务、页面、原生与清理分别取证，不把分项或旧版本通过合并为当前完整 suite 通过。

## Transfer 仅新增 MySQL 同步

Transfer 仅新增 MySQL T4 使用永久 PostgreSQL 与 MySQL Engine Instance，并由专属复合 Fixture 管理两端物理表。真实 User 必须从 Console 页面选择源表和目标库、进入字段映射、通过正式“分析源数据并推荐精度”接口把无声明精度的 PostgreSQL `numeric` 收敛为不会截断当前值的 MySQL `DECIMAL(6,2)`，再以主键 `id` 作为唯一 watermark 创建任务。页面自动执行的首轮必须读写 6 行；Fixture 同时修改 `id=1` 并新增 `id=7` 后，用户从任务详情再次执行必须只读写 1 行。最终 MySQL 目标必须共 7 行、`id=1` 保持首轮值、`id=7` 为新增值且无重复键，以证明单字段水位只覆盖严格递增新增、不承诺旧记录更新。浏览器还必须断言任务配置的 `tie_breaker=[]`、目标 `upsert` 键为 `id`，捕获并删除临时任务且确认 404；Fixture 在全部退出路径删除两端固定表并停止两个容器。该 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得增加定时触发。

## transfer-relational-sql-etl

仅手动 Hosted `ubuntu-24.04` x86_64、共享 `scripts/utils/hosted-online.sh`、独占 Infra／非默认 Tenant。System helper 建身份，独立 Provisioner 正式登记 PG／MongoDB／Doris／ClickHouse／MinIO，随后清 Token；消费 User 无 `system.engine.read/execute`，仅读同 Tenant Meta 投影。Business 仅管理自有容器／源／目标和 tmpfs MinIO，不调用 System；DB 账号只读源、只在指定 schema 创建／管理自有目标。

Console 真实选源→固定 SQL（无 MQL／语言下拉／高级编辑）→基础构造器投影与两参数过滤→PG snapshot。保存仅标准 `source.query` statement／类型化 parameters，decimal 为字符串；映射只有投影字段，Worker 准确读写 2 行，独立物理核行集合／金额／列序。任务正式删／404，自有业务容器／Infra／卷／凭据清理，身份／Engine 随平台库销毁。

标准 `make test-transfer-online-runner`、`make test-online-runner`、`make test-platform`；T4 `make test-online ONLINE_SUITE=transfer-relational-sql-etl`。先等 Transfer Security 投影、Manager 就绪／Worker 注册和同提交 preflight，失败不建任务／改保护状态。ClickHouse 就绪使用原 240 秒共享预算，探针 NETWORK_ERROR(210)仅在预算内等待；各准备阶段有界、初始化不重试部分写入，诊断只私有保存并清理，不在本页重复命令时限表。具体阶段／报告 schema 以原 Runner 登记为准。

### Manager 私有导入

| 链路 | 关键断言与独立核验 |
| --- | --- |
| Shapefile 私有导入 | Console 弹窗上传固定两点 ZIP，核目标 namespace／CPG、当前 User 持久 sync 任务及 Transfer execution，等唯一 Meta 子扫和树刷新；Transfer 唯一读写／血缘 owner，不复制后台 HTTP 旁路。Business `manager-import`独立核完整 PostGIS 值／坐标／SRID，不从平台响应造期望。 |
| 私有来源拒绝 | 绑定源码／Run／Tenant／User／真实父子 UUID；非上传者启动同任务零读写拒绝、普通 User 直接 Inspect 拒绝，专业详情仅安全诊断。接受上传即捕获任务／execution，后续失败也清；独立领域成功不代全 suite 成功。 |
| 初始使用权 | 原同一 execution 授权终态后重开 namespace，上传者真实预览两点、旁观隐藏／拒读。准备者只查精确 `task_production`历史，唯一 until_revoked read 绑定接收方／边界；正式撤销后历史在、目录隐藏／预览拒绝。页面失败也沿原 execution 回收已签 Grant。 |
| 对象上传 | 同 User 正式 `/uploads`传 CSV＋五成员 Shapefile 到自有 MinIO，真实 Meta UUID 须 source=manager、原 User／Tenant／Engine、deep 及直接上传来源；自动扫后目录仍空。准备者仅对 `transfer-upload/upload-check.csv`、`transfer-upload/points.shp`签 15 分钟 metadata_read，消费权限不变，结构两项同身份；撤两项后目录空／详情 404，部分失败也清。 |
| 物理原件 | 独立读六对象全字节／SHA／大小／ETag／时间。CSV 大小是相等正整数、时间按同一时刻、ETag 原样；Shapefile 一个 multi，大小为五对象之和、ref 集合精确。源随 tmpfs 销毁，设施零残留另核。 |

保留 `manager-private-import.json`（`addp.manager-private-import/v3`／`addp.manager-private-import-browser/v1`）、`manager-object-upload.json`（`addp.manager-object-upload/v2`）的身份、授权和物理绑定。导入后刷新／Monitor 导航竞态由独立嵌入式 T3 证明，Hosted 不补认；现有 Manager 前端门禁与[私有导入证据](../验收证据索引.md#manager-预览与私有导入历史证据)按各自范围认定。

### 写入失败与证据边界

Doris／ClickHouse 各两合成失败，原普通 User／Tenant、独占固定镜像／随机回环目标。Doris 唯一 FE／BE 入口＋严格写入／分区限制，ClickHouse CHECK。四份 Python 配置须经真实 Go planner 纯解析，目标 `parent_locator + name`，拒 `target.locator`。

1001 行按显式顺序同批：原生约束拒末行前已提交 1000 行；非法 JSON 末行则本批无新增。加原哨兵后物理分别 1001／1 行，`records_written=0`不能推目标空。各任务 failed、不同 UUID、关自动扫描，目标 Meta 前后未扫；同 User 真实详情核安全原因码、可确定列序／声明类型、target_change_possible、终态／执行号和截图，不暴露原 error_msg／Runtime 正文／checkpoint。提交标记另由原生 T2 验证。

各层断言归[写入与安全诊断门禁](../Owner补充门禁.md#关系型写入与安全诊断门禁)；[历史证据](../验收证据索引.md#addp-验收证据索引)区分 v3 原生通过、Hosted 前置失败和实际进入四 Provider 场景。源／任务／容器清理不抵业务首败，不把前置失败记 v3 T4 通过。

### ClickHouse 二进制传输

`Nullable(String)`以独立 `addp:type=bytes`注释声明 bytes；四样本为 `4100FF42`、`4142`、空字节串、SQL NULL，另有文本。准备者正式扫 PG 及 ClickHouse 精确源、仅签两表 read。普通 User 沿目录／Gateway 建手动 bounded snapshot，批大小 2，映射新 PG 原生表；同 UUID 核发起人、首次 attempt、开始／完成和两批各 2 行。

Business 独立按 ID 核全行 hex／字节长／空值／文本，PG 为可空 BYTEA。任务删／404，撤 ClickHouse 源 Grant 后同 User 读检查 403；自有设施另清。`clickhouse-binary-roundtrip.json`绑定提交／Run／User／Tenant／扫描／Grant／execution、批次、物理值及清理，独立段先于私有导入／血缘／写入失败取证，后者目标保持未扫。

### 字段级血缘验收

来源／结构冻结、租户过滤与 unavailable 定义归[数据血缘契约](../../addp数据血缘能力规范.md#61-字段级首期契约)，本节只列实际链路与独立 oracle。消费无管理员、具 `meta.lineage.read`／`manager.data_item.read`／`manager.content.read`，无 `catalog.entry.read`；编排扩展仅按原夹具补 Develop／Orchestrator 任务 CRUD 执行、Develop 数据读写、Monitor 读取和 System 执行授权。

**来源准入。** 准备者精确扫描 `transfer_fixture.activities`，System 事实核身份，`ExactRecordGrants`签 read 前 Meta 同路径 404／manager-preview403，签后 200 且指纹／Grant 一致并有观察时间。两次 Mongo→PG 各 3 行；退出撤权后 Meta404／内容 403，再启动原传输须零读写且不提交扫描。部分签发／后续失败同样清，`mongodb-source-access.json`单独记证（完整 suite v17 起）。SQL ETL／Continuous 的同级项、祖先、扫描时间／深度／状态／统计，只由当轮 addp_online 只读事务按正式身份核；原始值仅内存，拒绝由原普通 User API 取证。

| 链路 | 必须独立核对的事实 |
| --- | --- |
| 原生表／SQL | 字段改名、decimal 精度转换、同目标 replace 映射更新、两跳搬运；真实成功 execution 自动采集、精确字段／结构、direct／derived、关闭旧入边、不串字段。可完整证明的单来源 SQL 记录映射，opaque／unresolved／不完整证据仍 unavailable，不伪造同名来源。 |
| Mongo→ODS | 原生只读集合经透明 MQL `$project`六显式映射；冻结保 `title.date`、`title.level`、`leader.personid`、`leader.userInfo.nickName`，别名只供后续映射。原任务两次，目标 ID 稳定，仅最新执行活跃边；不手动补扫／collect。独立核六列全行集，页面跨引擎来源及日期／昵称切换。 |
| 编排 ODS→DIM→DWD | 预建／扫空 DIM／DWD，同编排两轮执行 Transfer／DIM relation／DWD JOIN；每轮三子 execution 与父、owner／任务绑定，日期三跳到 DWD、昵称 direct／强度 derived。Develop 只安全资源／顶层 outputs／行数，operations／schema_snapshot 不从安全 DTO 取，字段 hash 取 Meta；URI 不带选择器 item_id。独立 DIM3 行／DWD2 行、全类型／值；父编排不造来源边、第二轮不串首轮。 |
| 真实结构演进 | DWD `person_nickname`→`person_display_name`、`activity_date` date→timestamp without time zone；正式扫描后重写前四当前图 unavailable、各只根、旧字段 404。旧 hash＋第二轮父完成时间的历史图保原字段／direct 日期／第二轮证据，DWD 边第三轮前 stale、后 closed；原编排第三次只当次子证据，新日期 derived，源／ODS／DIM hash 和 ID 不变。独立核新列／timestamp／全行，v5 保两代结构。 |
| 多来源 SQL | 在原生源和已 replace 映射表上两个 `query.inputs`，真实 CTE／JOIN／UNION ALL 产 4 行；各来源独立冻结，多值来源精确 derived，仅 JOIN／筛选作用不造字段值来源边，三层结构／完整目标／列序一致。 |
| 500 列宽表 | 2 行两跳，仅扫初始源；自动目标／血缘 1500 节点、1000 活跃边，三表 hash／两次 execution。两目标各 500 非空 bigint 列，独立全行多重集合；整图请求不传 limit。四表内 fit 字号可读，宽概览不要求同时读 500 行，字段重定位至少 11px。 |

**公开／私有证据分工。** User 字段图只可见端点／hash／状态／真实 execution 摘要，不含转换字段或任意 evidence；direct／derived 在自有平台库只读事务，精确绑 User／Tenant／两端资源字段／hash／execution 及同一不可变 observation，历史另绑 as_of。不能从原生私有查询替 User 读取或公开配置／样本。

**页面。** 登录后核同 User／Tenant 才监听业务错误，登录前 refresh401 不算，之后 401／403／503 均失败。真实 Manager iframe 仅一次 `subject_kind=data_item&granularity=field`整表查询，切字段本地聚焦、零 Catalog 摘要；自动采集不能由 collect／手扫替代，namespace 锁造成的“扫描成功但目标未入库”不算完成。820×667 三跳 fit 后字段完整；卡片左／下各 30px 后曲线端点≤2px，等真实绘制帧；搜索、折叠／展开、自动布局及 Console iframe 原生全屏往返有效。

**物理与清理。** MongoDB7／PG 独占 tmpfs、自有只读源账号；先等正式 PID1 mongod 再认证 ping（[官方入口](https://github.com/docker-library/mongo/blob/master/7.0/docker-entrypoint.sh)的临时实例成功不代正式就绪），同一有界等待、只初始化一次。浏览器／多来源全部结束后生命周期统一核 ODS／DIM／DWD／多源／宽表全行类型，浏览器不提前调用 oracle。所有 Run 命名 Transfer／Develop／编排定义正式删／404；源集合／表与六目标、自有容器／Infra／凭据清理。首次图差异只留有界脱敏诊断：精确项／字段、首次／末次节点边与转换枚举／UUID／过滤标志／预期集，数组各≤100，不复制原响应／表达式／样本；收敛或超时更新终态。

#### 扫描调用链与读取隔离

原上传者与同 Tenant 同业务权限的另一 User，不额外赋权。对象上传后上传者读同一 Meta UUID，Monitor 树仅该根扫描；私有导入后读 Transfer 父及唯一 Meta 子扫。另一 User 可读持久任务父及过滤后的仅父树，但 Meta 专业详情、Monitor 详情／树／事件四入口对一次性扫描都 404、仅错误字段，不能以缺功能 403 代隔离。树不截断／重复／丢失／串 UUID，详情／树投影一致。

`scan-trace-visibility.json`（`addp.scan-trace-visibility/v1`）绑定源码／Run／两 User／权限／父子／可见节点／拒绝状态和字段名，仅安全执行 DTO 最小投影。两链全过才留独立收据，后续失败不抹该分项；v14 纳隔离、v15 纳二进制，完整报告仍按当前登记，不由局部收据认整套通过。

#### 性能采样口径

几何验证页与轻量性能页分离，同登录、1500 节点／1000 边、每页只加载一次，不装详细 Canvas 观察器。首／中／末字段由真实按钮事件到≥11px 居中后的下一稳定帧，保 CPU 剖面；初绘从字段级切换事件开始，用 Resource Timing `responseEnd`拆网络／后处理；自动布局从真实按钮到重新可用后的稳定帧。只认实际可见绘制，离屏文字测量不算完成，fit 概览不套聚焦 11px。

归档 `transfer-wide-native-focus.json`、三份 `.cpuprofile`，初绘／布局另为 `transfer-wide-native-render.json`、`transfer-wide-initial.cpuprofile`、`transfer-wide-layout.cpuprofile`。排除搜索／Playwright 定位／导航等待但包含轻探针／CPU 开销；端到端／轻量／几何／本地夹具口径不互算倍率，CPU 嵌套归因不相加，采样总长非点击耗时，也不构成固定延迟 SLA。历史数值与固定结果归[血缘场景证据](../验收证据索引.md#历史证据索引)，不替当前源码验收。
