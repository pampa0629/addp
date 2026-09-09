# Gateway 运维手册

## 健康与依赖

```bash
curl -i http://localhost:8000/health
curl -i http://localhost:8000/ready
```

Gateway 就绪依赖 System 模块注册表快照。Redis 不可用时消费方级限流失败关闭；PostgreSQL 不可用时访问日志降级，但不改变 Service owner 的最终授权。

## 验证 API Consumer 数据面

```bash
curl -i \
  -H 'X-API-Key: addp_api_<credential>' \
  -H 'Content-Type: application/json' \
  -d '{"format":"json"}' \
  http://localhost:8000/api/query/<service-name>/query
```

预期错误：

| 状态 | 错误码 | 含义 |
| --- | --- | --- |
| 400 | `ambiguous_credentials` | 同时发送 Bearer 与消费凭据 |
| 401 | `api_consumer_credential_invalid` | 格式错误、撤销、过期或消费方停用 |
| 401 | `api_consumer_control_plane_denied` | 消费凭据试图访问 `/api/v1/*` |
| 403 | owner 返回的禁止码 | Consumer Tenant 不匹配或未授权精确 Service ID |
| 429 | `rate_limit_exceeded` | 超过该 Consumer 的分钟限额 |
| 503 | `api_consumer_auth_unavailable` | System Runtime 验证暂不可用 |

## 检查限流

Redis 限流键为：

```text
ratelimit:api-consumer:<consumer_id>
```

不要创建或清理凭据验证 Redis 缓存；当前实现只使用进程内 30 秒有效投影缓存。撤销后若 Gateway 仍有短期正缓存，Service 的逐请求验证仍会拒绝请求。

## 检查访问日志

```sql
SELECT api_consumer_id, service_name, response_status, response_time_ms, rate_limited, accessed_at
FROM gateway.api_access_logs
ORDER BY accessed_at DESC
LIMIT 100;
```

日志不得出现完整 `X-API-Key`、凭据 Hash、Authorization、Cookie 或请求体。只允许保存不可用于认证的短前缀。

## 排障顺序

1. 确认请求走 `/api/query/:serviceName/query`，而非 `/api/v1/*`；
2. 确认凭据前缀为 `addp_api_`，且没有同时发送 `Authorization`；
3. 在 System 的“应用接入 > API 消费方”确认 Consumer 和 Credential 均有效；
4. 确认 Consumer 的 Grant 精确包含目标 Query Service ID；
5. 确认目标 Query Service 仍属于同一 Tenant；
6. 查看 Gateway、System、Service 日志区分认证失败、最终授权失败和执行失败。

不得通过直接修改数据库、恢复旧 Application 表、放宽 `/api/v1` 中间件或伪造 Tenant Header 排障。

## 回归验证

```bash
cd gateway && go test ./...
cd service/backend && go test ./internal/api
cd system/backend && go test ./internal/api ./internal/service ./internal/migration
make test-authorization
```
