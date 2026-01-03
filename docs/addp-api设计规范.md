# ADDP API 设计规范

版本：v1.1
更新日期：2026-01-03（修订版）
适用范围：ADDP 全平台所有模块（System、Manager、Meta、Transfer、Orchestrator、Develop、Service）

---

## 一、总则

本规范旨在统一 ADDP 全域数据平台各模块的 API 设计风格，提升接口的一致性、可维护性和易用性。

### 1.1 设计原则

1. **统一性** - 所有模块遵循统一的响应格式、命名规范和错误处理
2. **简洁性** - 优先采用简单直观的设计，避免过度工程化
3. **RESTful** - 遵循 REST 架构风格，使用标准 HTTP 方法和语义
4. **向前兼容** - 通过版本控制支持 API 演进
5. **开发友好** - 清晰的文档、统一的错误码、便于调试

### 1.2 适用场景

- ✅ 所有面向前端的 HTTP API（Portal、各模块前端）
- ✅ 模块间的 HTTP API 调用
- ✅ 对外开放的 API（如数据服务 API）
- ⚠️  内部 gRPC/消息队列等非 HTTP 通信可参考但不强制

---

## 二、响应格式规范

### 2.1 统一响应结构

所有 API 响应统一使用以下 JSON 格式：

```json
{
  "code": 0,
  "message": "success",
  "data": <any>
}
```

**字段说明：**

| 字段    | 类型   | 必填 | 说明                                      |
|---------|--------|------|-------------------------------------------|
| code    | int    | 是   | HTTP 状态码（200、400、404、500 等）      |
| message | string | 是   | 提示信息（成功或失败原因，可直接展示给用户）|
| data    | any    | 否   | 实际数据（成功时有值，失败时为 null）      |

#### 设计决策说明

本规范采用 `{code, message, data}` 的统一响应格式，主要基于以下考虑：

**为什么采用这种格式？**
1. **国内开发习惯** - 国内大多数团队习惯这种格式，团队接受度高
2. **前端统一处理** - 前端可以统一拦截器处理所有响应，无需判断HTTP状态码
3. **向后兼容** - 与现有代码保持一致，降低迁移成本
4. **便于调试** - 响应体中包含完整信息，便于日志记录和问题排查

**与国际主流的差异**
- ⚠️ **HTTP状态码冗余** - HTTP状态码已经表达了请求结果，响应体中再包含`code`存在信息重复
- ⚠️ **RESTful原则** - 严格的RESTful设计通常只在错误响应中包含错误对象，成功响应直接返回数据
- 📚 **国际标准** - Google、Microsoft、GitHub等公司的API通常采用更简洁的响应格式

**国际主流做法示例**：
```json
// 成功响应 - 200
{
  "id": 1,
  "name": "PostgreSQL",
  "created_at": "2024-01-01T12:00:00Z"
}

// 错误响应 - 400
{
  "error": {
    "message": "参数错误：用户名不能为空",
    "code": "INVALID_ARGUMENT",
    "details": [...]
  }
}
```

**团队选择**
- ✅ 当前项目采用 `{code, message, data}` 格式，适合内部使用和国内市场
- 💡 未来对外开放API时，可考虑提供更符合国际标准的v2版本
- 🎯 关键是保持一致性，避免同一项目中混用多种响应格式

### 2.2 成功响应示例

