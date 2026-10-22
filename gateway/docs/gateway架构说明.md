# Gateway 架构说明

## 1. 定位

Gateway 是统一入口和路由边界，不是业务资源授权 owner。它负责：

1. 通过 System 模块注册表动态发现 Backend 实例并透明代理；
2. 暴露正式的数据面协议入口；
3. 对 API Consumer Credential 做前置认证、限流和访问记录；
4. 显式隔离数据面机器凭据与 `/api/v1/*` 控制面 Bearer API。

## 2. 路由平面

| 平面 | 路由 | 认证 | 最终授权 owner |
| --- | --- | --- | --- |
| 控制面 | `/api/v1/:module/*` | 各 owner 声明的 Bearer Credential | 目标模块 |
| Query 数据面 | `/api/query/:serviceName/query` | 公开、User Bearer 或 `X-API-Key: addp_api_*` | Service |
| 其他公开协议 | `/ogc/*`、`/wmts/*`、`/tiles/*` | 由 Service 协议实现决定 | Service |

表中协议路径为路由组概览，实际挂载以 [Router](../internal/router/router.go) 为准，不代表任意 `/ogc/*` 都可访问。Gateway 已挂载 Features 与瓦片协议路由；仓库根 Nginx 仅将 `/api/` 转发 Gateway，尚未转发 `/ogc/features`、`/wmts`、`/tiles`、`/ogc/tiles`。这些路径在根 Nginx 会落入 Console 的 `/`，HTTP 200 可能是前端页面；加 `/api` 不是已挂载的协议入口。

