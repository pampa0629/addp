# Gateway 运维手册

[模块入口](../README.md) · [架构与授权边界](gateway架构说明.md)

## 健康与依赖

先从标准启动脚本输出或 `.dev-state/ports.env` 确认当前入口；下面的 `GATEWAY_ORIGIN` 为实际 origin，不固定开发端口。

```bash
curl -i "${GATEWAY_ORIGIN:?设置实际 Gateway origin}/health/live"
curl -i "${GATEWAY_ORIGIN:?设置实际 Gateway origin}/health/ready"
```

存活只证明进程可响应；Gateway 就绪只要求至少成功应用一次 System 完整模块路由快照，不证明所有模块、Redis、日志库或目标数据源可用。未就绪时 ready 为 503，业务入口由公共中间件拒绝；路由与实例租约需另查。统一响应见[健康端点规范](../../docs/spec/api/健康与平台控制面.md#进程存活与模块就绪端点)。

## 瓦片协议入口核对

先按[Service 入口专题](../../service/docs/architecture/瓦片发布与缓存.md#元数据链接与公开入口)区分直连 Service Backend、直连 Gateway 与根 Nginx 统一入口。仓库根 Nginx 尚未转发 `/wmts`、`/tiles`、`/ogc/tiles`，这些路径落入指向 Console 的 `/`；给协议 URL 添加 `/api` 不能视为已挂载的替代入口。

实际验收统一沿[公开入口核对](../../service/docs/testing/瓦片协议.md#元数据链接与公开入口核对)，分别留存原始元数据链接、各入口响应和客户端访问结果。直连成功不证明公开入口可达；HTTP 200 可能是 Console 页面，健康或配置检查通过也不能代替 XML/JSON 元数据、实际瓦片及 HTTPS 链路验收。未运行项单独注明。

## 验证 API Consumer 数据面

使用已授权的测试凭据和真实 Query Service 程序标识；Query 结果的字段、分页和协议检查见[Service 测试指南](../../service/docs/testing/查询与要素协议.md)。凭据材料不写入共享报告或脚本。

```bash
curl -i \
  -H "X-API-Key: ${API_CONSUMER_CREDENTIAL:?设置测试消费凭据}" \
  -H 'Content-Type: application/json' \
  -d '{"page":{"limit":1},"format":"json"}' \
  "${GATEWAY_ORIGIN:?设置实际 Gateway origin}/api/query/${QUERY_SERVICE_NAME:?设置真实服务程序标识}/query"
```

| 状态 | 当前响应与排查方向 |
| --- | --- |
| 400 | `ambiguous_credentials`：Query 同时携带 Bearer 与消费凭据。 |
| 401 | `api_consumer_credential_invalid`：凭据格式错误或当前无效；`api_consumer_control_plane_denied`：在 `/api/v1/*` 携带消费凭据。 |
| 403 | 由 Service 最终授权裁决；核对 Consumer Tenant 与精确 Query Service ID Grant，以及源数据和保护要求。 |
| 429 | 当前 Gateway 限流响应为 `error`、`limit`、`window`，没有 `rate_limit_exceeded` 错误码；下游也可能返回 429，需核对来源。 |
| 503 | `api_consumer_auth_unavailable` 表示 Gateway 无法取得消费凭据验证结果；还需区分 `module_not_ready`、无有效 Backend 租约及下游错误。 |
| 502 | 目标代理连接或传输失败，结合 Gateway 日志核对目标实例；不直接判为凭据无效。 |

## 限流与凭据缓存

当前限流只覆盖已取得 API Consumer 上下文的 Query 请求；Redis 使用 `ratelimit:api-consumer:<consumer_id>`，首次计数后设 60 秒过期。Redis 初始化失败或请求期计数失败时当前实现继续放行，不能依赖它保证故障期间的请求上限。匿名、User Bearer 和其他协议不能据此视为已覆盖；完整边界见[架构说明](gateway架构说明.md#4-缓存与撤销)。

凭据验证仅有 30 秒进程内正缓存，无 Redis 凭据正缓存；Service 逐请求复核，因此最终授权不依赖 Gateway 缓存。不要通过删除限流键或扩大缓存时间处理凭据撤销。

## 访问日志与排障

按要查的事实选择来源：

| 日志 | 实际来源与用途 | 覆盖边界 |
| --- | --- | --- |
| Gateway 消费访问日志 | `gateway.api_access_logs`，检查消费方请求的路由、响应状态、耗时及限流标记 | 只覆盖取得有效 API Consumer 上下文并到达日志中间件的 Query 请求；异步写入可能失败，字段语义见[架构说明](gateway架构说明.md#6-数据库) |
| Nginx 请求记录与错误日志 | 统一代理入口的访问记录、代理警告及错误；路径和容器输出核对见[Nginx 日志说明](../../nginx/README.md#2-日志查看) | 仅反映经过该 Nginx 的请求与代理行为，不包含消费方授权结论或模块内部处理正文 |
| Gateway 与目标模块的服务正文日志 | System → 模块管理 → 服务实例 → 查看日志，定位认证验证、代理、数据库写入及模块内部异常 | 按实例可信来源和当前权限查询；授权、分页与送达边界见[运行日志规范](../../docs/spec/addp模块服务运行日志规范.md#五正文查询与历史分页) |

在有权限的目标日志库只读检查同一测试请求的路径、消费方、状态、耗时和时间，考虑异步写入延迟，并结合 Nginx 与目标模块记录定位。`service_name` 的 `query` 不是实际服务名，`rate_limited=true` 不能单独判定为 Gateway 拒绝；具体字段边界统一引用架构说明。核对与报告中不放入完整凭据、Hash、认证 Header、Cookie 或请求体，遵循[认证凭据日志边界](../../docs/spec/addp登录认证的统一要求.md#24-认证凭据日志边界)。

现有访问日志单元测试覆盖字段构建、Query 敏感参数名脱敏、请求体未被读取，以及无消费方上下文时不建记录；不证明真实数据库异步落库、Nginx 请求记录或集中运行日志送达。真实验收须分别核对请求响应和对应来源记录，不能用健康检查、单条日志或 HTTP 200 推导完整计量、审计或业务成功。

1. 先区分存活、快照就绪与目标模块有效租约，再核对实际 origin 和请求路径。
2. Query 凭据使用 `addp_api_` 前缀，不同时携带 Authorization；在 System 的 API 消费方管理中核对 Consumer、Credential、Tenant 和精确服务 Grant。
3. 依次定位 Gateway 前置认证/限流、Service 最终授权、源数据及执行错误；缺日志不表示请求未发生。
4. 按[配置规范](../../docs/spec/addp配置介绍.md)核对依赖；不要直接改业务数据库、伪造 Tenant Header 或放宽中间件。需要重启时由用户按标准生命周期流程执行。

## 回归验证

从仓库根目录使用 `make test-module MODULE=gateway`；当前工作区按范围使用 `make test-changed`。快速定位可用 `make test-module MODULE=gateway GO_ONLY=1`，只证明 Gateway Go T1，不替代完整模块门禁。

认证或 owner 执行实现同时变更时，再按[测试与验收规范](../../docs/spec/addp测试与验收规范.md)扩散到 System / Service 门禁。真实启动、租约失效与恢复使用已登记的[模块注册恢复场景](../../docs/spec/testing/scenarios/部署与执行恢复.md#module-registry-recovery)，不能以单元测试或手工请求代替 T4 证据；本页样例不表示本次已执行。
