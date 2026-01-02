# audit_logs 表结构和 API 说明

## 一、表结构概览

`system.audit_logs` 表是 ADDP 平台的审计日志表,负责自动记录所有用户操作(非 GET 请求)。支持按租户隔离查询,用于安全审计、操作回溯和合规检查。

### 核心功能

- **自动记录**:通过中间件自动捕获所有 POST/PUT/DELETE 操作
- **租户隔离**:按 tenant_id 隔离日志记录和查询
- **操作追溯**:记录操作用户、时间、IP、请求详情
- **资源关联**:记录操作对象类型和 ID
- **安全审计**:支持按用户、时间、操作类型查询

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 日志唯一标识 |
| `user_id` | INTEGER | FK → users.id, NULLABLE, INDEXED | 操作用户 ID(匿名操作为 NULL) |
| `username` | VARCHAR(255) | | 用户名快照(冗余存储,防止用户删除) |
| `tenant_id` | INTEGER | FK → tenants.id, NULLABLE, INDEXED | 租户 ID(SuperAdmin 操作为 NULL) |
| `action` | VARCHAR(255) | NOT NULL | 操作类型(HTTP 方法 + 路径) |
| `engine_type` | VARCHAR(255) | | 操作对象类型(engine/user/tenant/task 等) |
| `engine_id` | VARCHAR(255) | | 操作对象 ID |
| `details` | TEXT | | 操作详情(JSON 格式,包含请求体) |
| `ip_address` | VARCHAR(255) | | 客户端 IP 地址 |
| `created_at` | TIMESTAMP | DEFAULT NOW(), INDEXED | 操作时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 类型 | 说明 |
|--------|------|------|------|
| `idx_audit_logs_user` | `user_id` | 普通索引 | 按用户查询优化 |
| `idx_audit_logs_tenant` | `tenant_id` | 普通索引 | 租户隔离查询优化 |
| `idx_audit_logs_created` | `created_at` | 普通索引 | 按时间降序查询优化 |

### 2.3 外键关系

| 字段 | 引用表 | 说明 |
|------|--------|------|
| `user_id` | `system.users.id` | 操作用户(可为 NULL) |
| `tenant_id` | `system.tenants.id` | 操作租户(SuperAdmin 为 NULL) |

---

## 三、Go 模型定义

### 3.1 AuditLog 模型

```go
package models

type AuditLog struct {
    ID           uint      `gorm:"primaryKey" json:"id"`
    UserID       *uint     `gorm:"index" json:"user_id"`
    Username     string    `json:"username"`
    TenantID     *uint     `gorm:"index" json:"tenant_id"`
    Action       string    `gorm:"not null" json:"action"`
    EngineType   string    `json:"engine_type"`   // 改名为 EntityType 更合适
    EngineID     string    `json:"engine_id"`     // 改名为 EntityID 更合适
    Details      string    `gorm:"type:text" json:"details"`
    IPAddress    string    `json:"ip_address"`
    CreatedAt    time.Time `gorm:"index" json:"created_at"`
}
```

**注意**:
- `EngineType` 和 `EngineID` 字段命名历史遗留,实际用于记录任意资源类型
- 建议未来重构为 `EntityType` 和 `EntityID`

### 3.2 请求 DTO

```go
// 创建日志请求(内部使用)
type AuditLogCreateRequest struct {
    Action     string `json:"action" binding:"required"`
    EngineType string `json:"engine_type"`
    EngineID   string `json:"engine_id"`
    Details    string `json:"details"`
}
```

---

## 四、自动记录机制

### 4.1 中间件实现

**触发条件**:
- 所有 **非 GET 请求**(POST/PUT/DELETE/PATCH)
- 通过认证的请求(有 JWT Token)
- 排除内部 API 路径(`/internal/*`)

**记录时机**:
- 请求处理**之后**
- 获取响应状态码
- 提取请求体和用户信息

**实现位置**:`system/backend/internal/middleware/logger.go`

### 4.2 记录字段映射

| 字段 | 来源 | 说明 |
|------|------|------|
| `user_id` | JWT Payload | 从 Context 提取 |
| `username` | JWT Payload | 从 Context 提取 |
| `tenant_id` | JWT Payload | 从 Context 提取(SuperAdmin 为 NULL) |
| `action` | HTTP Request | `{METHOD} {PATH}`,如 `POST /api/engines` |
| `engine_type` | 请求体 | 根据路径推断或从请求体提取 |
| `engine_id` | 请求体/URL | 从 URL 参数或响应提取 |
| `details` | 请求体 | JSON 序列化的请求体 |
| `ip_address` | HTTP Request | `c.ClientIP()` |
| `created_at` | 自动 | 数据库默认值 |

