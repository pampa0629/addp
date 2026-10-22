# registered_services 表说明

[返回数据库架构](../数据库架构.md)

`service.registered_services` 对应 `RegisteredService`。它登记第三方端点，与[注册服务图层](registered_service_layers表.md)是一对多关系，不是 ADDP 模块注册表或 System APIConsumer。

## 字段

字段类型按当前 Go 模型展示；不把 DTO 投影、关联切片或旧设计字段列为数据库列。

| 字段 | 模型类型 | 含义与约束 |
| --- | --- | --- |
| `id` | `uint` | 主键；不是源数据或目录身份。 |
| `tenant_id` | `uint` | 所属 Tenant，由当前 AuthContext 限定管理范围。 |
| `service_name` | `string` | 程序标识及公开路径中的服务名；不等于显示标题。 |
| `title` | `string` | 显示标题。 |
| `description` | `string` | 描述。 |
| `keywords` | `StringArray` | PostgreSQL TEXT[] 关键词；没有独立关键词关联表。 |
| `service_type` | `string` | wms、wfs、wmts、ogc_api、xyz、rest。 |
| `endpoint_url` | `string` | 上游端点；不是 ADDP 管理 API 地址。 |
| `metadata` | `JSONB` | 上游能力文档及解析结果，JSONB；按协议保存。 |
| `auth_type` | `string` | none、basic、bearer、api_key；默认 none。 |
| `auth_config` | `JSONB` | 上游认证参数，JSONB；当前写入与读取路径未接入字段加解密。 |
| `health_check_url` | `string` | 可选的专用健康检查地址。 |
| `last_checked_at` | `*time.Time` | 最近一次健康检查的时间，可为空。 |
| `status` | `string` | active / inactive / error 的持久管理状态，模型默认 active；当前公开代理不按此字段校验消费资格。 |
| `error_message` | `string` | 服务错误信息。 |
| `created_by` | `uint` | 创建人引用；不据此自动授予权限。 |
| `created_at` | `time.Time` | 创建时间。 |
| `updated_at` | `time.Time` | 更新时间。 |

## 索引与约束

模型声明的索引：`idx_registered_services_metadata_gin`、`idx_registered_services_status`、`idx_registered_services_tenant`、`idx_registered_services_type`、`unique_registered_service_name`。具体数据库约束与索引创建结果以当前迁移和集成验证为准。

`Layers` 声明 `foreignKey:ServiceID;constraint:OnDelete:CASCADE`；图层通过父服务归属 Tenant。模型没有 `DeletedAt`，Repository 物理删除服务，不提供软删除恢复流程。删除注册配置不删除第三方服务中的原始数据。

名称的应用查重按 `tenant_id + service_name`，模型唯一索引只标在 `service_name`；历史 SQL 的复合唯一约束与当前模型声明存在差异，实际约束范围见[数据库待核对事项](../数据库架构.md#待核对事项)。

## 类型、元数据与上游认证

- 当前服务类型为 WMS、WFS、WMTS、OGC API、XYZ 和 REST；没有 `data_api` 类型。接入类型不等于对协议所有版本或能力已完成验收。
- 创建后异步刷新、手动与定时刷新使用不同的状态处理；统一见[注册与元数据刷新](../数据库架构.md#注册与元数据刷新)。保存成功不保证图层齐备，异步失败不回滚已创建服务，也不自动写 error；需要重试时使用管理刷新入口。
- `metadata` 保存能力文档及解析结果；具体图层处理见[注册服务图层](registered_service_layers表.md#解析与刷新)。REST / XYZ 不自动解析任意 API 文档，也不承诺通用版本/端点数组结构。
- 当前普通 DTO 返回 auth_type 与 has_auth_config，不返回 auth_config 正文；has_auth_config 只表示配置对象非空，不是完整性或有效性校验。endpoint_url 仍会返回，JSONB 存储也没有因此加密。

Basic、Bearer、API Key 的唯一参数与装配说明见[上游认证配置](../数据库架构.md#上游认证配置)，包括 Header 覆盖、Query 追加、配置缺失时的行为，以及更新配置的替换/保留语义。存储加密、TLS、重定向与日志保护分别验证，不能将凭据成功发给上游当作保护已闭合；验收范围见[上游认证矩阵](../testing/注册服务与代理.md#上游认证矩阵)。

## 健康、管理与消费

健康探测结果与本表的持久状态分别表达上游请求结果和管理记录，均不构成公开代理消费授权。探测条件、10 秒超时、定时范围、状态与时间的分步回写，以及手动检查对 inactive 的影响，统一见[健康检查与定时刷新](../数据库架构.md#健康检查与定时刷新)；不能从健康接口 HTTP 200 推导上游健康或状态已持久化。

管理操作使用当前 Tenant AuthContext 及精确 `service.external_registration.*` Permission；`created_by` 记录来源，不自动授予权限。当前管理列表只读取 `search/page/limit`；Handler 未读取 `service_type/status` 过滤参数，不能因模型有相应字段或 Repository 有方法就声明该 HTTP 能力已开放。搜索与列表共用入口，不保留第二条 registry search 路线。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/api/v1/service/registered` | 创建注册配置。 |
| GET | `/api/v1/service/registered` | 管理列表，当前读取 search、page、limit。 |
| GET | `/api/v1/service/registered/:id` | 管理详情，包含解析出的图层。 |
| PUT | `/api/v1/service/registered/:id` | 更新管理配置。 |
| DELETE | `/api/v1/service/registered/:id` | 删除注册配置。 |
| POST | `/api/v1/service/registered/:id/refresh` | 显式刷新上游元数据。 |
| POST | `/api/v1/service/registered/:id/health` | 手动健康检查。 |
| ANY | `/api/service/registered/proxy/:id/*path` | 独立的公开代理边界；不继承管理 API 的身份与 Tenant 保证。 |

公开代理按 ID 读取注册配置、添加上游认证并流式转发，超时 60 秒。当前路由未按管理 Tenant 条件读取，完整调用授权、SSRF/重定向约束与 System 审计接入仍需核对；“已登记”不等于已建立安全白名单。具体访问与失败边界见[数据库架构](../数据库架构.md#认证与访问边界)。

## 实现依据

- [当前模型](../../backend/internal/models/registered_service.go)
- [Service 模块说明](../../CLAUDE.md)
