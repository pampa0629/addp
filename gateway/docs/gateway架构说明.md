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
- 日志只保存 `api_consumer_id` 和不可用于认证的短前缀，不保存凭据、Hash、Authorization、Cookie 或请求体。

## 5. 模块发现

System 是模块注册表权威。Gateway 使用自身 Platform Service Access Token读取快照和 revision watch，只代理 `enabled + backend + up + lease valid` 的实例。同一模块的多个有效 Backend 参与请求级轮询，失败请求不做隐式重放。

## 6. 数据库

`gateway.api_access_logs` 保存 API Consumer 数据面请求的路由、状态码、耗时、限流结果和消费方标识。字段 `api_consumer_id` 表达稳定消费方，`api_credential_prefix` 只保存不可认证的短前缀；启动迁移会删除旧 `application_id` 与 `api_key_prefix` 列。

## 7. 安全不变量

- 禁止在通用 `/api/v1` 注册 API Consumer 认证中间件；
- 禁止同时接受 Bearer 与 `X-API-Key`；
- 禁止仅凭 Gateway 投影跳过 owner 最终授权；
- 禁止把 API Consumer 建模为 User、Service Principal、Tenant Service Account 或 OAuth Client；
- 禁止恢复旧 Application + `allowed_services []string` 的粗粒度授权。

## 8. 验证

```bash
cd gateway && go test ./...
cd service/backend && go test ./internal/api
cd system/backend && go test ./internal/api ./internal/service ./internal/migration
make test-authorization
```