**（1）查询单个资源**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "id": 1,
    "name": "PostgreSQL 主库",
    "engine_type": "postgresql",
    "created_at": "2024-01-01T12:00:00Z"
  }
}
```

**（2）查询列表（无分页）**

```json
{
  "code": 200,
  "message": "success",
  "data": [
    {"id": 1, "name": "user1"},
    {"id": 2, "name": "user2"}
  ]
}
```

**（3）查询列表（分页）**

```json
{
  "code": 200,
  "message": "success",
  "data": [
    {"id": 1, "name": "user1"},
    {"id": 2, "name": "user2"}
  ],
  "pagination": {
    "total": 100,
    "page": 1,
    "page_size": 20,
    "total_pages": 5
  }
}
```

**（4）创建资源**

```json
{
  "code": 201,
  "message": "创建成功",
  "data": {
    "id": 123,
    "name": "新引擎"
  }
}
```

**（5）更新/删除资源**

```json
{
  "code": 200,
  "message": "删除成功"
}
```

或返回更新后的完整对象：

```json
{
  "code": 200,
  "message": "更新成功",
  "data": {
    "id": 1,
    "name": "更新后的名称"
  }
}
```

### 2.3 错误响应格式

```json
{
  "code": 400,
  "message": "参数错误：用户名不能为空"
}
```

或包含详细信息（可选，生产环境可隐藏）：

```json
{
  "code": 500,
  "message": "数据库连接失败",
  "error_detail": "connection timeout after 5s"
}
```

### 2.4 分页响应格式

**请求参数：**

```
GET /api/v1/users?page=1&page_size=20
```

| 参数      | 类型 | 默认值 | 说明                          |
|-----------|------|--------|-------------------------------|
| page      | int  | 1      | 页码（从 1 开始）              |
| page_size | int  | 20     | 每页大小（最大 100）           |

**响应格式：**

```json
{
  "code": 200,
  "message": "success",
  "data": [...],
  "pagination": {
    "total": 100,
    "page": 1,
    "page_size": 20,
    "total_pages": 5
  }
}
```

**分页信息字段：**

| 字段        | 类型 | 说明           |
|-------------|------|----------------|
| total       | int  | 总记录数       |
| page        | int  | 当前页码       |
| page_size   | int  | 每页大小       |
| total_pages | int  | 总页数         |

---

## 三、RESTful 设计规范

### 3.1 资源命名规则

1. **使用复数名词**：`/api/v1/users`、`/api/v1/engines`
2. **小写字母 + 下划线**：`/api/v1/audit_logs`（与数据库字段命名一致）
3. **避免动词**：❌ `/api/v1/getUsers`、✅ `/api/v1/users`
4. **使用名词表示资源**：❌ `/api/v1/create_user`、✅ `POST /api/v1/users`

### 3.2 HTTP 方法语义

| 方法   | 语义         | 示例                         | 幂等性 | 响应码      |
|--------|--------------|------------------------------|--------|-------------|
| GET    | 查询资源     | GET /api/v1/users            | 是     | 200         |
| POST   | 创建资源     | POST /api/v1/users           | 否     | 201         |
| PUT    | 完整更新     | PUT /api/v1/users/:id        | 是     | 200         |
| PATCH  | 部分更新     | PATCH /api/v1/users/:id      | 是     | 200         |
| DELETE | 删除资源     | DELETE /api/v1/users/:id     | 是     | 200         |

### 3.3 标准资源操作

```
GET    /api/v1/users           # 列表查询（支持过滤、排序、分页）
POST   /api/v1/users           # 创建用户
GET    /api/v1/users/:id       # 查询单个用户
PUT    /api/v1/users/:id       # 更新用户（完整替换）
PATCH  /api/v1/users/:id       # 更新用户（部分字段）
DELETE /api/v1/users/:id       # 删除用户
```

### 3.4 子资源设计

```
GET    /api/v1/tenants/:id/users        # 获取租户下的用户列表
POST   /api/v1/tenants/:id/users        # 为租户创建用户
DELETE /api/v1/tenants/:id/users/:uid   # 删除租户下的某个用户
```

**规则：**
- 子资源深度建议不超过 2 层
- 超过 2 层时考虑使用查询参数：`GET /api/v1/users?tenant_id=1`

### 3.5 特殊操作的 RESTful 化

**原则：** 优先考虑可读性和直观性，适度资源化，不追求教条式的RESTful。

**核心观点：**
- ✅ **清晰直观优先** - API设计首要目标是让开发者容易理解和使用
- ✅ **合理使用动词** - 对于"触发动作"类操作，使用动词往往更清晰
- ⚠️ **避免过度资源化** - 不要为了资源化而降低可读性

**推荐做法：**

| 操作类型           | 推荐方案                           | 说明                                      |
|--------------------|------------------------------------|-------------------------------------------|
| 测试引擎连接       | POST /engines/:id/test             | 动作清晰，保留动词合理                    |
| 创建前测试连接     | POST /engines/test                 | 临时操作，不创建资源，使用动词合理        |
| 触发扫描任务       | POST /engines/:id/scan             | 触发动作，保留动词直观                    |
| 修改密码           | PUT /users/:id/password            | 密码是用户的子资源，资源化合理            |
| 导出日志           | GET /logs?format=csv&export=true   | 使用查询参数表示格式                      |
| 批量删除           | POST /users/batch_delete           | 批量操作，使用动词清晰                    |
| 用户登录           | POST /auth/login                   | 认证操作，保留动词（业界标准）            |
| Token 刷新         | POST /auth/refresh                 | 认证操作，保留动词（业界标准）            |

**对比说明：**

```bash
# ❌ 过度资源化 - 降低可读性
POST /engines/:id/connection_tests         # 比 /test 更冗长，没有带来额外价值
POST /scan_tasks                            # body需要传engine_id，不如 /engines/:id/scan 直观

