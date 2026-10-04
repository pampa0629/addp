# NFS 存储引擎计划

> 状态：阶段性方案已收口。当前实现以 provider 化 engine plugin 体系为准。

本文只保留 NFS 当前语义和后续注意事项。正式路径规范见 [../spec/addp存储引擎路径体系规范.md](../spec/addp存储引擎路径体系规范.md)，插件接口规范见 [../spec/addp引擎插件接口规范.md](../spec/addp引擎插件接口规范.md)。

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

2026-10-04 实现进展：Simple 只读插件、Business NameNode/DataNode 与三格式样例已落地；独占 T2 已验证每页 2 条的真实目录分页、有界内容与范围读取、中文/空格/百分号文件名、共享 CSV/JSON/Parquet 解析，以及真实 Worker 每格式 20 行、金额合计 2100 的聚合。格式验证消费由 WebHDFS 插件读出的原始字节，在宿主机使用现有 CGO 工具链运行共享解析器；便携 Linux WebHDFS 测试程序仅携带 HDFS 依赖。正式 T4 仍待开发环境重启后验收。
