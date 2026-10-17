# 文件系统引擎后续事项

NFS/HDFS 的当前路径、Catalog/Content Provider、Simple 身份与范围读取规则，统一维护于[存储引擎路径规范](../spec/addp存储引擎路径体系规范.md#七对象存储与文件系统模型边界)、[HDFS 契约](../spec/addp存储引擎路径体系规范.md#hdfs-路径目标契约)及[引擎插件规范](../spec/addp引擎插件接口规范.md)。本文不再保留逐轮诊断、镜像修复、重启、业务 ID 和 CI 流水。

## 一、已有验收与边界

HDFS Simple 只读已取得 WebHDFS/真实 Spark Worker T2，以及正式 System/Meta/Manager/Develop 的隔离 Hosted T4。产品容器默认入口及后续结果发布的最终证据由[Spark Runtime 验证说明](../../engines/spark-workflow/README.md#运行测试)唯一维护；早期 Thrift、Sedona 和浏览器夹具失败不继续列为当前阻塞，也不把已有通过外推到所有模式。

## 二、未实施或需单独验收的范围

- HDFS 写回、Kerberos、HA、YARN 和 MapReduce 尚不在首版范围；有真实需求后先确认认证、路径、权限及 Provider/Runtime 契约，再实施。
- Spark Runtime 尚未声明 NFS Engine 服务器/导出路径接入；本地文件辅助函数不能当作该能力。新增时须遵循固定管理根和同一授权 locator，不引入任意宿主文件读取。
- Runtime 跨重启执行快照恢复不由当前进程内 ExecutionRegistry 保证；需要时单独确认持久化及恢复语义。
- MinIO/S3/NFS 的根文件、深层文件和 whole scope 组合回归见[文件与目录表格后续事项](湖仓一体化第二阶段详细工作规划.md)，不维护第二份格式能力矩阵。

## 三、验证与维护

现有标准入口为 `make test-common-hdfs-unit`、`make test-common-hdfs`、`make test-spark-workflow` 和 `make test-hdfs-online-runner`。真实部署使用已登记 `hdfs-spark-consumer-flow` Hosted suite，仍仅人工触发，不登记夜间 schedule；按 workflow 当前 profile 隔离启动、真实用户消费和退出零残留，不接管个人环境。新增认证/写入/部署依赖须同次接入门禁和 CI，未验收组合不开放能力。完成内容回写规范，从本文件移除，不追加过程流水。
