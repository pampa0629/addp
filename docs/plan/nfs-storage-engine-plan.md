# NFS 存储引擎计划

> 状态：阶段性方案已收口。当前实现以 provider 化 engine plugin 体系为准。

本文只保留 NFS 当前语义和后续注意事项。正式路径规范见 [../spec/addp存储引擎路径体系规范.md](../spec/addp存储引擎路径体系规范.md)，插件接口规范见 [../spec/addp引擎插件接口规范.md](../spec/addp引擎插件接口规范.md)。

HDFS 隔离自动验收使用手动 Hosted T4 `hdfs-spark-consumer-flow`。Business 夹具复用现有 Hadoop 配置、三格式初始化和固定官方镜像，在独占 Linux Runner 的宿主网络启动 NameNode、DataNode、Spark Master/Thrift 与 Worker；消费者地址统一为回环，Driver 和 Worker 共享同一网络命名空间。平台及 Spark Workflow 通过标准开发入口启动，固定 Java 11、Simple 用户 `addp_business_reader`，不启用本地 Spark 模式。System owner 夹具提供最小权限普通 User 和独立登记 User；经正式 API 登记两个 general Engine，移除登记令牌后由普通 User 完成扫描、四个文件预览及八节点正式工作流。结果必须核对三格式每种 20 行、金额 2100，并关联当次 Spark Application 与 Worker 实际完成任务的日志；Console 真实登录、Meta 重扫、Manager 预览和 Develop 专业详情必须输出同一身份的证据。成功、失败、中断均清理当次应用、业务容器和 Infra，并删除凭据；首次 Hosted 真实通过前只允许人工触发，不登记夜间调度。

