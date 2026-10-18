# Service 数据库迁移入口

本页说明当前迁移入口与文档归属。当前表及存储边界见[数据库架构](../数据库架构.md)，字段、协议和配置见对应表说明；不再维护针对旧 internal/external 服务表的手工迁移或回滚步骤。

## 当前入口

| 范围 | 权威实现与说明 |
| --- | --- |
| 启动初始化 | [主程序](../../backend/cmd/server/main.go)调用 [InitDatabase / AutoMigrate](../../backend/internal/repository/database.go)，建立 schema、迁移当前模型并执行 analytical 发布及 Catalog 变化流迁移。 |
| analytical 发布数据 | [版本化迁移](../../backend/internal/repository/analytical_migration.go)由 AutoMigrate 调用；版本是否应用以 `service.data_migrations` 账本及执行结果为准。 |
| Catalog 变化流 | [迁移实现](../../backend/internal/repository/database.go)负责初始摘要、索引与变化捕获触发器；不等于企业 Catalog 专业定义迁移。 |
| Query 消费契约 | 主程序建立消费目录服务后调用[契约迁移服务](../../backend/internal/service/consumer_catalog_service.go)，由 [Repository](../../backend/internal/repository/query_service_consumer_contract_migration.go)按版本将不满足当前消费契约的 active REST 记录置为 `error`；不伪造输出契约。 |

当前启动代码不扫描执行本目录或 `backend/migrations/` 下的 SQL 文件。后者的历史 SQL 不作为一组可按文件名顺序直接执行的部署步骤；具体约束以当前模型、迁移代码与集成验证为准，不能仅凭历史建表 SQL 宣称已安装。

## 当前规则的正文归属

- 查询来源、协议、公开设置与资源上限：[查询服务表](../tables/query_services表.md)及[查询发布与执行](../architecture/查询发布与执行.md)。
- 注册端点与图层：[注册服务表](../tables/registered_services表.md)、[注册图层表](../tables/registered_service_layers表.md)。
- 瓦片协议与 MVT/缓存配置：[瓦片服务表](../tables/tile_services表.md)、[瓦片图层表](../tables/tile_service_layers表.md)及[瓦片发布与缓存](../architecture/瓦片发布与缓存.md)。

## 验证与运行边界

按[测试与验收规范](../../../docs/spec/addp测试与验收规范.md)使用 `make test-changed`，或在范围明确时使用 `make test-module MODULE=service`；迁移与持久化专项沿 `make test-service-postgres`。本地 PostgreSQL 仅使用允许的隔离测试库，准备与清理由标准入口负责，不为文档验证执行迁移 SQL、创建测试 database 或操作业务数据。

运行升级沿[部署与开发指南](../../../docs/guide/addp部署和开发步骤.md)及[配置规范](../../../docs/spec/addp配置介绍.md)，先核对目标部署和实际基础设施状态；需要重启时由用户按标准生命周期流程执行。启动成功、结构迁移成功、数据迁移成功及接口运行验收分别核对，不用文档清理或历史清单代替通过证据。
