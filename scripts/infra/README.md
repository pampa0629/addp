# ADDP 基础设施管理脚本

本目录管理 ADDP Infra；Business 引擎由 `business/` 独立管理。操作规则和命令按下列专题查阅，命令在仓库根目录执行。

## 阅读导航

| 阅读内容 | 专题 |
| --- | --- |
| 容器归属、Compose 项目、网络、端口、启动顺序与数据卷 | [部署与项目隔离](docs/部署与项目隔离.md) |
| 启动/停止/状态入口、目录结构、初始化调用与脚本职责 | [生命周期脚本](docs/生命周期脚本.md) |
| database 清单、安全测试 DSN、Schema 隔离、扩展与测试边界 | [PostgreSQL与测试库](docs/PostgreSQL与测试库.md) |
| MinIO Bucket、Redis Key、搜索索引及对应初始化脚本 | [存储与缓存初始化](docs/存储与缓存初始化.md) |
| 环境模板、部署参数、CPU 架构与镜像选择 | [环境与跨平台配置](docs/环境与跨平台配置.md) |
| Kafka 单机与 HA 认证的范围、入口及恢复边界 | [Kafka认证](docs/Kafka认证.md) |
| 只读备份、隔离恢复、旧搜索卷迁移与加密网盘备份 | [备份恢复与搜索迁移](docs/备份恢复与搜索迁移.md) |
| 项目隔离问题、连接故障、旧搜索实例与端口排查 | [故障排查](docs/故障排查.md) |
| 日志设施选择、权限、采集链路、保留与验证入口 | [运行日志](docs/运行日志.md) |
| Prometheus、独立节点来源、查询身份与私有 TLS 转发 | [指标中心与来源](docs/指标中心与来源.md) |

## 操作前查阅

- 本地 PostgreSQL 测试先确认[database 清单与实际端口](docs/PostgreSQL与测试库.md#postgresql-database-清单)。
- 旧搜索卷升级按[迁移与回滚流程](docs/备份恢复与搜索迁移.md#旧-meilisearch-卷迁移与回滚)执行，普通启动不承担迁移。
- 数据卷删除、备份恢复和观测凭据的操作边界以各专题为准。

## 相关文档

- [端口分配](../../docs/spec/addp端口分配.md)
- [部署步骤](../../docs/guide/addp部署和开发步骤.md)
- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构说明
- [docker-compose.infra.yml](../../docker-compose.infra.yml) - 基础设施容器配置
- [.env.example](../../.env.example) - 环境变量配置模板
- [docs/spec/addp配置介绍.md](../../docs/spec/addp配置介绍.md) - 配置分层与管理能力规范

### 可选磁盘与卷容量

标准节点入口在 `ADDP_NODE_METRICS_ENABLED=true` 后可显式选择 `ADDP_NODE_BLOCK_CAPACITY_ENABLED=true`。默认关闭；部署者仍使用同一 `node-metrics.py up/down/status`，不手工采样或导入文件。仅仅读元数据的非特权采集器每十五秒原子发布专属 Textfile，原生 Linux 绑定只读 sysfs/proc，Desktop 使用自身 VM 视图，不访问 Mac 物理盘、原始设备或 Docker Socket。容量与卷／底层设备逐项展示，不求和；零容量、分区、RAM、loop 不进入主表。开关变更由部署者应用标准节点入口，未选、采集失败及超限均不影响原 CPU／文件系统等来源。停用扩展移除其容器和归属核验后的专属临时卷；节点 down 同样清理。不向个人环境自动应用。来源协议见[运行观测规范](../../docs/spec/addp平台运行观测规范.md#磁盘与卷容量)。
