# Gateway 模块说明

Gateway 是 ADDP 的统一网络入口，负责模块动态路由、正式数据面入口、API 消费方认证与限流，以及消费方访问日志。模块业务授权仍由资源 owner 完成。

## 强制边界

- `/api/v1/:module/*` 是控制面和 owner 管理 API，只接受各 owner 声明的 Bearer Credential；携带 `X-API-Key` 必须直接拒绝。
- `/api/query/:serviceName/query` 是首期 API Consumer 数据面入口。未携带 `X-API-Key` 时保留公开访问或 User Bearer 主路径；携带时只接受 `addp_api_` 消费凭据。
- API Consumer 不生成 Principal、Role Assignment 或 AuthContext，不得模拟 User、Service Principal 或 OAuth Client。
- Gateway 只认证消费方、执行消费方级限流并记录访问；Service 必须再次校验凭据，并按 Tenant 与精确 `(service_type, service_id)` Grant 最终授权。
- 同一请求不得同时携带 `Authorization` 与 `X-API-Key`。

## API 消费凭据流程

```text
X-API-Key
  -> Gateway 计算 SHA256
  -> 以 platform.gateway_runtime Service Access Token 调用 System Runtime 验证
  -> 30 秒本地正缓存（不缓存失败结果，不写 Redis 正缓存）
  -> 按 API Consumer ID 执行 Redis 分钟限流
  -> 记录 api_consumer_id 与不可认证的短前缀
  -> 转发到 Service
  -> Service 再次调用 System 验证
  -> 校验 Consumer Tenant == Query Service Tenant
  -> 校验精确 query Service ID Grant
```

凭据撤销最多受 Gateway 30 秒正缓存影响；Service 当前逐请求验证，因此 owner 最终授权不会依赖 Gateway 缓存。

## 关键文件

- `internal/router/router.go`：控制面和数据面路由边界。
- `internal/middleware/api_consumer_auth.go`：消费凭据认证与控制面拒绝中间件。
- `internal/middleware/rate_limiter.go`：按 API Consumer ID 限流。
- `internal/middleware/access_logger.go`：消费方访问日志。
- `internal/cache/local_cache.go`：30 秒本地正缓存。
- `pkg/client/system_client.go`：System Runtime 与模块发现客户端。
- `docs/gateway架构说明.md`：当前架构与协议说明。

## System 依赖

- `GET /api/v1/system/runtime/api-consumer-credentials/validate?key_hash=<sha256>`
- `GET /api/v1/system/runtime/modules`
- 认证统一使用 Gateway 自身 Platform Service Access Token。

## 验证

```bash
cd gateway && go test ./...
bash scripts/swagger/check-route-coverage.sh system
```

不得恢复通用 `/api/v1` API Key 中间件、Redis 长期凭据正缓存、Application ID 授权、调用方自报 Tenant，或仅由 Gateway 完成最终数据授权。