# ✅ 推荐做法 - 清晰直观
POST /engines/:id/test                      # 一目了然：测试引擎连接
POST /engines/:id/scan                      # 清楚明确：触发扫描
PUT /users/:id/password                     # 资源化合理：密码是子资源
```

**例外场景（可保留动词）：**
1. **认证相关** - `/auth/login`、`/auth/logout`、`/auth/refresh`（业界标准）
2. **触发动作** - `/test`、`/scan`、`/execute`、`/deploy`（不创建持久资源的操作）
3. **批量操作** - `/batch_delete`、`/batch_update`（涉及多个资源的操作）
4. **特殊操作** - 当资源化会显著降低可读性时

**GitHub API 案例参考：**
- `POST /repos/:owner/:repo/merges` - 合并操作
- `POST /repos/:owner/:repo/dispatches` - 触发工作流
- `PUT /repos/:owner/:repo/topics` - 更新主题

**小结：**
RESTful是指导原则，不是教条。优秀的API设计应该在RESTful原则和实用性之间取得平衡，让开发者能够快速理解和正确使用。

### 3.6 当前需要调整的接口

基于上述原则，以下是当前接口的迁移建议：

| 当前接口                              | 建议调整为                                   | 优先级 |
|---------------------------------------|----------------------------------------------|--------|
| POST /api/engines/:id/test            | POST /api/v1/engines/:id/test                | 高     |
| POST /api/engines/test                | POST /api/v1/engines/test                    | 高     |
| POST /api/engines/:id/scan            | POST /api/v1/engines/:id/scan                | 高     |
| PUT /api/users/:id/change-password    | PUT /api/v1/users/:id/password               | 中     |
| GET /api/logs/export                  | GET /api/v1/logs?format=csv&export=true      | 中     |

**迁移重点：**
1. 主要是添加 `/v1` 版本前缀
2. `change-password` 改为资源化的 `password`
3. `export` 改为查询参数
4. 保留 `test`、`scan` 等动词（根据3.5节原则）

---

## 四、HTTP 状态码规范

### 4.1 状态码语义

**成功：**

| 状态码 | 含义       | 使用场景                          |
|--------|------------|-----------------------------------|
| 200    | OK         | 查询、更新、删除成功              |
| 201    | Created    | 创建资源成功                      |
| 204    | No Content | 删除成功且无需返回内容（不推荐）  |

**客户端错误：**

| 状态码 | 含义                  | 使用场景                                      |
|--------|-----------------------|-----------------------------------------------|
| 400    | Bad Request           | 请求格式错误、JSON 解析失败、缺少必填参数     |
| 401    | Unauthorized          | 认证失败（用户名密码错误、token 无效/过期）   |
| 403    | Forbidden             | 已认证但无权限                                |
| 404    | Not Found             | 资源不存在                                    |
| 409    | Conflict              | 资源冲突（如用户名已存在）                    |
| 422    | Unprocessable Entity  | 业务逻辑验证失败（可选，也可统一用400）       |

**服务端错误：**

| 状态码 | 含义                  | 使用场景                     |
|--------|-----------------------|------------------------------|
| 500    | Internal Server Error | 服务器内部错误               |
| 502    | Bad Gateway           | 网关错误、调用下游服务失败   |
| 503    | Service Unavailable   | 服务不可用                   |

### 4.2 常见场景的状态码选择

| 场景                     | 状态码 | 响应示例                                    |
|--------------------------|--------|---------------------------------------------|
| 用户名密码错误           | 401    | `{"code": 401, "message": "用户名或密码错误"}` |
| Token 无效/过期          | 401    | `{"code": 401, "message": "未登录或 token 已过期"}` |
| JSON 格式错误            | 400    | `{"code": 400, "message": "请求格式错误"}` |
| 缺少必填参数             | 400    | `{"code": 400, "message": "缺少必填参数：用户名"}` |
| 无权限访问资源           | 403    | `{"code": 403, "message": "无权访问该资源"}` |
| 资源不存在               | 404    | `{"code": 404, "message": "引擎不存在"}` |
| 用户名已存在             | 409    | `{"code": 409, "message": "用户名已存在"}` |
| 数据库连接失败           | 500    | `{"code": 500, "message": "数据库连接失败"}` |

**状态码选择要点：**
- **401** - 用于所有认证失败的场景（登录失败、token无效、token过期）
- **400** - 用于请求格式问题（JSON解析失败、参数缺失、参数类型错误）
- **403** - 用于已登录但权限不足
- **422** - 可选，用于业务逻辑验证失败；也可统一使用400

### 4.3 错误处理原则

1. **HTTP 状态码与响应 code 保持一致**
2. **message 应清晰描述错误原因**，可直接展示给用户
3. **生产环境可隐藏 error_detail**，避免泄露敏感信息
4. **统一错误处理中间件**，避免每个 handler 重复处理

---

## 五、API 版本控制

### 5.1 版本策略

**采用 URL 路径版本：**

```
/api/v1/users
/api/v2/users
```

**优点：**
- 直观明确
- 支持同时运行多个版本
- 便于网关路由和灰度发布

### 5.2 版本演进规则

- **破坏性变更**（字段删除、类型变更、接口移除）→ 升级大版本（v1 → v2）
- **新增字段、新增接口** → 不需要升级版本（保持向后兼容）
- **废弃接口**：标记为 `@deprecated`，在下一大版本移除

### 5.3 当前版本

**所有新接口统一使用 `/api/v1/` 前缀**

现有接口需逐步迁移：
- `/api/users` → `/api/v1/users`
- `/api/engines` → `/api/v1/engines`

---

## 六、过滤、排序、搜索规范

### 6.1 过滤参数

通过 Query 参数过滤：

```
GET /api/v1/users?username=admin&is_active=true
GET /api/v1/logs?start_time=2024-01-01&end_time=2024-12-31&action=create
GET /api/v1/engines?engine_type=postgresql&tenant_id=1
```

**规则：**
- 字段名使用 snake_case
- 布尔值使用 `true/false`
- 时间使用 ISO 8601 格式或 Unix 时间戳

### 6.2 排序参数

```
GET /api/v1/users?sort=created_at&order=desc
GET /api/v1/users?sort=created_at&order=asc
```

| 参数  | 值            | 说明           |
|-------|---------------|----------------|
| sort  | 字段名        | 排序字段       |
| order | asc/desc      | 升序/降序      |

**默认排序：**
- 通常按 `created_at desc`（最新的在前）
- 或按 `id desc`

### 6.3 搜索参数

```
GET /api/v1/users?search=张三
GET /api/v1/engines?search=postgres
```

- `search` 参数用于全文搜索（模糊匹配 name、description 等字段）
- 精确匹配使用具体字段名（如 `username=admin`）

---

## 七、命名规范

### 7.1 字段命名

**统一使用 snake_case**（与数据库字段保持一致）

```json
{
  "user_id": 1,
  "created_at": "2024-01-01T12:00:00Z",
  "is_active": true,
  "engine_type": "postgresql"
}
```

**不使用 camelCase**（即使 Go 结构体使用 CamelCase，JSON 输出也要转换为 snake_case）

#### 命名规范的权衡说明

**为什么选择 snake_case？**
1. **与数据库一致** - 数据库字段使用snake_case，保持一致性，减少转换
2. **后端语言习惯** - Go、Python等后端语言更习惯snake_case
3. **避免转换错误** - 减少ORM层的字段映射问题

**与前端生态的差异**
- ⚠️ **JavaScript习惯** - JavaScript/TypeScript生态标准是camelCase
- ⚠️ **国际主流** - Google、AWS、Azure等API通常使用camelCase
- 💡 **前端需要转换** - 前端团队需要在API层进行命名转换

**国际主流做法**：
```json
// Google Cloud API, AWS API 等
{
  "userId": 1,
  "createdAt": "2024-01-01T12:00:00Z",
  "isActive": true,
  "engineType": "postgresql"
}
```

**当前选择的理由**：
- ✅ 后端为主导的项目，优先考虑后端一致性
- ✅ 减少数据库到API的转换复杂度
- ✅ 团队已习惯snake_case风格

**未来考虑**：
- 对外开放的公共API可以考虑提供camelCase选项（通过Accept-Case header）
- 前端工具链（axios interceptor）可自动转换命名风格

### 7.2 时间格式

**统一使用 ISO 8601 格式：**

```
2024-01-01T12:00:00Z           # UTC 时间
2024-01-01T12:00:00+08:00      # 带时区
```

**GORM 自动序列化：**
```go
type User struct {
    CreatedAt time.Time `json:"created_at"`  // 自动转为 ISO 8601
}
```

### 7.3 布尔值

使用 `true/false`，不使用 `1/0` 或字符串 `"true"`/`"false"`

```json
{
  "is_active": true,
  "is_deleted": false
}
```

### 7.4 枚举值

使用字符串常量，不使用数字：

```json
{
  "engine_type": "postgresql",   // ✅ 推荐
  "user_type": "tenant_admin",   // ✅ 推荐
  "status": 1                    // ❌ 不推荐
}
```

---

## 八、认证授权规范

### 8.1 认证方式

**（1）用户认证（JWT）**

```http
Authorization: Bearer <jwt_token>
```

**（2）内部服务认证（API Key）**

```http
X-Internal-API-Key: <secret_key>
```

**（3）应用认证（API Key）**

```http
X-API-Key: <app_api_key>
```

### 8.2 认证失败响应

**未认证（401）：**

```json
{
  "code": 401,
  "message": "未登录或 token 已过期"
}
```

**权限不足（403）：**

```json
{
  "code": 403,
  "message": "无权访问该资源"
}
```

### 8.3 Token 刷新

```
POST /api/v1/auth/refresh
Authorization: Bearer <expired_token>
```

响应：

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "access_token": "new_jwt_token",
    "token_type": "Bearer"
  }
}
```

