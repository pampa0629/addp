# Spark 计算集群与内置运行时用户手册

## 入口与职责

Business Spark 是用户登记的 Standalone 计算集群，不保存 ADDP 业务表或文件。内置 Spark Runtime 负责把一次查询或工作流提交到任务选择的集群；Runtime HTTP 地址不是 Master 地址，也不是用户登记的 SQL 服务。

| 入口 | 用户选择 | 数据来源 |
| --- | --- | --- |
| Develop 查询工作台 | Business Spark 集群 | 获准读取的真实 Source Engine，首期为 PostgreSQL／MySQL |
| Develop 工作流编辑器 | 内置 Spark Workflow，再绑定一个 Business Spark 集群 | 算子 locator 指定的已授权源／目标 |
| Manager／Meta | 仅展示、扫描声明存储能力的引擎 | 不把 Spark 当作表或文件存储 |

以上是已确认的统一契约。**当前处于迁移阶段**：独立查询 Driver、执行授权和能力路由基础已落地；正式查询 HTTP 接口、执行快照绑定及页面链路仍须统一接通。不能把下述部署调整或底层准入通过当作查询工作台已经可用。

## Business 部署

在仓库根目录由部署用户执行：

```bash
bash business/scripts/start.sh -spark
```

只启动 Master 与默认 Worker，不启动 Thrift Server，不初始化固定 SQL 示例视图。标准入口核对 Master `/json/` 的原生 `ALIVE` 状态和在线 Worker；未就绪明确返回失败。端口冲突检查仍走 Business 标准生命周期。

| 服务 | 首选宿主机端口 | 用途 |
| --- | --- | --- |
| Standalone Master | 7077 | Runtime 提交计算作业 |
| Master Web UI | 18088 | 查看 Worker 与应用状态 |

不再发布11000端口，删除 `SPARK_THRIFT_PORT`。已有私有环境文件中的旧变量不生效，不为外部工具保留默认 Thrift 服务。资源限额、固定镜像和 Worker 拓扑以 [Business Compose](../../../business/docker-compose.yml) 为准，启动与停止见 [Business 文档](../../../business/README.md)。

## 登记与任务绑定

System 的 `spark` 引擎登记实际 Master 计算端点：

| 字段 | 说明 |
| --- | --- |
| `host` | 调用 Runtime 与检测服务可达的 Master 主机 |
| `port` | 必填 Master RPC 端口，表单默认7077 |
| `web_ui_url` | 同一集群的 Master Web UI 地址；本机默认 `http://localhost:18088`，不从 RPC 端口推算 |

测试连接读取 Web UI 原生 `/json/`，核对公布 Master URI、正常状态和非零在线 Worker，并检查配置的 RPC 端点可达；不运行计算应用，真实计算另行验收。Docker/NAT 下公布 URI 与宿主端点可不同，检测不会用公布地址替换已登记目标。首版仅支持未启用 Spark RPC 认证的 Standalone 开发集群。旧 `database`、`username`、`password` 和 `master_port` 不接受，旧登记须显式编辑，不自动兼容。Business 不改写 System 登记、不删除历史 Meta 项、不静默改绑任务。

工作流继续通过 `execution_config.engine_specific.spark_cluster_id` 绑定一个实际集群，同一任务固定一个集群，不同任务可选不同集群。后端在执行前验证目标，再向 Runtime 传递可信集群与租户、执行授权。源数据身份与集群身份分开，不能用 Runtime ID 或集群 ID 代替 Source Engine ID。

查询工作台直接选择集群，由 Develop 从受信控制面解析内置 Runtime，冻结绑定并调用同一服务的 `addp.query-runtime/v1`。不新增独立 `spark-query` 服务。每次查询使用独立 Driver，不能依赖前一次查询留下的临时视图、缓存或会话设置。

## 数据与空间能力边界

- 业务数据继续保存在 PostgreSQL、MySQL、Elasticsearch、HDFS、MinIO 等真实存储引擎；具体查询或工作流能访问哪些来源，以各入口已实现并通过验证的能力声明为准。
- 查询首期只做 PostgreSQL／MySQL 只读联邦，不据此声明对象存储、写入、流式或空间查询；原有 Thrift 空间 SQL 示例不再作为 ADDP 查询入口。
- Spark Workflow 的 Sedona 依赖由 [spark_dependencies.py](../spark_dependencies.py) 管理，算子、数据类型和发布保证见 [Runtime README](../README.md#存储接入与结果发布)。不能把 Workflow 的空间依赖等同于查询入口已开放空间能力。
- 文件格式识别和三维格式能力归 Common 格式层及相应 Runtime，不由 Spark 集群登记推断。

规范以 [引擎体系](../../../docs/concepts/addp引擎体系图.md#spark-集群与内置运行时边界)、[查询 Runtime 协议](../../../docs/spec/addp引擎插件接口规范.md)及[能力声明](../../../docs/spec/addp引擎能力声明规范.md)为准；本手册不把代码落地、底层集群运行和正式页面验收合并为同一证明。
