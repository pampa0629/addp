# 资源缓存同步架构（Redis Pub/Sub）

## 概述

本文档描述 ADDP 平台基于 Redis Pub/Sub 的资源缓存同步机制，确保 System 模块的存储引擎配置变更后，各模块能够实时感知并更新缓存。

## 设计原则

1. **只存储 res_id**：除 System 模块外，其他模块只存储 `res_id`，不存储具体连接信息
2. **按需缓存**：各模块根据自身需要进行 TTL 缓存，避免频繁调用 System API
3. **实时同步**：通过 Redis Pub/Sub 实现资源变更事件的实时推送，保证缓存一致性

## 架构组件

### 1. Redis 频道定义

```
resource:changed     # 资源变更事件（创建、更新、删除）
resource:deleted     # 资源删除事件（需要清理所有相关数据）
```

### 2. 事件消息格式

```json
{
  "resource_id": 123,
  "action": "create|update|delete",
  "timestamp": "2025-01-22T10:00:00Z"
}
```

## 模块实现

### System 模块（事件发布者）

**职责**：当资源配置发生变更时，发布事件到 Redis

**实现位置**：
- [system/backend/internal/service/resource_service.go](../system/backend/internal/service/resource_service.go)

**关键代码**：
```go
// 创建资源时发布事件
if s.eventPublisher != nil {
    _ = s.eventPublisher.PublishResourceChange(context.Background(), resource.ID, events.ActionCreate)
}

// 更新资源时发布事件
if s.eventPublisher != nil {
    _ = s.eventPublisher.PublishResourceChange(context.Background(), resource.ID, events.ActionUpdate)
}

// 删除资源时发布事件
if s.eventPublisher != nil {
    _ = s.eventPublisher.PublishResourceChange(context.Background(), resource.ID, events.ActionDelete)
}
```

**配置项**：
```bash
# .env
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=addp_redis
REDIS_DB=0
```

### Meta 模块（事件订阅者）

**职责**：订阅资源变更事件，自动清除缓存

**实现位置**：
- [meta/backend/internal/service/resource_service.go](../meta/backend/internal/service/resource_service.go)

**缓存策略**：
- TTL: 5 分钟
- 缓存命中：直接返回
- 缓存过期：从 System API 重新获取
- 收到变更事件：立即清除缓存

**事件处理逻辑**：
```go
func (s *ResourceService) handleResourceChangeEvent(event events.ResourceChangeEvent) error {
    switch event.Action {
    case events.ActionCreate:
        // 资源创建：不需要特殊处理，等待下次访问时自动加载

    case events.ActionUpdate:
        // 资源更新：清除缓存，强制下次访问时重新获取
        s.ClearResourceCache(event.ResourceID)

    case events.ActionDelete:
        // 资源删除：清除缓存
        s.ClearResourceCache(event.ResourceID)
        // TODO: 可以考虑删除相关的元数据（meta_node, meta_item）
    }
    return nil
}
```

**配置项**：
```bash
# .env
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=addp_redis
REDIS_DB=0
```

### Manager 模块（事件订阅者）✅

**职责**：订阅资源变更事件，自动清除缓存

**实现位置**：
- [manager/backend/internal/service/resource_cache.go](../manager/backend/internal/service/resource_cache.go)

**缓存策略**：
- TTL: 3 分钟
- 缓存命中：直接返回
- 缓存过期：从 System API 重新获取
- 收到变更事件：立即清除缓存

**事件处理逻辑**：
```go
func (s *ResourceCacheService) handleResourceChangeEvent(event events.ResourceChangeEvent) error {
    switch event.Action {
    case events.ActionUpdate:
        s.ClearResourceCache(event.ResourceID)
    case events.ActionDelete:
        s.ClearResourceCache(event.ResourceID)
    }
    return nil
}
```

**配置项**：
```bash
# .env
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=addp_redis
REDIS_DB=0
```

**状态**：✅ **已实现**

### Transfer 模块（事件订阅者）✅

**职责**：订阅资源变更事件，自动清除缓存，降低 API 调用频率

**实现位置**：
- [transfer/backend/internal/service/local_resource_service.go](../transfer/backend/internal/service/local_resource_service.go)

**缓存策略**：
- TTL: 2 分钟（较短，适合高频任务）
- 缓存命中：直接返回
- 缓存过期：从 System API 重新获取
- 收到变更事件：立即清除缓存

**特点**：
- 针对高频任务优化，使用较短 TTL
- 与任务队列共享 Redis 连接
- 对于频繁执行的传输任务，缓存可显著降低 System API 压力

**配置项**：
```bash
# .env
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=addp_redis
```

**状态**：✅ **已实现**

## 通用组件

### common/events 包

**位置**：[common/events/resource_event.go](../common/events/resource_event.go)

**核心类型**：

```go
// 事件发布器（System 模块使用）
type ResourceEventPublisher struct {
    redis  *redis.Client
    logger *slog.Logger
}

// 事件订阅器（其他模块使用）
type ResourceEventSubscriber struct {
    redis   *redis.Client
    logger  *slog.Logger
    handler ResourceChangeHandler
}

// 事件处理函数类型
type ResourceChangeHandler func(event ResourceChangeEvent) error
```

**使用示例**：