---

## 九、文档规范

### 9.1 OpenAPI/Swagger 文档

**推荐使用 `swaggo/swag` 自动生成 API 文档**

**示例：**

```go
// @Summary 用户登录
// @Description 用户登录获取 JWT token
// @Tags 认证
// @Accept json
// @Produce json
// @Param request body LoginRequest true "登录信息"
// @Success 200 {object} Response{data=LoginResponse} "登录成功"
// @Failure 400 {object} Response "用户名或密码错误"
// @Failure 401 {object} Response "未授权"
// @Router /api/v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
    // ...
}
```

### 9.2 接口文档要求

每个接口应包含：
1. **接口描述** - 功能说明
2. **请求参数** - 路径参数、Query 参数、Body 参数
3. **响应示例** - 成功和失败的响应
4. **认证要求** - 是否需要 JWT、权限要求
5. **错误码说明** - 可能的错误码和含义

### 9.3 Swagger UI

各模块应提供 Swagger UI 访问入口：

```
http://localhost:8080/swagger/index.html   # System 模块
http://localhost:8081/swagger/index.html   # Manager 模块
```

---

## 十、实施指南

### 10.1 新接口开发

所有新开发的接口**必须**遵循本规范：

1. 使用 `/api/v1/` 前缀
2. 采用统一响应格式（code + message + data）
3. 遵循 RESTful 设计
4. 使用 snake_case 命名
5. 添加 Swagger 注释

