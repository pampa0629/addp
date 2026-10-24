# Service 数据库迁移入口

本页维护启动迁移顺序、历史 SQL 的用途及验证入口；当前表及存储边界见[数据库架构](../数据库架构.md)，字段与声明差异见对应表说明。

## 当前入口

下表按[主程序](../../backend/cmd/server/main.go)的调用顺序排列；前三项位于 `InitDatabase / AutoMigrate` 内，后两项由主程序另行调用。

| 范围 | 权威实现与说明 |
| --- | --- |
| 模型结构 | [InitDatabase / AutoMigrate](../../backend/internal/repository/database.go)建立 schema，逐个迁移[表清单](../数据库架构.md#模块与表清单)中的八个模型；共享保护投影存储由后续独立入口维护。 |
| analytical 发布数据 | [版本化迁移](../../backend/internal/repository/analytical_migration.go)由 AutoMigrate 调用；版本是否应用以 `service.data_migrations` 账本及执行结果为准。 |
| Catalog 变化流 | [迁移实现](../../backend/internal/repository/database.go)负责初始摘要、索引与变化捕获触发器；不等于企业 Catalog 专业定义迁移。 |
| 保护投影存储 | 主程序调用共享 [projectionstore.Migrate](../../../common/dataprotection/projectionstore/store.go)；结构及版本核对由[共享迁移实现](../../../common/dataprotection/projectionstore/schema.go)负责，使用独立的 `protection_projection_store_migrations` 账本，不记入 `service.data_migrations`。存储初始化不证明保护投影已同步或实际请求已通过保护验收。 |
| Query 消费契约 | 主程序建立消费目录服务后调用[契约迁移服务](../../backend/internal/service/consumer_catalog_service.go)，由 [Repository](../../backend/internal/repository/query_service_consumer_contract_migration.go)按版本将不满足当前消费契约的 active REST 记录置为 `error`；不伪造输出契约。 |

各阶段没有共同事务；后续阶段失败会阻止启动，但不承诺撤销此前已完成的迁移。`service.data_migrations` 记录 analytical、Catalog 初始摘要及消费契约等版本化数据迁移，不是所有模型结构或约束的安装清单。Catalog 索引与触发器仍由当前启动代码维护，不能按账本版本号推导执行顺序或完整结构状态。

## 历史 SQL 与数据库约束

当前启动代码不扫描执行本目录或 [`backend/migrations/`](../../backend/migrations/) 下的 SQL 文件。历史 SQL 仅用于核对当时的声明，不是可按文件名顺序直接执行的升级或回滚方案，也不能替代当前入口。

应用预查、模型声明与实库约束的证据规则统一见[索引与读取](../数据库架构.md#索引与读取)。注册服务名唯一范围的未确认项见[数据库待核对事项](../数据库架构.md#待核对事项)；瓦片服务名、图层唯一性及级联的声明差异分别见[瓦片服务约束](../tables/tile_services表.md#索引与约束)和[瓦片图层约束](../tables/tile_service_layers表.md#索引与约束)。这些差异仍需按目标数据库验证，不能从启动成功或账本记录推导已消除。

## 当前规则的正文归属

- 查询来源、协议、公开设置与资源上限：[查询服务表](../tables/query_services表.md)及[查询发布与执行](../architecture/查询发布与执行.md)。
- 注册端点与图层：[注册服务表](../tables/registered_services表.md)、[注册图层表](../tables/registered_service_layers表.md)。
- 瓦片协议与 MVT/缓存配置：[瓦片服务表](../tables/tile_services表.md)、[瓦片图层表](../tables/tile_service_layers表.md)及[瓦片发布与缓存](../architecture/瓦片发布与缓存.md)。

## 验证与运行边界

标准命令统一见[标准入口与证据](../testing/验收方法与排查.md#标准入口与证据)；迁移与持久化专项的实际选择、DSN/隔离要求、断言及未覆盖项见[迁移与持久化覆盖](../testing/验收方法与排查.md#迁移与持久化覆盖)，不为文档核对执行迁移 SQL 或操作业务数据。

运行升级沿[部署与开发指南](../../../docs/guide/addp部署和开发步骤.md)及[配置规范](../../../docs/spec/addp配置介绍.md)，先核对目标部署和实际基础设施状态；需要重启时由用户按标准生命周期流程执行。启动成功、结构迁移成功、数据迁移成功及接口运行验收分别核对，不用文档清理或历史清单代替通过证据。
