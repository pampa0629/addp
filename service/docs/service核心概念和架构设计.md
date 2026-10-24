# Service 模块核心概念和架构设计

Service 负责将受管数据或确定计算来源发布为可消费服务，并登记第三方服务端点。专业定义归 Service，Engine 与身份归 System，资源与结构事实归 Meta，指标定义与实现修订由对应 owner 维护。

## 服务对象与职责

| 对象 | 保存和提供的能力 | 详细说明 |
| --- | --- | --- |
| QueryService | 表、普通/联邦 SQL 或确定指标实现修订的查询与有界导出。 | [查询发布与执行](architecture/查询发布与执行.md)、[指标来源与版本](architecture/指标来源与版本.md) |
| TileService / TileServiceLayer | 多图层二维动态 MVT 或业务 PMTiles 发布。 | [瓦片发布与缓存](architecture/瓦片发布与缓存.md) |
| RegisteredService / RegisteredServiceLayer | 第三方端点、上游认证配置和能力解析结果；公开代理有独立的访问边界。 | [注册服务](tables/registered_services表.md)、[注册图层](tables/registered_service_layers表.md) |
| GraphQueryService | 图引擎的 shape / cypher 查询定义与输出配置。 | [当前图服务模型](../backend/internal/models/graph_query_service.go)、[模块契约](../CLAUDE.md)、[验收入口与边界](testing/验收方法与排查.md#图查询服务验收入口) |

服务分类不等于所有协议、格式和权限路径均已接通。当前入口、Consumer Descriptor 与未决消费边界以各专题及模块契约为准。

## 阅读导航与正文归属

| 阅读目的 | 文档 |
| --- | --- |
| 来源、引擎绑定、依赖指纹、统一查询与命名参数 | [查询发布与执行](architecture/查询发布与执行.md) |
| 查询创建、详情与页面预览操作 | [查询服务创建与详情](architecture/管理界面与模块协作.md#查询服务创建与详情) |
| REST/Features 请求、地址来源、身份与失败分支 | [查询与要素协议](testing/查询与要素协议.md) |
| 发布血缘身份、实际同步范围、失败重试与验收边界 | [发布血缘同步边界](architecture/查询发布与执行.md#发布血缘同步边界) |
| 指标修订、管理并发号、消费版本、重绑及编译预览 | [指标来源与版本](architecture/指标来源与版本.md) |
| 动态/静态来源、协议、存储缓存与待核对的失效边界 | [瓦片发布与缓存](architecture/瓦片发布与缓存.md) |
| Console 入口、表单流程、模块分层与协作 | [管理界面与模块协作](architecture/管理界面与模块协作.md) |
| 当前表、字段、索引及写入一致性 | [数据库架构](数据库架构.md)及其表说明 |
| 当前 API 路由、DTO 与模块规则 | [Service 模块说明](../CLAUDE.md)、[当前 Router](../backend/internal/api/router.go) |
| 协议样例、入口/身份、代理完整性与能力验收边界 | [API 测试指南](API测试指南.md)及其现行测试专题 |
| 模块门禁、真实链路、客户端互通与故障排查 | [标准入口与证据](testing/验收方法与排查.md#标准入口与证据)、[验收方法与排查](testing/验收方法与排查.md) |
| 待确认的能力范围与专题归属 | [后续能力清单](service后续能力清单.md) |

架构专题维护来源、执行与模块协作；数据库架构维护跨表流程、状态回写和上游认证装配，表说明维护字段、索引、JSONB 结构及协议写入差异；测试专题维护请求样例、断言与入口核对。通用验收判定归 [API 测试指南](API测试指南.md#能力与验收边界)，门禁和证据要求归[验收方法与排查](testing/验收方法与排查.md#标准入口与证据)。其他文档保留必要摘要并链接正文；接口与数据库迁移仍以当前实现入口为据。

## 阅读与验证边界

固定协议和能力以当前路由、模型及实现为据。字段/来源不完整、修订撤回、依赖漂移或版本冲突按 owner 契约拒绝；未接通的栅格渲染、TMS 路由、缓存 TTL 判定和公开代理控制不计为已实现或已验收。

管理 API 使用当前 Tenant AuthContext 与精确 Permission；公开消费、源数据和数据保护仍各自核验，不从服务状态、目录可见或配置保存推导已获授权。实现验证沿上述门禁与证据入口，指标选择及编译预览的独有约束见[指标来源与版本](architecture/指标来源与版本.md)。