### 10.2 现有接口迁移

**迁移策略：**

1. **阶段 1（兼容过渡期）**：
   - 新路由 `/api/v1/xxx` 遵循新规范
   - 旧路由 `/api/xxx` 保留，逐步废弃
   - 前端逐步切换到新接口

2. **阶段 2（完全迁移）**：
   - 移除旧路由
   - 所有接口统一使用 `/api/v1/`

**优先级：**
- 高频接口优先迁移（如用户、引擎、日志）
- 内部接口可延后迁移

### 10.3 共享响应处理

在 `common/api` 模块中提供统一的响应方法：

```go
// RespondSuccess 成功响应
func RespondSuccess(c *gin.Context, data interface{}) {
    c.JSON(http.StatusOK, gin.H{
        "code":    200,
        "message": "success",
        "data":    data,
    })
}

// RespondCreated 创建成功响应
func RespondCreated(c *gin.Context, data interface{}) {
    c.JSON(http.StatusCreated, gin.H{
        "code":    201,
        "message": "创建成功",
        "data":    data,
    })
}

// RespondError 错误响应
func RespondError(c *gin.Context, code int, message string) {
    c.JSON(code, gin.H{
        "code":    code,
        "message": message,
    })
}

// RespondPaginated 分页响应
func RespondPaginated(c *gin.Context, data interface{}, total int64, page, pageSize int) {
    totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
    c.JSON(http.StatusOK, gin.H{
        "code":    200,
        "message": "success",
        "data":    data,
        "pagination": gin.H{
            "total":       total,
            "page":        page,
            "page_size":   pageSize,
            "total_pages": totalPages,
        },
    })
}
```