### 4.3 示例中间件代码

```go
func LoggerMiddleware(logService *service.LogService, userRepo *repository.UserRepository) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 跳过 GET 请求和内部 API
        if c.Request.Method == "GET" || strings.HasPrefix(c.Request.URL.Path, "/internal") {
            c.Next()
            return
        }

        // 读取请求体
        var bodyBytes []byte
        if c.Request.Body != nil {
            bodyBytes, _ = io.ReadAll(c.Request.Body)
            c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
        }

        // 处理请求
        c.Next()

        // 提取用户信息
        userID, _ := c.Get("user_id")
        username, _ := c.Get("username")
        tenantID, _ := c.Get("tenant_id")

        // 创建日志记录
        log := models.AuditLog{
            UserID:     getUserIDPointer(userID),
            Username:   username.(string),
            TenantID:   getTenantIDPointer(tenantID),
            Action:     fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path),
            Details:    string(bodyBytes),
            IPAddress:  c.ClientIP(),
        }

        // 异步保存日志
        go logService.CreateLog(&log)
    }
}
```

---

## 五、API 端点说明

### 5.1 GET /api/logs - 查询审计日志

**权限**:
- TenantAdmin:查看本租户日志
- SuperAdmin:查看所有租户日志

**请求头**:

```
Authorization: Bearer <jwt_token>
```

**查询参数**:
- `page`(可选):页码,默认 1
- `page_size`(可选):每页条数,默认 20
- `user_id`(可选):按用户过滤

**响应**(200 OK):

```json
{
  "logs": [
    {
      "id": 100,
      "user_id": 2,
      "username": "admin",
      "tenant_id": 1,
      "action": "POST /api/engines",
      "engine_type": "engine",
      "engine_id": "5",
      "details": "{\"name\":\"PostgreSQL-测试\",\"engine_type\":\"postgresql\"}",
      "ip_address": "127.0.0.1",
      "created_at": "2026-01-01T10:30:00Z"
    },
    {
      "id": 99,
      "user_id": 2,
      "username": "admin",
      "tenant_id": 1,
      "action": "PUT /api/users/3",
      "engine_type": "user",
      "engine_id": "3",
      "details": "{\"full_name\":\"新名字\"}",
      "ip_address": "127.0.0.1",
      "created_at": "2026-01-01T10:25:00Z"
    }
  ],
  "total": 2,
  "page": 1,
  "page_size": 20
}
```

**租户隔离**:
- TenantAdmin 自动过滤 `WHERE tenant_id = <当前租户>`
- SuperAdmin 可查看所有日志(包括 tenant_id=NULL 的系统操作)

---

### 5.2 GET /api/logs/:id - 获取指定日志

**权限**:本租户用户 / SuperAdmin

**响应**(200 OK):

```json
{
  "id": 100,
  "user_id": 2,
  "username": "admin",
  "tenant_id": 1,
  "action": "POST /api/engines",
  "engine_type": "engine",
  "engine_id": "5",
  "details": "{\"name\":\"PostgreSQL-测试\",\"engine_type\":\"postgresql\",\"connection_info\":{\"host\":\"localhost\",\"port\":\"5432\"}}",
  "ip_address": "127.0.0.1",
  "created_at": "2026-01-01T10:30:00Z"
}
```

**响应**(403 Forbidden):

```json
{
  "error": "无权限访问此日志"
}
```

---

## 六、记录场景示例

### 6.1 创建引擎

**操作**:

```bash
curl -X POST http://localhost:8080/api/engines \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "PostgreSQL-测试",
    "engine_type": "postgresql",
    "connection_info": {"host": "localhost"}
  }'
```

**生成日志**:

```json
{
  "user_id": 2,
  "username": "admin",
  "tenant_id": 1,
  "action": "POST /api/engines",
  "engine_type": "engine",
  "engine_id": "5",
  "details": "{\"name\":\"PostgreSQL-测试\",\"engine_type\":\"postgresql\"}",
  "ip_address": "192.168.1.100"
}
```

---

### 6.2 更新用户信息

**操作**:

```bash
curl -X PUT http://localhost:8080/api/users/3 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"full_name": "新名字"}'
```

**生成日志**:

```json
{
  "user_id": 2,
  "username": "admin",
  "tenant_id": 1,
  "action": "PUT /api/users/3",
  "engine_type": "user",
  "engine_id": "3",
  "details": "{\"full_name\":\"新名字\"}",
  "ip_address": "192.168.1.100"
}
```

