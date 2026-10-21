# Spark Workflow 分布式空间计算引擎

Spark Workflow 是基于 Apache Spark 和 Apache Sedona 的工作流运行时，连接 System 中登记的 Spark 集群执行 DataFrame 与空间算子。算子协议见[工作流计算引擎接口规范](../../docs/spec/addp工作流计算引擎接口规范.md)，本文说明 Spark 的运行、资源绑定、存储接入与验证边界。

## 配置与启动

部署配置使用仓库根目录 `.env`，不读取引擎目录中的 `.env`。关键配置为 `SYSTEM_URL`、`SPARK_WORKFLOW_PORT`（默认 8098）、独立 `SPARK_WORKFLOW_SERVICE_CLIENT_SECRET`，以及应用固定的 HDFS Simple 身份 `HADOOP_USER_NAME`。默认不设置 `SPARK_MODE`，计算使用用户选择的 Spark general Engine。

在仓库根目录启动；需要重启时，由用户在自己的终端执行对应命令：

```bash
bash scripts/dev/start.sh -spark-workflow
# 修改运行时代码后重启原生开发服务
./scripts/dev/restart.sh -spark-workflow
```

Spark Workflow 本地开发统一使用宿主机 Python 3.11/3.12 虚拟环境和 OpenJDK 11，与 Business Spark Worker 保持 JVM 主版本一致。标准入口自动发现 Java 11、创建缺失的虚拟环境。`start.sh -spark-workflow` 与 `restart.sh -spark-workflow` 共用唯一原生启动入口，按完整 requirements 和 editable `common-python` 同步依赖并执行 `pip check`。全套与局部重启均先通过 stop.sh 停止所选服务，再由 start.sh 完成 Java、Python、依赖和共享地址验证；准备失败的服务不启动，入口返回非零。HTTP 仅绑定 `127.0.0.1`，就绪必须同时验证原生 PID、监听归属和 HTTP 健康检查；`stop.sh` 按原生 PID 停止服务。macOS 使用 `host.docker.internal` 公布 Driver 与回环数据端点，需要在本机 `/etc/hosts` 配置 `127.0.0.1 host.docker.internal`；Docker Worker 保留 Docker 内置解析。该方式不依赖 Docker Desktop host networking。生产 Compose 和 Hosted 产品验收使用独立容器入口，仍验证同一应用的镜像默认启动命令。

原生开发与产品默认入口 `python api_server.py` 使用一个 Gunicorn HTTP worker、四个请求线程及公共 `ExecutionRegistry`。健康检查成功后后台持续退避自注册，System 暂不可用不会阻塞监听。依赖版本只在进程加载时读取；健康请求直接读取算子注册表的数量，不重建元数据或重复查询安装包。请求日志记录方法、路径、状态和耗时，不记录 query、请求体或认证头；真实 HTTP 回归同时验证执行任务和等待注册期间仍可查询健康状态与执行快照。HTTP worker 与远端 Spark Worker 是不同职责；实际计算由注册集群的 Executor 执行。执行快照在 Runtime 重启后丢失，正式业务历史由平台保存。