---

## 十一、常见问题

### Q1: 是否所有操作都要严格资源化？

**A:** 不需要。API设计应该**优先考虑可读性和直观性**，而不是教条式地遵循RESTful。

**资源化适用场景：**
- 操作的结果是创建/修改/删除持久化资源
- 子资源概念清晰（如 `/users/:id/password`）
- 资源化后更易理解

**可以保留动词的场景：**
- 认证类操作：`/auth/login`、`/auth/refresh`（业界标准）
- 触发动作类：`/test`、`/scan`、`/execute`（不创建持久资源）
- 批量操作：`/batch_delete`、`/batch_update`
- 资源化会显著降低可读性的场景

**参考：** GitHub、GitLab等优秀API也在合理场景下使用动词（如 `/repos/:owner/:repo/merges`）。

### Q2: 删除操作返回 200 还是 204？

**A:** 推荐返回 200 + 成功消息，便于前端提示用户。204 不返回 body，前端不好处理。

### Q3: 用户名密码错误返回什么状态码？

**A:** 返回 **401 Unauthorized**（推荐）。

**状态码语义（RFC 9110标准）：**
- **401 Unauthorized** - 认证失败（用户名密码错误、token无效/过期）
- **400 Bad Request** - 请求格式错误（JSON解析失败、缺少必填参数、参数类型错误）
- **403 Forbidden** - 已认证但权限不足

