# ADDP Online 场景：Spark与HDFS消费验收

[返回同步与引擎消费](同步与引擎消费.md) · [返回测试与验收总入口](../../addp测试与验收规范.md)

环境、身份预检、日志安全与退出沿[T4 规范](../T4-Online验收规范.md)。下列例外和断言仅适用于各自 suite／profile；确定性、原生、页面与 Hosted 结果分别认定，不外推当前 HEAD。

## Spark 查询取消与超时：原生 T2

`make test-common-spark-federation` 在原独占双集群夹具中追加 `query-cleanup`，调用生产查询 HTTP 处理器和 `PreparedDriver`，身份／执行授权传输仍为 T2 夹具，不替代 System IAM、真实用户或页面 T4。分别验证运行中 DELETE、原请求绝对时限到期和准备后未执行的到期释放；长查询只使用已准入的纯 SQL 与原两行只读源，通过真实 Worker 的任务日志及 Executor 进程证明已经开始计算，不注入 sleep／UDF 或放宽生产查询校验。

停止前冻结 query ID、Master application ID、本地 Python／JVM 进程组及 Worker Executor 的 PID／启动时刻。停止后在销毁夹具之前，核本地活动进程组及私有目录消失、远端活动 Executor 消失、Worker 核／内存分配回到基线、Master 不再列该活跃应用；另一个集群的原应用必须保持活跃并可正常完成，每次停止后在原集群新查询须读回原精确值。远端 PID 来自隔离 Worker 内的只读 `/proc` 观察器及独占卷回执，观察时间必须新鲜，不能只凭 UI 状态认定物理退出。30 秒只是该原生用例的停止后观测上限，不是产品 SLA，也不包含客户端请求无法送达的网络分区承诺。Worker 留存日志及应用工作目录单独记录，不冒充活跃资源；最后 Compose 零残留是独立夹具清理断言。

实现与通过证据分别认定。该用例不包含工作流停止协议、写入中断、提交回滚或页面取消；工作流协议仍待用户确认后实施。

## hdfs-spark-consumer-flow

**部署与入口。** 手动 GitHub Hosted Linux x86_64；`scripts/test/online-hosted-hdfs-gate.sh` 复用共享 Hosted 生命周期。Business 管当次 Hadoop、两套 Spark Master／Worker、PostgreSQL、MinIO，以及 MySQL／Doris 扩展夹具；System helper 管普通消费 User 与独立登记／准备身份。HDFS storage Engine 与 Spark general Engine 分别经 System API 登记、连接测试。Spark Workflow 由 `scripts/utils/hosted-online.sh` 调用根 `make build-images` 构建并使用镜像默认入口，核对镜像 ID、入口、提交和运行容器；不得覆盖 command、启动原生 Workflow 服务或直接调用 Runtime 代替 Develop 执行；须核对 Runtime 与 Develop 的同一 execution 状态。Linux 夹具使用宿主网络与回环地址，真实 Worker 执行分布式任务，不能用 `local[*]`。

**授权准备（2026-11-03／04 已确认）。** 只消费既有精确授权契约：独立管理员经 System `authorization-facts` 取得 Meta 持久身份及完整边界；边界缺失、替换或不完整即失败。消费账号先拒读，正式签发后再沿其过滤目录核名称、类型和指纹；不能把未获权账号的目录当授权发现接口或改由管理员代读。

| 准备范围 | 独有约束 |
| --- | --- |
| 文件元数据 | 仅 HDFS `samples/orders.csv`、`samples/orders.json`、`samples/orders.parquet`、`订单 100%.csv`，及同轮两次 MinIO 首次发布的独立 Parquet whole DataItem，共六项精确 `metadata_read`；不扩大到目录／桶，不把 whole 根冒充普通对象或据此宣称内容授权贯通。 |
| PG／MySQL／Doris | 由同一独立准备 User 办理精确 `read`。其原十一项授权办理权限仅在本场景增加 `meta.scan_task.execute/read`，共享 helper 必须精确核十三项，其他消费者不接受任意附加权限。PG 原初始化扫描仅执行一次；MySQL／Doris 首次写后等自动扫描，再由准备身份签发两张表的读取 Grant。 |
| 消费与撤权 | 普通 User 保持原十三项加 `develop.task.create`、`develop.task.update` 的十五项权限。准备回执绑定 Run／Tenant／User／Engine／Runtime；正式消费前清除登记和办理凭据。退出由独立进程重读私有凭据，按持久回执撤销文件及表授权并验证隐藏／拒读；未知签发也须清理，证据缺失或不匹配即失败，不重扫、重发或代读。 |

HDFS 原扫描和两次 MinIO 发布各在独立准备进程执行一次，后续消费扣除已耗用的等待预算。第一次发布后的 Manager 基线须由不带两种办理凭据的普通账号子进程在第二次发布前取得，再比对第二次写入未改变第一次成果；不增发工作流。

