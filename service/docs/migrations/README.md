# Service 数据库迁移入口

[返回核心架构总入口](../service核心概念和架构设计.md)

根发布入口在启动 Service 之前执行 `service-backend prepare-database`。部署账号准备自有结构与保护投影，运行账号只读核验 Common、Service 与保护投影版本。查询服务发布规则、访问范围和服务名唯一性沿用现行数据库约束。

## 当前入口

PG 在版本事务内迁移八个领域模型、analytical 发布记录、Catalog 初始摘要和变更触发器、清理结果、保护投影以及 Query 消费契约。关键字和图节点标签在 PG 保留原生 TEXT[]，MySQL 使用原生 JSON 数组。Service 仓储模型负责两种存储格式的编解码，保留元素顺序、空数组和数据库 NULL。MySQL 新装时，模块自行安装约束和 Catalog 触发器，保护投影使用 Common 的安装入口。两个引擎均在相关步骤成功后发布 Service 版本。

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