**示例：**
```http
# 用户名密码错误
POST /api/v1/auth/login
Response: 401 {"code": 401, "message": "用户名或密码错误"}

# Token过期
GET /api/v1/users
Response: 401 {"code": 401, "message": "token已过期"}

# JSON格式错误
POST /api/v1/users
Response: 400 {"code": 400, "message": "JSON格式错误"}

# 已登录但无权限
GET /api/v1/admin/users
Response: 403 {"code": 403, "message": "无权访问该资源"}
```

**注意：** 虽然国内一些项目将登录失败归为400（认为是"参数错误"），但这不符合HTTP标准语义。建议遵循RFC标准使用401。

### Q4: 分页信息放在哪里？

**A:** 与 `data` 平级，放在外层：
```json
{
  "code": 200,
  "data": [...],
  "pagination": {...}
}
```

### Q5: 是否需要细分业务错误码（如 10001、20001）？

**A:** 当前不需要。使用 HTTP 状态码 + 清晰的 message 即可。后续如有需要可扩展。

### Q6: 如何处理批量操作？

**A:** 使用 POST 请求，body 传递批量数据：
```
POST /api/v1/users/batch_delete
Body: {"user_ids": [1, 2, 3]}
```

### Q7: 内部 API（/internal）是否也需要遵循规范？

**A:** 是的。内部 API 也应遵循统一响应格式和命名规范，便于维护和调试。

---

## 十二、参考资料

- [Google API Design Guide](https://cloud.google.com/apis/design)
- [Microsoft REST API Guidelines](https://github.com/microsoft/api-guidelines)
- [RESTful API Best Practices](https://restfulapi.net/)
- [HTTP 状态码 RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html)

---

## 附录：快速对照表

### A. HTTP 方法对照

| 操作     | 方法   | 路径                | 响应码 |
|----------|--------|---------------------|--------|
| 列表查询 | GET    | /api/v1/users       | 200    |
| 创建     | POST   | /api/v1/users       | 201    |
| 查询详情 | GET    | /api/v1/users/:id   | 200    |
| 更新     | PUT    | /api/v1/users/:id   | 200    |
| 删除     | DELETE | /api/v1/users/:id   | 200    |

### B. 响应格式对照

| 场景           | code | message     | data        | pagination |
|----------------|------|-------------|-------------|------------|
| 查询单个资源   | 200  | "success"   | {...}       | -          |
| 查询列表       | 200  | "success"   | [...]       | -          |
| 查询分页列表   | 200  | "success"   | [...]       | {...}      |
| 创建成功       | 201  | "创建成功"  | {...}       | -          |
| 更新成功       | 200  | "更新成功"  | {...} 或 -  | -          |
| 删除成功       | 200  | "删除成功"  | -           | -          |
| 参数格式错误   | 400  | "xxx错误"   | -           | -          |
| 认证失败       | 401  | "认证失败"  | -           | -          |
| 无权限         | 403  | "无权限"    | -           | -          |
| 资源不存在     | 404  | "不存在"    | -           | -          |
| 资源冲突       | 409  | "已存在"    | -           | -          |
| 服务器错误     | 500  | "服务器错误"| -           | -          |

### C. 命名规范对照

| 类型         | 规范          | 示例                     |
|--------------|---------------|--------------------------|
| URL 路径     | snake_case    | /api/v1/audit_logs       |
| JSON 字段    | snake_case    | user_id, created_at      |
| 时间格式     | ISO 8601      | 2024-01-01T12:00:00Z     |
| 布尔值       | true/false    | is_active: true          |
| 枚举值       | 字符串        | engine_type: "postgresql"|

---

**本规范最后更新：2026-01-03（v1.1修订版）**
**适用版本：ADDP v0.0.20+**

**主要修订内容：**
1. 增加响应格式设计决策说明，明确与国际主流的差异
2. 修正HTTP状态码使用错误（用户名密码错误改为401）
3. 放宽资源化要求，强调可读性优先原则
4. 增加命名规范的权衡说明
5. 完善常见问题解答