---

### 6.3 删除租户(SuperAdmin)

**操作**:

```bash
curl -X DELETE http://localhost:8080/api/tenants/2 \
  -H "Authorization: Bearer $SUPERADMIN_TOKEN"
```

**生成日志**:

```json
{
  "user_id": 1,
  "username": "SuperAdmin",
  "tenant_id": null,
  "action": "DELETE /api/tenants/2",
  "engine_type": "tenant",
  "engine_id": "2",
  "details": "{}",
  "ip_address": "192.168.1.100"
}
```

---

## 七、使用示例

### 7.1 查询所有日志

```bash
TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."

curl "http://localhost:8080/api/logs?page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

---

### 7.2 查询指定用户的操作日志

```bash
curl "http://localhost:8080/api/logs?user_id=2&page=1&page_size=50" \
  -H "Authorization: Bearer $TOKEN"
```

---

### 7.3 查询特定时间段的日志(SQL)

```sql
-- 查询最近 24 小时的日志
SELECT * FROM system.audit_logs
WHERE created_at > NOW() - INTERVAL '24 hours'
  AND tenant_id = 1
ORDER BY created_at DESC;
```

---

### 7.4 统计操作类型分布(SQL)

```sql
-- 统计各操作类型的次数
SELECT
  SPLIT_PART(action, ' ', 1) AS method,
  COUNT(*) AS count
FROM system.audit_logs
WHERE tenant_id = 1
  AND created_at > NOW() - INTERVAL '7 days'
GROUP BY method
ORDER BY count DESC;
```

**结果**:

```
method | count
-------+------
POST   | 150
PUT    | 80
DELETE | 20
```

---

## 八、权限控制

### 8.1 查询权限

| 用户类型 | 查询范围 | 说明 |
|---------|---------|------|
| SuperAdmin | 所有日志 | 包括所有租户和系统操作 |
| TenantAdmin | 本租户日志 | 仅查看 tenant_id=<当前租户> 的日志 |
| User | 无权限 | 普通用户不能查看审计日志 |

### 8.2 自动隔离

**中间件实现**:

```go
func (h *LogHandler) List(c *gin.Context) {
    userType := c.GetString("user_type")
    tenantID := c.GetUint("tenant_id")

    // 构建查询条件
    query := h.logService.GetDB()

    // 非 SuperAdmin 自动过滤租户
    if userType != "super_admin" {
        query = query.Where("tenant_id = ?", tenantID)
    }

    // 继续处理...
}
```

---

## 九、数据保留策略

### 9.1 推荐策略

| 环境 | 保留期 | 清理方式 |
|------|-------|---------|
| 开发环境 | 30 天 | 自动清理脚本 |
| 测试环境 | 90 天 | 自动清理脚本 |
| 生产环境 | 1 年 | 归档到对象存储 |
| 合规要求 | 3-7 年 | 归档到冷存储 |

### 9.2 清理脚本示例

```sql
-- 删除 90 天前的日志
DELETE FROM system.audit_logs
WHERE created_at < NOW() - INTERVAL '90 days';
```

**建议**:
- 使用定时任务(Cron)执行清理
- 清理前先归档到 MinIO/S3
- 重要租户的日志单独保留

---

## 十、重要说明

### 10.1 字段命名历史遗留

**当前命名**:
- `engine_type`:实际用于存储任意资源类型(engine/user/tenant/task 等)
- `engine_id`:实际用于存储任意资源 ID

**建议重构**:
- 改名为 `entity_type` 和 `entity_id`
- 兼容旧数据(保留字段,添加新字段)

### 10.2 性能优化

**索引使用**:
- 按时间查询:使用 `idx_audit_logs_created`
- 按用户查询:使用 `idx_audit_logs_user`
- 按租户查询:使用 `idx_audit_logs_tenant`

**查询优化**:
- 避免全表扫描
- 使用分页查询
- 添加时间范围限制

### 10.3 敏感信息处理

**不记录的内容**:
- 密码原文(仅记录 password 字段存在)
- 加密密钥
- JWT Token

**Details 字段**:
- 记录完整请求体
- 敏感字段应在业务层过滤
- 建议添加脱敏逻辑

---

## 十一、相关文档

- [users 表](./users表.md) - 用户表,记录操作用户
- [tenants 表](./tenants表.md) - 租户表,日志按租户隔离
- [engines 表](./engines表.md) - 引擎配置表,操作被记录
- [数据库架构](../数据库架构.md) - System 模块整体架构
- [System 模块说明](../../system/CLAUDE.md) - 模块整体架构和设计理念