默认 API 端口为 8098；实际端口以标准启动脚本输出和 `.dev-state/ports.env` 为准。服务提供 `GET /health` 和 `GET /api/operators`，完整接口见 [HTTP 协议](../../docs/spec/workflow/HTTP协议.md)。产品镜像构建入口见[运行测试](#运行测试)。

### Driver 与 Executor 连接

Spark JDBC 的 schema 解析发生在 driver，分区读取和写入发生在 executor，因此两端必须使用同一个可达地址。JDBC、ES 和 S3A 统一将回环主机派生为 `SPARK_WORKFLOW_SHARED_HOST`；System 中保存的连接配置不变，远程主机地址不会被改写。S3A 保留协议和显式端口，并将同一派生端点用于操作配置、bucket 配置、成果核验与失败清理。缺少或非法的共享主机配置在存储访问前拒绝。PostgreSQL JDBC URL 同时继承 System `connection_info.sslmode`。

原生开发启动将本机回环地址与 `host.docker.internal` 加入 Java 和 Python 的代理绕过名单，保留已有条目和外部下载代理。Java 名单取自所选 Java 11 的有效 `http.nonProxyHosts`，覆盖 Driver 的 S3A 客户端；不依赖 Python `NO_PROXY` 间接影响 Java。该规则仅注入 Spark Runtime 子进程，需重启生效，不修改全局代理或远程引擎策略。`make test-dev-lifecycle` 验证合并、重复准备、其他 JVM 参数保留和启动失败边界。

连接失败时先核对所选集群的 Standalone Master、Driver 与 Executor 的双向可达性，以及存储端点的共同可达性。SparkSession 通过 Master 提交作业；Thrift 端口用于 SQL 查询，不参与该连接。依赖准备和分发使用标准生命周期及 `spark_dependencies.py`，不另行拼装 Session 依赖或在全局 Python 环境安装包。

## 资源绑定与执行

1. 在 System 注册 Spark 通用引擎资源，在 Develop 的 `execution_config.engine_specific.spark_cluster_id` 中记录所选资源。
2. Develop 校验资源为已启用的 `engine_type=spark`，执行时映射为请求顶层 `engine_id`。它与 Workflow Runtime 实例 ID、数据源 locator 中的存储引擎 ID 分属不同职责。
3. 后端按当前用户授权，从输入 `locator` 或输出 `target_parent_locator + target_name` 派生连接、表名或路径；用户与 AI 侧任务不保存明文连接。Runtime 不解析 `addp://` locator。
4. 后端注入 `runtime.tenant_id` 与 `runtime.execution_authorization`，授权 effects 必须覆盖整个 DAG。`SparkConnector.get_or_create_session(engine_id, tenant_id)` 使用可信租户上下文查询 System，并按 `(tenant_id, engine_id)` 缓存会话。

公开与内部参数边界见 [Develop 资源与执行规范](../../docs/spec/workflow/Develop资源与执行.md#算子契约分层)，授权摘要及请求封装见 [HTTP 执行协议](../../docs/spec/workflow/HTTP协议.md#执行工作流)。`engine_id`、租户和授权上下文不得混入算子的公开参数或工作流示例 `params`。

工作流的 DAG、引用和 `$input` 使用公共 `WorkflowRunner`。算子输出按元数据声明的端口适配，`all_results` 的单端口节点直接返回结果，多端口节点返回端口对象，DataFrame 对外只返回最多五行的摘要。

当前 `POST /api/workflow` 同步等待执行结束，成功返回 HTTP 200；工作流校验失败返回 400，执行失败返回 500。执行期间及结束后可通过 `GET /api/executions/{execution_id}` 查询进程内快照。当前内置算子只声明 `workflow` 模式；只有元数据明确声明 `direct` 的算子才允许通过单算子接口调用，不能把 `buffer` 作为 direct 成功示例。

## 算子发现与扩展

`GET /api/operators` 返回 `operator_metadata.py` 从 `operators/` 聚合的标准元数据，包含参数、端口、执行模式和 effects；算子清单以该出口为准。

| 类别 | 算子 |
| --- | --- |
| 数据 I/O | `load`、`save`、`preview`、`cache`、`persist` |
| 空间计算 | `buffer`、`centroid`、`intersection`、`union`、`spatial_join`、`distance`、`transform` |
| 数据转换 | `select`、`filter`、`add_column`、`rename_column`、`drop_column` |
| 聚合与连接 | `group_by`、`join` |
| SQL | `sql`、`create_temp_view` |

新增算子时，在对应 `operators/*_operators.py` 实现函数、定义 `OperatorMetadata` 并通过 `register_operator` 注册，再由 `operators/__init__.py` 汇总。参数命名、`execution_modes`、effects 和输出端口遵循[算子元数据与实现规范](../../docs/spec/workflow/算子元数据与实现.md)；`operator_metadata.py` 只提供统一出口，不重复手写单个算子定义。领域结果通过 `workflow_engine.py` 的适配进入公共 `WorkflowRunner`。

## 存储接入与结果发布

### 存储接入声明

`storage_adapters.STORAGE_ACCESS` 是 Spark Runtime 自注册 `compute.workflow.storage_access` 与直接接入校验的同一事实源。当前读取 PostgreSQL、MySQL、Doris、Elasticsearch、HDFS、MinIO；写入 PostgreSQL、MySQL、Doris、MinIO。NFS 服务器与导出路径尚未接入，不声明支持；本地文件辅助函数不代表已注册 NFS Engine 接入。MinIO 写入继续仅普通 Parquet + create。最小确定性验证为 `make test-spark-workflow`；前后端选择与执行校验由 `make test-module MODULE=develop` 验证。

### MinIO Parquet 成果

MinIO Parquet 成果按《工作流计算引擎接口规范》的独立目录规则发布：每次执行创建普通 Parquet whole scope，通过实际 ResourceLocator 复用；不提供目录 overwrite/append 或数据集级事务保证。Spark 使用固定 Hadoop 3.3.4 S3A 与 Spark 3.5.0 云提交组件，连接只取自已授权的存储 Engine。成功核验 Magic 提交标记和全部分片 footer 行数；失败按 Magic pending 记录精确取消本次已记录上传，再清理独占目录；无记录且无有效成功标记、记录损坏或取消失败时保留证据并报告清理未核验，不用目录前缀 multipart 空列表证明清理成功。GeoParquet 与其他对象输出格式不进入此发布路线。

完整成果身份、授权和扫描规则见 [Spark 对象存储 Parquet 成果规范](../../docs/spec/workflow/Develop资源与执行.md#spark-对象存储-parquet-成果)。

### Elasticsearch 只读加载

Elasticsearch 的 `load` 使用索引 locator 和独立 Spark 集群；Develop 按当前用户授权派生连接与单段索引名，任务不保存连接或 `index`。首版仅普通具体索引的 HTTP Basic 只读批量访问，HTTPS 显式拒绝。Driver 和 Executor 均直接访问源端点，不继承系统 HTTP/SOCKS 代理；必须能访问同一端点，回环地址使用 `SPARK_WORKFLOW_SHARED_HOST`。公开 `array_fields` 显式声明 Mapping 无法区分的数组字段，nested 无需重复声明。不支持别名、data stream、隐藏索引、DSL、流式、写回，也不承诺各次 action 共享同一个 PIT。Spark 内部保留 bigint，JSON 摘要超出 JavaScript 安全整数范围时转成十进制字符串。固定官方 `elasticsearch-spark-30_2.12:9.5.4` 完整 JAR 校验 SHA256，构建时缓存，通过 `PYSPARK_SUBMIT_ARGS --jars` 在 Spark 提交阶段与 Sedona/JDBC Maven 依赖合并分发，避免该制品 POM 引入另一版 Spark。Python Session 不设置 `spark.jars`，以免覆盖提交阶段已解析的完整依赖列表。生产 Runtime、镜像构建和分布式 T2 共用同一个依赖配置入口；T2 必须同时核对实际 JAR 分发列表和 Worker 上的 Sedona/ES 计算。

最小验证：`make test-spark-workflow`、`make test-common-elasticsearch-unit`、`make test-common-elasticsearch`；正式消费验收使用既有 Hosted T4 `elasticsearch-consumer-flow`，同时核对 Console、Runtime 状态和真实 Worker。

### PostgreSQL 发布

保存到 PostgreSQL 时，普通表和空间表统一通过 Worker JDBC 写入同 schema 的唯一暂存表。Runtime 按 DataFrame schema 识别 Geometry 列并以 EWKT 暂存，在发布事务中恢复 PostGIS geometry。已有目标的覆盖保存只 DELETE + INSERT，保留结构、约束、索引、权限和注释；追加只 INSERT。字段、类型或约束不匹配时，确认回滚后清理暂存表；回滚未确认或提交响应丢失时保留现有证据，不自动重试。已有目标的暂存表在提交确认后另行清理，新目标仍由事务内改名发布。资源关闭或清理失败不覆盖原始错误，也不把已确认提交改报失败；返回实际提交行数，暂存表不作为工作流产物暴露。表目标需要 write + ddl 授权；详细类型与事务边界见工作流计算引擎接口规范。 提交异常的确定性回归使用 `make test-spark-workflow`；真实事务回归复用 `make test-common-hdfs` 已有的独占 PostGIS、真实 Worker 和全退出路径清理。

### MySQL 发布

MySQL 保存限定普通 InnoDB 表，先由 Worker JDBC 写独占暂存表，再在 SERIALIZABLE 事务内发布。覆盖只替换数据，保留表定义；生成列、自增列明确拒绝；原有约束和触发器保持数据库语义，目标外副作用不属于目标表事务保证。时间列保留微秒，TIMESTAMP 统一使用 UTC JDBC 会话。新表通过暂存表原子改名发布。发布响应丢失时保留证据并报告状态不确定，不自动重试；MySQL DDL 不进入数据发布事务。

### Doris 读取与发布

Doris 表读取和暂存写入统一使用官方 `spark-doris-connector-spark-3.5:25.2.0` 完整 JAR，固定 SHA256 并与 ES JAR 一起在 submit 阶段分发，不通过连接器 POM 引入另一版 Spark。不再通过 MySQL JDBC 读取 Doris 表，避免协议将 VARCHAR 报告为 CHAR 后触发 Spark 补空格；保留原始字符串及合法尾随空格，不执行 trim。System Engine 的 `http_port` 是独立 FE HTTP 部署端口；已有连接须显式配置实际映射，不从 JDBC 端口猜测。官方连接器通过两阶段提交、严格模式和零错误行容忍写独占暂存表；JSON 导入固定设置 `doris.sink.properties.num_as_string=true`，保留数字原文后由目标列解析，避免 Decimal 经浮点数解析丢失精度；缓存本次输入并核对全部预期行可见后，Driver 才通过 JDBC 控制连接执行原生 INSERT OVERWRITE SELECT 或 INSERT INTO SELECT，等待原发布标签的 FINISHED 状态。首期仅支持字段类型一致的普通 Duplicate Key OLAP 表；覆盖保留模型、分区、分桶和属性。未确认可见的发布保留证据，不重放，不承诺跨表事务、跨 action 快照或端到端恰好一次。Driver/Executor 必须能访问 FE 和承载读取、导入的 BE。

确定性验证使用 `make test-spark-workflow`；真实 Worker 与独占 MySQL 8.0 / Doris 2.1.0 的完整发布验证在原生 Linux x86_64 使用 `make test-common-spark-table`，与现有 Doris 门禁同属 Hosted T2；macOS 可显式运行 `SPARK_TABLE_TEST_ENGINE=mysql make test-common-spark-table` 验证 MySQL 子集（不计为完整 T2 通过），覆盖覆盖/追加、空成果、结构拒绝、计算失败、精确 bigint/Decimal、MySQL 约束回滚及提交响应丢失，Doris 另覆盖官方连接器完整读写回路，核对中文、emoji、空串、null、合法尾随空格、超 85 字符文本及 bigint/Decimal 精度；退出核对本轮容器、卷和网络零残留。T2 不替代 Develop/Console 的正式 T4。

## 实现导航与验证

| 文件 | 职责 |
| --- | --- |
| [api_server.py](api_server.py) / [runtime_server.py](runtime_server.py) | HTTP 接口、执行快照与共用启动入口 |
| [workflow_engine.py](workflow_engine.py) | 公共 WorkflowRunner 的 Spark 算子与端口适配 |
| [spark_connector.py](spark_connector.py) | 可信租户上下文与 SparkSession 管理 |
| [storage_adapters.py](storage_adapters.py) / [elasticsearch_adapter.py](elasticsearch_adapter.py) | 存储接入声明、加载和发布 |
| [spark_dependencies.py](spark_dependencies.py) | 固定连接器依赖及 SHA256 校验 |
| [operators/](operators/) / [operator_metadata.py](operator_metadata.py) | 算子实现与公开元数据出口 |
| [requirements.txt](requirements.txt) / [原生生命周期脚本](../../scripts/dev/spark-workflow.sh) | 依赖与开发运行环境 |

### 运行测试

确定性契约入口为 `make test-spark-workflow`，覆盖公共 WorkflowRunner、跨请求执行快照、PostgreSQL 结果发布及真实 Gunicorn 的先监听、后注册行为；Hosted 编排入口为 `make test-hdfs-online-runner`。HDFS Hosted 场景按 `region` 比较 Spark 聚合、Manager 预览和再次读取的完整结果行，不把无排序 Spark 结果的行顺序作为跨执行契约；同一执行的 Runtime 快照仍须保持原始结果一致。`make test-common-hdfs` 使用独占 HDFS、Spark Worker 和 PostGIS，验证三种文件格式的分布式读取、普通与空间结果的事务发布、回滚和再次读取，并检查暂存表及测试容器清理。产品镜像通过标准 `make build-images IMAGE_BUILD_ARGS="--services spark-workflow-engine --verify --jobs 1"` 构建验证。执行快照仅在 Runtime 进程内保存，不提供跨重启恢复。

容器默认入口、HDFS 读取和 PostgreSQL 结果发布的历史验收结果统一见[测试与验收规范的证据索引](../../docs/spec/testing/验收证据索引.md#addp-验收证据索引)。独立 T1、分布式 T2 与 Hosted T4 按各自源码及实际覆盖范围认定，不合并成全工作区通过。

```bash
# 在仓库根目录运行标准入口
make test-spark-workflow
make test-hdfs-online-runner
make test-common-hdfs
```

## 许可证

与 ADDP 平台保持一致

## 相关文档

- [Spark Workflow 与 Spark 通用引擎关系说明](docs/spark-design.md)：两类引擎的职责、调用路径与执行期资源绑定。
- [Spark 通用引擎与 Sedona 用户手册](docs/spark-user-guide.md)：部署注册、SQL 查询示例与故障排查。
- [ADDP 开发原则](../../docs/spec/addp开发原则.md)
- [ADDP 工作流计算引擎接口规范](../../docs/spec/addp工作流计算引擎接口规范.md)
- [Apache Spark 文档](https://spark.apache.org/docs/latest/)
- [Apache Sedona 文档](https://sedona.apache.org/)