Features 的具体路由、元数据链接地址来源及公开入口核对统一见[Service 协议测试专题](../../service/docs/testing/查询与要素协议.md#元数据链接与公开入口核对)，瓦片沿[瓦片入口专题](../../service/docs/architecture/瓦片发布与缓存.md#元数据链接与公开入口)。Service/Gateway 直连成功、集合发现或改写链接后取数成功，不证明公开 HTTPS 入口与原始返回链接可用；实际部署仍须逐项验证代理转发、响应内容、身份权限及客户端取数，未接通、失败和未运行项分别记录。

API Consumer Credential 仅进入 Query 数据面。任何 `/api/v1/*` 请求只要携带 `X-API-Key` 就返回 `api_consumer_control_plane_denied`，不得把机器消费凭据解释为 Principal 或 AuthContext。

## 3. API Consumer 认证与授权

```text
External Consumer
  -> Gateway: X-API-Key
  -> System Runtime: credential hash validation
  -> Gateway: per-consumer rate limit and access log
  -> Service: repeat credential validation
  -> Service: tenant equality + exact (query, service_id) grant
  -> Query executor
```

System 的验证投影包含 Consumer ID、Tenant ID、速率限制、状态和精确 Service Grants。Gateway 不根据名称或 URL 推导授权，也不把 Tenant Header 作为事实来源。

Service 的二次验证和精确 Grant 检查是最终授权点，因此绕过 Gateway 直连 Service 也不能越权。API Consumer 即使被授予公开服务，携带凭据时仍必须通过该 Grant。

## 4. 缓存与撤销

- Gateway 仅缓存有效投影 30 秒；无效结果不缓存。
- 不使用 Redis 保存凭据验证正缓存。
- Redis 只保存按 `ratelimit:api-consumer:{consumer_id}` 计算的分钟限流状态。
- Service 当前逐请求向 System 验证，确保 owner 授权不依赖 Gateway 缓存。
- 消费身份只记录 `api_consumer_id` 和不可用于认证的凭据短前缀；认证材料与请求体的日志边界见[认证规范](../../docs/spec/addp登录认证的统一要求.md#24-认证凭据日志边界)。

当前 Query 限流实现按 Consumer ID 原子递增计数，首次请求设置 60 秒过期；它不是按 IP、Tenant、服务或图层划分的全局限流，也不是令牌桶。Redis 初始化不可用或计数失败时继续放行；超限返回 429 及 `error/limit/window`，未返回稳定限流错误码或 `X-RateLimit-*` 响应头。这些是现状与限制，不代表故障放行策略已经作为新的平台规范确认。

## 5. 模块发现

System 是模块注册表权威。Gateway 使用自身 Platform Service Access Token 读取快照和 revision watch，只代理满足 `enabled + backend + up + lease valid` 的业务模块实例。同一业务模块的多个有效 Backend 参与请求级轮询，失败请求不做隐式重放。System 是引导控制面，其自注册实例供观测和配置入口声明；Gateway 在监听建立后通过部署提供的 `SYSTEM_URL` 直连 System，以不可路由的 `ingress` 角色注册自身并续租。容器部署用 `SERVICE_HOST=gateway` 提供可定位的实例地址，本地开发使用监听所在主机的地址。Gateway Ingress 不进入业务路由池、动态入口或可路由模块计数，System 与 Gateway 的 `enabled` 固定为 true。

Gateway 使用公共 `/health/live` 与 `/health/ready`；就绪检查为 `system_registry_snapshot`，仅要求成功应用过完整快照。后续 watch 失败不清除已有快照，代理仍逐请求检查 Backend 租约；因此 Ready 不等于所有模块可路由或所有依赖健康。

## 6. 数据库

`gateway.api_access_logs` 保存 API Consumer 的消费访问记录，用于请求性能及限流排查；它与 [Nginx 请求记录](../../nginx/README.md#2-日志查看)、[模块服务正文日志](../../docs/spec/addp模块服务运行日志规范.md)使用不同来源，也不替代 System 操作审计。

当前只在 `POST /api/query/:serviceName/query` 挂载访问日志中间件，顺序为消费凭据认证 → 访问日志 → 限流 → Service 代理。仅当存在已验证的消费方上下文和可用数据库句柄时构建记录，因此 Gateway 限流拒绝或下游失败仍可形成记录；前置认证拒绝、匿名、User Bearer、控制面及其他协议请求不在该表覆盖范围内。

记录字段及解释边界：

- `api_consumer_id` 表达稳定消费方，`api_credential_prefix` 只保存不可认证的短前缀；不采集请求体、认证 Header 或 Cookie。`request_params` 只保存 Query 参数，按参数名匹配敏感字段后以 `[REDACTED]` 替换值，不能据此宣称任意参数内容已完成脱敏。
- `request_method`、`request_path` 记录方法和不含 Query 的路径。`service_name` 取 `/api` 后首段，Query 请求会记为 `query`；定位实际服务须核对 `request_path`，不能将该字段当作精确服务标识。
- `response_status` 记录中间件捕获的响应状态；`rate_limited` 仅由该状态是否为 429 推导，不能区分 Gateway 限流与下游 429。`cache_hit` 只读取请求上下文标记，不代表结果缓存已经接通。
- `response_time_ms` 从进入日志中间件计至下游处理返回，不含此前的消费凭据认证，也不代表客户端端到端耗时；`accessed_at` 为下游返回后构建记录的时刻。

数据库连接或迁移失败时禁用该记录路径；下游处理返回后异步写入，单次写入超时为 5 秒，失败只记运行日志，不改变请求响应。HTTP 响应成功不代表已落库，缺少记录也不证明请求未发生；该表不能作为全入口访问计量或完整审计证据。

## 7. 安全不变量

- 禁止在通用 `/api/v1` 注册 API Consumer 认证中间件；
- 禁止同时接受 Bearer 与 `X-API-Key`；
- 禁止仅凭 Gateway 投影跳过 owner 最终授权；
- 禁止把 API Consumer 建模为 User、Service Principal、Tenant Service Account 或 OAuth Client；
- 禁止恢复旧 Application + `allowed_services []string` 的粗粒度授权。

## 8. 验证

标准入口与验证范围统一见[运维手册](gateway运维手册.md#回归验证)，不在架构正文复制一组语言命令。