```go
// 发布事件（System）
publisher := events.NewResourceEventPublisher(redisClient, logger)
publisher.PublishResourceChange(ctx, resourceID, events.ActionUpdate)

// 订阅事件（其他模块）
subscriber := events.NewResourceEventSubscriber(
    redisClient,
    handleResourceChangeEvent, // 处理函数
    logger,
)
go subscriber.Start() // 后台启动订阅
```

## 配置说明

### 环境变量

所有模块需要添加以下 Redis 配置：

```bash
# Redis 连接配置
REDIS_ADDR=localhost:6379           # Redis 服务器地址
REDIS_PASSWORD=addp_redis           # Redis 密码（可选）
REDIS_DB=0                          # Redis 数据库编号
```

### Docker Compose

确保 Redis 服务已启动：

```yaml
# docker-compose.yml
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    command: redis-server --requirepass addp_redis
    volumes:
      - redis_data:/data
```

## 工作流程

### 正常流程（缓存命中）

```
1. Meta 模块请求 resource_id=123
   ↓
2. 检查缓存（未过期）
   ↓
3. 返回缓存数据
```

### 缓存过期流程

```
1. Meta 模块请求 resource_id=123
   ↓
2. 检查缓存（已过期）
   ↓
3. 调用 System API 获取最新数据
   ↓
4. 更新缓存（设置新的过期时间）
   ↓
5. 返回数据
```

### 资源变更流程

```
1. 用户在 System 模块更新资源配置
   ↓
2. System 发布 Redis 事件
   {
     "resource_id": 123,
     "action": "update",
     "timestamp": "2025-01-22T10:00:00Z"
   }
   ↓
3. Meta/Manager/Transfer 订阅器收到事件
   ↓
4. 各模块清除本地缓存
   s.ClearResourceCache(123)
   ↓
5. 下次访问时自动从 System 获取最新配置
```

## 故障处理

### Redis 不可用

**行为**：
- System 模块：事件发布失败，但不阻塞业务逻辑
- 其他模块：订阅器停止，缓存依赖 TTL 自然过期

**降级策略**：
- 缓存 TTL 足够短（1-5 分钟），最终会自动刷新
- 重要操作前可以主动调用 `ClearCache()` 强制刷新

### 缓存不一致

**场景**：Redis 消息丢失或订阅器故障

**解决方案**：
1. TTL 机制保底：最多 5 分钟后自动失效
2. 手动清除接口：提供 `/internal/cache/clear` 接口手动清除缓存
3. 健康检查：定期检查 Redis 连接状态

## 监控与日志

### 关键日志

**System 模块**：
```
✅ Redis 客户端已初始化: localhost:6379
📤 resource event published: resource_id=123, action=update
```

**Meta 模块**：
```
✅ 资源事件订阅器已启动
📥 收到资源变更事件: resource_id=123, action=update
🗑️ 资源已更新，缓存已清除: resource_id=123
```

### 监控指标

建议监控以下指标：
- Redis 连接状态
- 事件发布/订阅成功率
- 缓存命中率
- 缓存失效次数

## 测试验证

### 手动测试步骤

1. **启动所有服务**：
```bash
make dev-start
```

2. **创建资源**：
```bash
curl -X POST http://localhost:8080/api/resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "测试数据库",
    "resource_type": "postgresql",
    "connection_info": {
      "host": "localhost",
      "port": 5432,
      "database": "testdb",
      "user": "testuser",
      "password": "testpass"
    }
  }'
```

3. **Meta 模块访问资源**（触发缓存）：
```bash
curl http://localhost:8082/api/resources?tenant_id=1 \
  -H "Authorization: Bearer $TOKEN"
```

4. **更新资源配置**：
```bash
curl -X PUT http://localhost:8080/api/resources/123 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "connection_info": {
      "host": "newhost",
      "port": 5433
    }
  }'
```

5. **验证 Meta 模块缓存已清除**（查看日志）：
```bash
tail -f logs/meta-backend.log | grep "资源已更新"
```

6. **再次访问资源**（应该获取新配置）：
```bash
curl http://localhost:8082/api/resources?tenant_id=1 \
  -H "Authorization: Bearer $TOKEN"
```

### 自动化测试

TODO: 编写集成测试脚本

## 未来优化

1. **代码重构** 🔄：
   - 提取通用缓存逻辑到 `common/cache` 包
   - Meta、Manager、Transfer 使用统一的缓存服务
   - 避免重复代码，提高可维护性

2. **Manager API 改造** 📋：
   - 修改预览接口从传递 `Resource` 对象改为 `resource_id`
   - 后端通过缓存服务获取资源连接信息
   - 避免前端传递敏感信息

3. **缓存预热** 🚀：
   - 服务启动时预加载常用资源
   - Meta 模块已实现 `PreloadResources()`
   - Manager 和 Transfer 可参考实现

4. **监控仪表板** 📊：
   - 缓存命中率可视化
   - 事件流量监控
   - Redis 连接状态检查
   - 异常告警

## 参考资料

- [Redis Pub/Sub Documentation](https://redis.io/docs/manual/pubsub/)
- [Go Redis Client](https://github.com/redis/go-redis)
- [CLAUDE.md - Configuration Center Pattern](../CLAUDE.md#configuration-center-pattern)
