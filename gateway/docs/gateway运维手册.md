# Gateway 运维手册

[模块入口](../README.md) · [架构与授权边界](gateway架构说明.md)

## 健康与依赖

先从标准启动脚本输出或 `.dev-state/ports.env` 确认当前入口；下面的 `GATEWAY_ORIGIN` 为实际 origin，不固定开发端口。

```bash
curl -i "${GATEWAY_ORIGIN:?设置实际 Gateway origin}/health/live"
curl -i "${GATEWAY_ORIGIN:?设置实际 Gateway origin}/health/ready"
```

存活只证明进程可响应；Gateway 就绪只要求至少成功应用一次 System 完整模块路由快照，不证明所有模块、Redis、日志库或目标数据源可用。未就绪时 ready 为 503，业务入口由公共中间件拒绝；路由与实例租约需另查。统一响应见[健康端点规范](../../docs/spec/api/健康与平台控制面.md#进程存活与模块就绪端点)。

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

`gateway.api_access_logs` 仅覆盖到达日志中间件且取得 API Consumer 上下文的请求，不能用于证明所有认证失败或匿名访问都已记录；写入是异步的，日志库故障也可能造成缺口。当前 `service_name` 从路径首段提取，Query 路径会得到 `query`，定位实际服务应同时检查 `request_path`。字段与记录边界见[架构说明](gateway架构说明.md#6-数据库)。

在有权限的目标日志库只读检查当前请求路径、消费方、状态、耗时、`rate_limited` 与时间；其中 `rate_limited` 仅按响应状态是否为 429 标记，不能单独区分 Gateway 限流与下游拒绝。日志不得保存完整凭据、Hash、Authorization、Cookie 或请求体；运行日志的可信来源、权限和分页沿[统一日志规范](../../docs/spec/addp模块服务运行日志规范.md)，访问日志不替代正式 System 审计。

1. 先区分存活、快照就绪与目标模块有效租约，再核对实际 origin 和请求路径。
2. Query 凭据使用 `addp_api_` 前缀，不同时携带 Authorization；在 System 的 API 消费方管理中核对 Consumer、Credential、Tenant 和精确服务 Grant。
3. 依次定位 Gateway 前置认证/限流、Service 最终授权、源数据及执行错误；缺日志不表示请求未发生。
4. 按[配置规范](../../docs/spec/addp配置介绍.md)核对依赖；不要直接改业务数据库、伪造 Tenant Header 或放宽中间件。需要重启时由用户按标准生命周期流程执行。

## 回归验证

从仓库根目录使用 `make test-module MODULE=gateway`；当前工作区按范围使用 `make test-changed`。快速定位可用 `make test-module MODULE=gateway GO_ONLY=1`，只证明 Gateway Go T1，不替代完整模块门禁。

认证或 owner 执行实现同时变更时，再按[测试与验收规范](../../docs/spec/addp测试与验收规范.md)扩散到 System / Service 门禁。真实启动、租约失效与恢复使用已登记的[模块注册恢复场景](../../docs/spec/testing/scenarios/部署与执行恢复.md#module-registry-recovery)，不能以单元测试或手工请求代替 T4 证据；本页样例不表示本次已执行。