**业务矩阵。** API 与 Console 均核同一普通身份，成功操作关联精确批次的 Master Application 及其 Worker 已完成任务日志；Driver 结束后不得仍有活跃 Application，不能取最新同名 Application 充数。

| 范围 | 必须保留的业务与物理断言 |
| --- | --- |
| HDFS 读取 | Meta 深扫、Manager CSV／JSON／Parquet 及中文百分号根文件预览；用资源 locator 创建八节点工作流，每格式 20 行、合计 2100、region 聚合一致。 |
| 双集群页面 | 单 Runtime 服务 A（7077／18080／18081）和 B（7078／18082／18084），不占 Kafka Connect 18083，不接管个人集群；两端 `SPARK_LOCAL_IP=127.0.0.1`，`/proc/net/tcp{,6}` 核六个监听（[Spark 3.5 WebUI 实现](https://github.com/apache/spark/blob/v3.5.0/core/src/main/scala/org/apache/spark/ui/WebUI.scala#L130-L145)）。真实查询工作台与工作流编辑器覆盖 A→B→A、并发、A 失败而 B 成功及 A 恢复；查询读已授权 PG 成果，单 SQL 工作流对分区 `range` 聚合得到四行。每批比较两端 Application 集合，只有目标端新增。 |
| 双集群失败与回查 | 原 16 次执行、12 批、两次预期失败及预算不变。临时视图缺失／联邦缺列分别核 `table_not_found`／`column_not_found`、共享面板与仅含版本／阶段／原因的回执；受保护查询兼有缺列时，由 Develop 真实保护门禁和 Spark Driver 放行顺序回归验证优先拒绝。保留并发与 Master 同时活跃观察；同批终态后才顺序回查详情，每次由当前页面真实认证请求重新核同一 User／Tenant／十五权限，不能复用旧 Bearer、准备身份或重试放宽 401，最后释放 API 上下文。 |
| MinIO Parquet | 两个独立成果目录，核自动扫描、Meta `table/parquet/whole`、精确血缘、Manager 预览、下游 Spark 读取、首次成果不变；仍保留原四种发布／复用和页面断言。T2 另验 Magic Committer、footer 行数、空成果、同名 bucket 的 endpoint／凭据隔离、失败目录清理，按 pending 记录只取消已知 upload ID 并保留兄弟上传；记录缺失／损坏须保留证据并记清理未核验，空 multipart 列表不足为据。 |
| MySQL／Doris | 当次独占实例、固定镜像、Doris 唯一初始化；Doris、Worker、Runtime 的 BE 公布名称解析到当次回环地址。普通 User 从 Develop 首次创建带 Run ID 的表，完成授权准备后覆盖／复用。五行含正负超安全 bigint、Decimal(38,18)、null、空串、emoji 尾随两空格及百字符文本；独立 oracle 核原生值、表身份／DDL 不变、零暂存表，页面核 Meta 字段、Manager 精确 JSON 和表格、Develop 首写／覆盖／复用，不以 Runtime 直调或手工补扫代替。 |

**页面契约。** 只选 samples、basic、显式 force，提交 `catalog_paths: ['samples'] / force: true`，核原执行／四项身份／全部预览；普通祖先投影可无 scan_status，不据“扫描／重扫”文案猜历史或增权。真实 label 勾选并核 checkbox，不强制点／改 DOM。中英／两初态、唯一载荷和 canonical `/develop/workflow?action=create`登录路由由 Console T1/T3 验证；不放宽 query／登录等待，受控控件通过不代后端授权或 T4。

**清理与验收。** 停止应用、保存 Runtime 日志，删除当次 MySQL／Doris 目标并确认不存在，再销毁当次容器、匿名卷、Runtime 镜像、Infra 和仓外凭据。Runtime／临时镜像仓库有独立所有权标签；Business 残留核验覆盖自有 Hadoop／Spark／PostgreSQL／MinIO／MySQL／Doris，不操作个人资源。缺页面、物理证据或清理失败均失败。

标准入口：`make test-hdfs-online-runner`、`make test-online-runner`、平台 T0；MinIO 原生覆盖复用 `make test-common-hdfs`，共享元数据 helper 按实际消费者回归。Hosted 部署前先跑同一 HDFS Owner 入口，真实验收用 `make test-online ONLINE_SUITE=hdfs-spark-consumer-flow`；定位用 `make test-hdfs-online-runner HDFS_ONLINE_TEST_ARGS="-k <用例名>"`，默认仍完整回归。不新增平行 suite、开发服务或调度；实现／T1／T2 不替代各扩展固定 SHA 的完整 Hosted T4，真实结果按实际运行记录认定。