首轮 Hosted T4 [37209233339](https://github.com/pampa0629/addp/actions/runs/37209233339) 在 HDFS general Engine 连接成功后，因 Spark general Engine 的 Thrift 连接探测失败而终止，尚未进入消费者工作流；应用和业务夹具清理完成，Infra 为零残留。登记器失败日志应保留脱敏后的协议错误，业务夹具清理前归档自身容器状态与 Spark Driver/Worker 日志，以便确定连接根因；不得将该轮计为 T4 通过。

第二轮诊断 [37211144056](https://github.com/pampa0629/addp/actions/runs/37211144056) 确认 Thrift 拒绝空用户名：客户端为 `Bad SASL negotiation status: 3`，服务端为 `No user name provided`，四个业务容器均在运行且无 OOM。Spark 连接唯一用户名字段以插件 `ConnectionSpec` 声明的 `username` 为准，连接探测、SQL 查询及表读取均消费该字段，不接收 `user` 别名。Hosted 夹具显式设置 `username=spark`，不再依赖 Runner 操作系统的用户名称；此为 Spark SQL 会话身份，与 HDFS Simple 用户 `addp_business_reader` 分属两种连接配置。该修复不改变认证模式或 HDFS 路径语义。

## 当前语义

- NFS 是文件系统语义存储，不是对象存储。
- 连接配置中的 `export_path` 是挂载配置，不进入用户可见路径、`full_name` 或 ResourceLocator。
- 用户看到的根目录为显性 root，数据库中 root 节点 `name` 使用引擎实例名称、`full_name=""`；`.` 不进入目录路径。
- 数据路径从挂载根 `/` 开始，`full_name` 使用相对路径。

## 当前接口

- 目录发现：`EngineCatalogProvider.ListChildren`
- 路径解析：`EngineCatalogProvider.ResolvePath`
- 文件 catalog leaf facts：`EngineCatalogFactsProvider.DescribeEngineCatalogFacts`
- 内容读取：`ContentReadableProvider.OpenContent`

旧的专用文件系统接口方案不再作为上层接口边界。

## 扫描与预览

- Meta 从 root 递归扫描目录和文件。
- 目录写入 `meta_node`，普通文件写入 `meta_item`。
- Parquet 文件或目录可识别为 `table` 语义。
- Manager 使用 locator `type=file` 预览；表格内容语义由 `attributes.item.data_type=table` 表达，不改变文件的目录术语。

## 后续事项

- 验证根目录文件、深层目录文件、湖表目录三类路径。
- 若引入 HDFS/local filesystem，应复用同一 Catalog/Content provider 模型。

## HDFS 已确认的首版范围

2026-10-04 确认 HDFS 首版包括统一引擎登记、文件目录扫描与预览，以及真实 Spark Worker 的分布式读取；认证采用 Simple 开发实验模式，固定应用 Hadoop 用户，拒绝连接主体与应用身份不一致的读取。路径目标契约见 [存储引擎路径体系规范](../spec/addp存储引擎路径体系规范.md#hdfs-路径目标契约)。实现与真实消费验收完成前不得登记为已支持。

实施前须同步识别并接入以下门禁：

| 层级 | 验证范围 | 标准入口与登记要求 |
| --- | --- | --- |
| T0/T1 | 插件唯一登记、能力/接口一致、目录与 locator、管理根边界、错误与有界读取、Develop 路径/执行参数派生、Spark 读取适配 | 现有 `make test-engine-plugin-registration`、`make test-go` 与模块自动发现；新增协议用例纳入对应 owner，不能另建旁路 |
| T2 | 固定官方镜像的独占 HDFS；真实目录/状态/范围读取；CSV、JSON、Parquet 样例幂等；真实 Spark Worker 读取与聚合 | 新增 owner gate 须同步登记根 Make、owned Compose、输入路径、影响选择和 CI；成功、失败与中断均清理并核验零残留，不接管个人开发服务 |
| T4 | System 正式登记、Meta 扫描、Manager 预览、Develop 通过资源 locator 发起 Spark 作业，验证同一源实例与结果 | 在隔离环境完成正式消费链路；若新增 suite，须同时登记标准分发、身份、报告和 CI，首次真实通过前保持人工触发 |

准备阶段补齐现有 Spark Workflow T1 的 `make test-spark-workflow` 与 Platform CI 登记，并由 `make test-module MODULE=engines` 自动发现；这只验证已有运行时契约，不表示 HDFS 访问已经完成。

实现门禁入口为 `make test-common-hdfs-unit`（插件边界与 Develop locator 派生）、`make test-spark-workflow`（Spark 身份/地址/只读适配）和 `make test-common-hdfs`（固定官方镜像、独占网络、真实 WebHDFS、共享格式解析器和 Standalone Worker 三格式聚合）。HDFS T2 注册到根 `test-integration`、Common 自动发现与 `release-and-t2-gates.yml`，其输入声明覆盖 Business HDFS、Spark Workflow 和 Develop 派生代码；这些路径的变化会选中 Common HDFS 门禁。T2 不替代 System 登记、Meta 扫描、Manager 预览和 Develop 正式发起作业的 T4。

WebHDFS 分页只使用 Hadoop 3.5.0 实际协议的全小写 `startafter`；[官方 StartAfterParam 源码](https://github.com/apache/hadoop/blob/rel/release-3.5.0/hadoop-hdfs-project/hadoop-hdfs-client/src/main/java/org/apache/hadoop/hdfs/web/resources/StartAfterParam.java)及固定镜像中的类常量均如此。官方 WebHDFS 文档的 `startAfter` 示例不能作为运行字段事实，不能发送双字段兜底。独占 T2 将 NameNode `dfs.ls.limit` 设置为 2，必须跨页列出全部样例；Business 默认仍为 1000。

Business 只拥有 HDFS 服务、样例和生命周期，不调用 System API。Spark 的 Hadoop 配置必须覆盖 Driver 和 Executor，连接必须同时到达 NameNode 与 DataNode；WebHDFS 可用不能代替原生 Spark 访问验收。引擎登记、权限校验与执行期连接解析继续归原有 owner。首版不包含 HDFS 写回、Kerberos 或 HA；真实门禁通过前，不声明 Simple 消费能力。

2026-10-04 实现进展：Simple 只读插件、Business NameNode/DataNode 与三格式样例已落地；独占 T2 已验证每页 2 条的真实目录分页、有界内容与范围读取、中文/空格/百分号文件名、共享 CSV/JSON/Parquet 解析，以及真实 Worker 每格式 20 行、金额合计 2100 的聚合。格式验证消费由 WebHDFS 插件读出的原始字节，在宿主机使用现有 CGO 工具链运行共享解析器；便携 Linux WebHDFS 测试程序仅携带 HDFS 依赖。

正式消费验收进展：已通过 Console 登记 `Business HDFS`（引擎 26），连接测试正常；Meta 自动扫描 229 与目录扫描 230 成功；Manager 的 CSV、JSON、Parquet 预览各显示 20 行。共享 locator 重复解码问题修正并重启后，`订单 100%.csv` 也已通过正式预览，显示 20 行。

Develop 已保存任务 5 `hdfs_simple_read_acceptance`，通过引擎 26 的三个资源 locator 读取，独立选择 `Business Spark`，按 region 聚合并关联校验结果，任务定义不保存物理地址或连接参数。Spark 的公开列表类型映射修正后，执行参数契约已正常生成。首次执行 244 因 CSV 被默认按 Parquet 读取而失败；修正 Develop 文件/对象格式派生后，Spark 显式消费共享格式识别器根据原始文件名确定的格式，缺失时拒绝读取。HDFS 三格式、对象格式、公开参数边界与 Spark 34 项确定性测试已通过。

2026-10-04 重启 Develop 与 Spark Workflow 后，正式执行 251（`758b526b-2ef8-4b30-99f3-eccfcb843f67`）成功，8 个节点全部完成，耗时 23.61 秒。Develop 专业执行详情及运行时结果均确认三种格式各有 20 行、金额合计 2100；east 分组每格式 10 行、合计 1100，west 分组每格式 10 行、合计 1000。Business Spark Standalone 应用 `app-20261004133131-0002` 分配 1 个核心，Worker 容器中该应用的 Executor 日志确认真实计算任务完成。本地开发环境的正式消费链路人工复验通过；尚未登记或运行隔离环境的 HDFS Online suite，不计为自动化 T4 门禁通过。

格式修复提交 `e93d72b08` 的 [Release and T2 gates](https://github.com/pampa0629/addp/actions/runs/37200801282) 已通过，包括 HDFS WebHDFS 与分布式 Spark、Redis、Elasticsearch 契约；[Platform CI](https://github.com/pampa0629/addp/actions/runs/37200801286) 的 Spark、Develop、Go workspace 与平台一致性检查通过，但产品镜像构建失败：Spark Workflow 镜像安装 Debian Bullseye security 软件包时返回 404。后续已迁移到 Python 3.11 Bookworm 与官方 Temurin Java 11，补齐共享 Python 包、根目录构建上下文、Compose 与构建登记；本地 ARM64 标准镜像构建已通过依赖一致性、API 导入和真实 Spark 计算检查。镜像修复提交 `7bf7489df` 的 [Platform CI](https://github.com/pampa0629/addp/actions/runs/37207120567) 已通过 AMD64 产品镜像构建、平台一致性与 Spark Workflow 测试，原镜像构建阻塞已消除；后续核对该轮 Platform CI 已全部通过。

HDFS Hosted suite 的实现及登记已补齐。本地 `make test-platform`、`make test-hdfs-online-runner`（17 项场景/夹具测试及 System IAM 夹具）、`make test-common-hdfs-unit`、`make test-spark-workflow`（34 项）与 `make test-business-config` 均通过。工作区 `make test-changed` 因多个 Owner 的 PostgreSQL DSN 和 MySQL/OceanBase 环境缺失停在预检，未执行后续门禁；不能计为通过。Hosted 前两轮在 Spark 空用户名连接探测处失败，修复后仍须完整复跑，不声明 T4 已通过。

Spark Thrift 字段修复的最小验证为 `make test-go` 和 `make test-hdfs-online-runner`：前者先复现三个入口发送旧字段，再验证修复后的真实 SASL 载荷及全部 Go 模块；后者确认夹具显式设置规范用户名及 18 项场景/生命周期测试、System IAM 夹具。两项已通过。现有 Go 模块自动发现覆盖新增协议测试，原 HDFS T4 入口用于验证完整修复，不新增认证路线或 CI 旁路。

第三轮 Hosted [37212514934](https://github.com/pampa0629/addp/actions/runs/37212514934) 的 HDFS 与 Spark 正式登记均成功，Meta 扫描与四文件 API 预览通过；正式工作流因 Sedona 1.5.1 的 `cdm-core:5.4.2` 传递依赖无法从 Maven Central 解析而失败，未开始分布式执行或浏览器验收。该上游缺陷由 [Sedona 官方说明](https://sedona.apache.org/1.5.3/setup/maven-coordinates/) 确认，统一升级同系列 Python/JAR 至 1.5.3、GeoTools wrapper 至 1.5.3-28.2；镜像验证使用实际生产连接器加载依赖并执行空间函数。最小门禁为 `make test-spark-workflow`、标准 `make build-images IMAGE_BUILD_ARGS="--services spark-workflow-engine --verify --jobs 1"` 和原 Hosted T4；现有自动发现覆盖相关路径，不增加额外 Maven 仓库或替代会话路线。

Sedona 修复的本地 `make test-spark-workflow`（34 项）和 ARM64 标准镜像构建/验证均通过；镜像在空 Ivy 缓存下从 Maven Central 解析 6 个依赖制品，经生产连接器建立 Java 11 会话，验证 3 行计数、聚合结果 3 及 `ST_AsText(ST_Point(1, 2))`。该验证属于镜像启动门禁，不能替代 Standalone Worker 的正式 HDFS T4。前置 Thrift 修复提交 `fafc70fc1` 的 [Platform CI](https://github.com/pampa0629/addp/actions/runs/37212498578) 和 [Release/T2](https://github.com/pampa0629/addp/actions/runs/37212498612) 已全部通过。

Sedona 修复后的 `make test-platform` 亦通过，包含 Online 分发/隔离生命周期、引擎启动与 CI 登记一致性和 Swagger 覆盖；完整 Hosted T4 仍须复跑。

第四轮 [37214813784](https://github.com/pampa0629/addp/actions/runs/37214813784) 已通过正式 Spark 三格式聚合和对应 Standalone Worker 已完成任务校验；浏览器因误用 ES 的直接叶模型“重新扫描引擎”按钮而失败。HDFS 层级目录模型的 Console 验收应在现有目录列表对 `samples` 行执行“重新扫描”，再核对四个 DataItem ID 稳定、四个文件预览和同一正式执行详情；完整引擎扫描与根文件发现由前置普通 User API 阶段验证。只修正测试操作，不新增界面扫描路线或放宽结果断言。

第五轮 [37216410872](https://github.com/pampa0629/addp/actions/runs/37216410872) 已通过目录重扫、四个文件页面预览和正式 Spark/Worker 校验，最后的测试辅助 API 因复用登录时的旧 Access Token 返回 401。页面切换通过共享 Browser AuthSession 恢复并轮换会话，System 按规范撤销旧 Access Token；测试应核对 Develop 页面实际发出的执行详情响应及当前 AuthContext，不跨页面复用令牌快照或新增刷新路线。该轮清理完成、Infra 零残留，仍不计为 T4 通过。对应提交 `e1aadf0f2` 的 [Platform CI](https://github.com/pampa0629/addp/actions/runs/37216400908) 和 [Release/T2](https://github.com/pampa0629/addp/actions/runs/37216400977) 均通过；本地 Console 141 项单元测试通过，浏览器回归启动前被其他进程的 4170 端口占用阻断，CI 的 Console 完整门禁已补齐验证。
