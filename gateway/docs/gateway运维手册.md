# Gateway 运维手册

## 目录

1. [性能监控](#性能监控)
2. [调试命令](#调试命令)
3. [日志查看](#日志查看)
4. [常见问题排查](#常见问题排查)

---

## 性能监控

### 监控指标

#### API Key 验证性能

**目标**：
- 本地缓存命中率：> 95%
- Redis 缓存命中率：> 90%
- System API 调用延迟：P99 < 50ms

**监控查询**：

```sql
-- 缓存命中率（过去 1 小时）
SELECT
    COUNT(*) FILTER (WHERE cache_hit = TRUE) * 100.0 / COUNT(*) as cache_hit_rate
FROM gateway.api_access_logs
WHERE accessed_at > NOW() - INTERVAL '1 hour';
```

#### 限流统计

**监控指标**：
- 被限流的请求数
- Top 10 限流应用
- 限流触发时段分布

**监控查询**：

```sql
-- 被限流的应用排行（过去 1 天）
SELECT
    application_id,
    COUNT(*) as rate_limited_count
FROM gateway.api_access_logs
WHERE rate_limited = TRUE
  AND accessed_at > NOW() - INTERVAL '1 day'
GROUP BY application_id
ORDER BY rate_limited_count DESC
LIMIT 10;
```

#### 访问日志分析

**监控指标**：
- 请求量 Top 10 服务
- 平均响应时间 Top 10 慢路由
- 4xx/5xx 错误率

**监控查询**：

```sql
-- 请求量 Top 10 服务（过去 1 小时）
SELECT
    service_name,
    COUNT(*) as request_count,
    AVG(response_time_ms) as avg_time
FROM gateway.api_access_logs
WHERE accessed_at > NOW() - INTERVAL '1 hour'
GROUP BY service_name
ORDER BY request_count DESC
LIMIT 10;

-- 慢查询 Top 10（过去 1 小时）
SELECT
    request_method,
    request_path,
    AVG(response_time_ms) as avg_time
FROM gateway.api_access_logs
WHERE accessed_at > NOW() - INTERVAL '1 hour'
GROUP BY request_method, request_path
ORDER BY avg_time DESC
LIMIT 10;

-- 错误率分析（过去 1 小时）
SELECT
    response_status,
    COUNT(*) as count
FROM gateway.api_access_logs
WHERE accessed_at > NOW() - INTERVAL '1 hour'
  AND response_status >= 400
GROUP BY response_status
ORDER BY count DESC;
```

---

## 调试命令

### Redis 缓存调试

```bash
# 连接 Redis
docker exec -it addp-infra-redis redis-cli

# 查看所有 API Key 缓存
KEYS gateway:apikey:*

# 查看特定 API Key 缓存
GET gateway:apikey:abc123...

# 查看 TTL
TTL gateway:apikey:abc123...

# 查看限流状态
GET ratelimit:app:123
TTL ratelimit:app:123

# 清理缓存（调试用）
DEL gateway:apikey:abc123...
```

### PostgreSQL 日志调试

```bash
# 连接 PostgreSQL
psql -h localhost -p 15432 -U addp -d addp

# 切换到 gateway schema
SET search_path TO gateway;

# 查看最近 100 条访问日志
SELECT
    id,
    application_id,
    api_key_prefix,
    service_name,
    request_method,
    request_path,
    response_status,
    response_time_ms,
    cache_hit,
    rate_limited,
    accessed_at
FROM api_access_logs
ORDER BY accessed_at DESC
LIMIT 100;

# 查看特定应用的访问日志
SELECT * FROM api_access_logs
WHERE application_id = 123
ORDER BY accessed_at DESC
LIMIT 50;

# 查看被限流的请求
SELECT * FROM api_access_logs
WHERE rate_limited = TRUE
ORDER BY accessed_at DESC;
```

### 健康检查

```bash
# Gateway 健康检查
curl http://localhost:8000/health

# 响应示例
{
  "status": "ok",
  "service": "gateway"
}

# Gateway 服务列表
curl http://localhost:8000/

# 响应示例
{
  "message": "全域数据平台 API Gateway",
  "version": "1.0.0",
  "modules": {
    "system": "http://localhost:8180",
    "manager": "http://localhost:8081",
    "meta": "http://localhost:8082",
    "transfer": "http://localhost:8083",
    "develop": "http://localhost:8185",
    "service": "http://localhost:8086",
    "copilot": "http://localhost:8087"
  }
}
```

### 测试 API Key 验证

```bash
# 测试有效的 API Key
curl -X GET http://localhost:8000/api/v1/manager/engines \
  -H "X-API-Key: sk_live_your_api_key_here"

# 测试无效的 API Key
curl -X GET http://localhost:8000/api/v1/manager/engines \
  -H "X-API-Key: invalid_key"
# 响应: 401 Unauthorized

# 测试无 API Key
curl -X GET http://localhost:8000/api/v1/manager/engines
# 响应: 透明转发，由目标模块验证 Bearer Token 或判断是否允许公开访问
```

### 测试限流

```bash
# 发送大量请求触发限流（假设限额 1000/分钟）
for i in {1..1100}; do
  curl -X GET http://localhost:8000/api/v1/manager/engines \
    -H "X-API-Key: sk_live_your_api_key_here"
done

# 第 1001 次请求返回：429 Too Many Requests
```

---

## 日志查看

```bash
# Gateway 日志（开发模式）
# 日志文件位置：logs/gateway.log
tail -f logs/gateway.log

# Gateway 日志（Docker 模式）
docker logs -f gateway

# 过滤特定日志
cat logs/gateway.log | grep "API Key validation"
cat logs/gateway.log | grep "Rate limit exceeded"
```

---

## 常见问题排查

### 1. User Access Token 问题

**症状**：
- 用户登录后访问 API 返回 401 Unauthorized
- 错误信息：`Invalid token` 或 `Token expired`

**排查步骤**：

1. **检查 System Token 和 Refresh Cookie 配置**
   ```bash
   grep -E 'ACCESS_TOKEN_EXPIRE_MINUTES|REFRESH_TOKEN_EXPIRE_DAYS' .env
   # 业务模块不配置或解析用户 Token 密钥
   ```

2. **检查 AuthContext 解析是否正常**
   ```bash
   curl -H 'Authorization: Bearer addp_at_...' \
     http://localhost:8180/api/v1/system/auth/context
   ```

3. **检查 System 模块是否正常**
   ```bash
   # System 模块负责 opaque Token、Refresh Token Family 和 AuthContext
   curl http://localhost:8180/health
   ```

**解决方案**：
- 确认 System 数据库和 Redis 可用，业务模块能访问 `/auth/context`
- Access Token 过期时由 Browser AuthSession 使用 HttpOnly Refresh Cookie 静默刷新
- 检查 System 模块日志：`tail -f logs/system-backend.log`

---

### 2. 跨服务调用失败

**症状**：
- Gateway 无法转发请求到后端服务
- 错误信息：`Service Unavailable` 或 `Connection refused`

**排查步骤**：

1. **检查后端服务是否启动**
   ```bash
   # 检查所有服务状态
   ps aux | grep "addp"

   # 或检查特定服务
   curl http://localhost:8180/health  # System
   curl http://localhost:8081/health  # Manager
   curl http://localhost:8082/health  # Meta
   ```

2. **检查 System bootstrap 和模块注册表**
   ```bash
   # Gateway 只静态配置 System；其他模块地址来自 System 注册表
   grep SYSTEM_URL .env
   curl -H "X-Internal-API-Key: ${INTERNAL_API_KEY}" \
     http://localhost:8180/api/v1/internal/modules
   ```

3. **检查网络连接**
   ```bash
   # 测试 Gateway 到后端服务的连接
   telnet localhost 8180
   ```

**解决方案**：
- 启动未运行的后端服务：`bash scripts/dev/start.sh -<模块名>`
- 修正 `SYSTEM_URL`，或恢复目标模块的注册和心跳
- 检查防火墙或网络配置

---

### 3. API Key 验证缓存问题

**症状**：
- 撤销的 API Key 仍然可以访问
- 更新的限流配置未生效

**原因**：
- Gateway 使用三层缓存（本地缓存 5分钟、Redis 1小时、System API）
- 撤销 API Key 后，缓存未及时清理

**解决方案**：

1. **清理 Redis 缓存**
   ```bash
   # 连接 Redis
   docker exec -it addp-infra-redis redis-cli

   # 删除特定 API Key 缓存
   DEL gateway:apikey:abc123...

   # 或删除所有 API Key 缓存
   KEYS gateway:apikey:* | xargs redis-cli DEL
   ```

2. **重启 Gateway**
   ```bash
   # 清理本地缓存的最快方式
   bash scripts/dev/restart.sh -gateway
   ```

---

### 4. 限流误触发

**症状**：
- 正常用户被限流（返回 429）
- 限流阈值配置不合理

**排查步骤**：

1. **查看当前限流状态**
   ```bash
   # 连接 Redis
   docker exec -it addp-infra-redis redis-cli

   # 查看应用的当前请求数
   GET ratelimit:app:123

   # 查看 TTL（距离重置还有多少时间）
   TTL ratelimit:app:123
   ```

2. **查看限流配置**
   ```sql
   -- 连接 PostgreSQL
   psql -h localhost -p 15432 -U addp -d addp

   -- 查看应用的限流配置
   SELECT id, name, rate_limit_per_minute
   FROM system.applications
   WHERE id = 123;
   ```

**解决方案**：

1. **临时重置限流计数**
   ```bash
   # 连接 Redis
   docker exec -it addp-infra-redis redis-cli

   # 删除限流 key
   DEL ratelimit:app:123
   ```

2. **调整限流阈值**
   ```sql
   -- 更新限流配置
   UPDATE system.applications
   SET rate_limit_per_minute = 2000
   WHERE id = 123;
   ```

3. **清理 Gateway 缓存**
   ```bash
   # 重启 Gateway 使新配置生效
   bash scripts/dev/restart.sh -gateway
   ```

---

### 5. 请求响应慢

**症状**：
- API 请求响应时间过长
- 用户体验差

**排查步骤**：

1. **查看慢查询日志**
   ```sql
   -- 查询平均响应时间 > 1000ms 的路由
   SELECT
       request_method,
       request_path,
       AVG(response_time_ms) as avg_time,
       COUNT(*) as count
   FROM gateway.api_access_logs
   WHERE accessed_at > NOW() - INTERVAL '1 hour'
   GROUP BY request_method, request_path
   HAVING AVG(response_time_ms) > 1000
   ORDER BY avg_time DESC;
   ```

2. **检查后端服务性能**
   ```bash
   # 查看后端服务日志
   tail -f logs/manager-backend.log
   tail -f logs/meta-backend.log
   ```

3. **检查数据库性能**
   ```sql
   -- 查看慢查询
   SELECT * FROM pg_stat_activity
   WHERE state = 'active'
   AND query_start < NOW() - INTERVAL '5 seconds';
   ```

**解决方案**：
- 优化后端服务的数据库查询
- 增加数据库索引
- 考虑增加缓存（Redis）
- 如果是特定路由慢，考虑限流或异步处理

---

### 6. Gateway 无法启动

**症状**：
- 执行 `bash scripts/dev/start.sh -gateway` 失败
- 错误信息：`Port already in use` 或 `Database connection failed`

**排查步骤**：

1. **检查端口占用**
   ```bash
   # 检查 8000 端口是否被占用
   lsof -i:8000

   # 或
   netstat -an | grep 8000
   ```

2. **检查数据库连接**
   ```bash
   # 测试 PostgreSQL 连接
   psql -h localhost -p 15432 -U addp -d addp -c "SELECT 1;"
   ```

3. **检查 Redis 连接**
   ```bash
   # 测试 Redis 连接
   docker exec -it addp-infra-redis redis-cli PING
   ```

**解决方案**：

1. **端口被占用**
   ```bash
   # 杀死占用端口的进程
   kill -9 $(lsof -ti:8000)

   # 或修改 Gateway 端口
   # 编辑 .env 文件，修改 GATEWAY_PORT
   ```

2. **数据库连接失败**
   ```bash
   # 启动数据库
   bash scripts/infra/up.sh

   # 检查数据库配置
   cat .env | grep DB_
   ```

3. **查看详细错误日志**
   ```bash
   tail -f logs/gateway.log
   ```

---

## 监控最佳实践

### 1. 定期检查缓存命中率

- 目标：> 95% 本地缓存命中率
- 频率：每天
- 如果命中率低，检查：
  - API Key 是否频繁更新
  - 是否有大量新用户访问

### 2. 监控限流事件

- 目标：限流事件 < 1% 总请求数
- 频率：每小时
- 如果限流频繁，考虑：
  - 提高限流阈值
  - 分析是否有恶意请求
  - 优化后端服务性能

### 3. 定期清理访问日志

- 建议：保留 30 天的访问日志
- 清理脚本：

```sql
-- 删除 30 天前的访问日志
DELETE FROM gateway.api_access_logs
WHERE accessed_at < NOW() - INTERVAL '30 days';

-- 或归档到历史表
INSERT INTO gateway.api_access_logs_archive
SELECT * FROM gateway.api_access_logs
WHERE accessed_at < NOW() - INTERVAL '30 days';

DELETE FROM gateway.api_access_logs
WHERE accessed_at < NOW() - INTERVAL '30 days';
```

### 4. 监控 Gateway 资源使用

```bash
# 查看 Gateway 进程的 CPU 和内存使用
ps aux | grep gateway

# 或使用 top
top -p $(pgrep -f gateway)
```

---

## 应急预案

### 高流量攻击

**症状**：
- 突然大量请求涌入
- 后端服务响应变慢或宕机

**应急措施**：

1. **临时降低限流阈值**
   ```sql
   -- 降低所有应用的限流阈值
   UPDATE system.applications
   SET rate_limit_per_minute = 100;
   ```

2. **识别攻击源**
   ```sql
   -- 查询请求量最大的 IP（需要记录 IP 地址）
   SELECT
       request_params->>'ip' as ip,
       COUNT(*) as count
   FROM gateway.api_access_logs
   WHERE accessed_at > NOW() - INTERVAL '10 minutes'
   GROUP BY request_params->>'ip'
   ORDER BY count DESC
   LIMIT 10;
   ```

3. **封禁恶意 IP**
   - 在防火墙层面封禁
   - 或在 Gateway 增加 IP 黑名单功能

### 数据库故障

**症状**：
- Gateway 无法连接 PostgreSQL
- 访问日志无法写入

**应急措施**：

1. **禁用访问日志记录**
   - 临时关闭访问日志写入，保证 Gateway 正常运行
   - 修改 Gateway 代码或配置

2. **切换到备用数据库**
   - 如果有备用数据库，修改 `.env` 配置
   - 重启 Gateway

### Redis 故障

**症状**：
- Gateway 无法连接 Redis
- 限流功能失效

**应急措施**：

1. **Gateway 自动降级**
   - Gateway 设计：Redis 不可用时，跳过限流检查
   - 所有请求都被允许通过

2. **重启 Redis**
   ```bash
   docker restart addp-infra-redis
   ```

3. **临时禁用限流**
   - 修改 Gateway 配置，跳过限流中间件

---

## 相关文档

- [Gateway 架构说明](gateway架构说明.md) - Gateway 的整体架构和设计
- [Gateway 鉴权限流设计（未实现）](gateway鉴权限流设计（未实现）.md) - 未来的鉴权和限流增强方案
- [ADDP 常见故障排查](../../docs/guide/addp常见故障排查.md) - 平台级故障排查指南
