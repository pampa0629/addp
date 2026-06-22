# ADDP API 设计规范

版本：v1.2
更新日期：2026-01-06
适用范围：ADDP 全平台所有模块（System、Manager、Meta、Transfer、Orchestrator、Develop、Service）

---

## 一、总则

本规范旨在统一 ADDP 全域数据平台各模块的 API 设计风格，提升接口的一致性、可维护性和易用性。

### 1.1 设计原则

1. **统一性** - 所有模块遵循统一的响应格式、命名规范和错误处理
2. **简洁性** - 优先采用简单直观的设计，避免过度工程化
3. **RESTful** - 遵循 REST 架构风格，使用标准 HTTP 方法和语义
4. **版本化演进** - 通过统一版本前缀管理接口契约，避免隐式旧路径
5. **开发友好** - 清晰的文档、统一的错误码、便于调试

### 1.2 适用场景

- ✅ 所有面向前端的 HTTP API（Console、各模块前端）
- ✅ 模块间的 HTTP API 调用
- ✅ 对外开放的 API（如数据服务 API）
- ⚠️  内部 gRPC/消息队列等非 HTTP 通信可参考但不强制

---

## 二、响应格式规范

### 2.1 灵活响应策略（推荐）

ADDP 采用**灵活响应策略**，根据场景选择最合适的响应格式，避免过度包装，强调简洁高效。

#### 核心原则

1. **HTTP 状态码语义优先** - 充分利用 HTTP 状态码表达请求结果
2. **简单场景直接返回** - CRUD 操作直接返回数据或资源对象
3. **复杂场景适度包装** - 列表查询包含分页信息，错误响应包含错误描述
4. **避免信息冗余** - 不在响应体中重复 HTTP 状态码信息

#### 设计决策说明

**为什么采用灵活响应？**
1. **简洁高效** - 减少不必要的包装层，降低响应体积
2. **符合国际主流** - Google、Microsoft、GitHub 等公司的 API 都采用类似策略
3. **充分利用 HTTP 语义** - HTTP 状态码已经表达了请求结果，无需在 body 中重复
4. **前端易于处理** - 现代前端框架（Axios、Fetch）都能很好地处理 HTTP 状态码

**与传统 `{code, message, data}` 包装格式的对比**：

```json
// 传统包装格式（冗余）
{
  "code": 200,
  "message": "success",
  "data": {"id": 1, "username": "admin"}
}

// 灵活响应（简洁，推荐）
{
  "id": 1,
  "username": "admin"
}
// HTTP 200 状态码已经表达了成功
```

**适用场景**：
- ✅ 内部 API 和外部开放 API 均适用
- ✅ 快速迭代期保持灵活性
- ✅ 符合 RESTful 最佳实践

**参考实现**：System 模块已采用此策略，可作为其他模块的参考标准

### 2.2 成功响应示例

**（1）查询单个资源 - 直接返回对象**

```json
// HTTP 200 OK
{
  "id": 1,
  "name": "PostgreSQL 主库",
  "engine_type": "postgresql",
  "created_at": "2024-01-01T12:00:00Z"
}
```

**（2）创建资源 - 返回新创建的对象**

```json
// HTTP 201 Created
{
  "id": 123,
  "name": "新引擎",
  "created_at": "2024-01-06T10:00:00Z"
}
```

**（3）查询列表（无分页）- 直接返回数组**

```json
// HTTP 200 OK
[
  {"id": 1, "name": "user1"},
  {"id": 2, "name": "user2"}
]
```

**（4）查询列表（分页）- 包含分页信息**

```json
// HTTP 200 OK
{
  "data": [
    {"id": 1, "name": "user1"},
    {"id": 2, "name": "user2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20,
  "total_pages": 5
}
```

**（5）更新资源 - 返回更新后的对象**

```json
// HTTP 200 OK
{
  "id": 1,
  "name": "更新后的名称",
  "updated_at": "2024-01-06T10:00:00Z"
}
```

**（6）删除资源 - 返回简洁消息（可选）**

```json
// HTTP 200 OK
{
  "message": "删除成功"
}
```

或直接返回空响应（HTTP 204 No Content），但不推荐，因为前端处理不便

### 2.3 错误响应格式

**使用简洁的 `{error}` 格式，HTTP 状态码表达错误类型：**

```json
// HTTP 400 Bad Request
{
  "error": "参数错误：用户名不能为空"
}
```

**可选：包含详细信息（开发/测试环境）**

```json
// HTTP 500 Internal Server Error
{
  "error": "数据库连接失败",
  "detail": "connection timeout after 5s"
}
```

**注意**：
- 生产环境应隐藏 `detail` 字段，避免泄露敏感信息
- HTTP 状态码已经表达了错误类型（400/401/403/404/500），无需在 body 中重复
- 错误消息应该清晰、可读，可以直接展示给用户

### 2.4 分页响应格式

**请求参数：**

```
GET /api/v1/system/users?page=1&page_size=20
```

| 参数      | 类型 | 默认值 | 说明                          |
|-----------|------|--------|-------------------------------|
| page      | int  | 1      | 页码（从 1 开始）              |
| page_size | int  | 20     | 每页大小（最大 100）           |

**响应格式：**

```json
// HTTP 200 OK
{
  "data": [
    {"id": 1, "name": "user1"},
    {"id": 2, "name": "user2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20,
  "total_pages": 5
}
```

**分页信息字段：**

| 字段        | 类型 | 说明           |
|-------------|------|----------------|
| data        | array| 当前页的数据   |
| total       | int  | 总记录数       |
| page        | int  | 当前页码       |
| page_size   | int  | 每页大小       |
| total_pages | int  | 总页数         |

**例外：TaskProvider 标准任务列表**

TaskProvider 是跨模块任务编排契约，不使用本节的通用分页列表格式。标准 `GET /tasks?task_type=` 必须返回：

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "page_size": 20
}
```

该响应不得包含 `data`、`status`、`message`、`total_pages` 或 `tasks` 等字段。详细约束以 `docs/spec/addp任务体系规范.md` 为准。

---

## 三、RESTful 设计规范

### 3.1 资源命名规则

1. **使用复数名词**：`/api/v1/system/users`、`/api/v1/system/engines`
2. **小写字母 + 下划线**：`/api/v1/system/audit_logs`（与数据库字段命名一致）
3. **避免动词**：❌ `/api/v1/system/getUsers`、✅ `/api/v1/system/users`
4. **使用名词表示资源**：❌ `/api/v1/system/create_user`、✅ `POST /api/v1/system/users`

### 3.2 HTTP 方法语义

| 方法   | 语义         | 示例                         | 幂等性 | 响应码      |
|--------|--------------|------------------------------|--------|-------------|
| GET    | 查询资源     | GET /api/v1/system/users            | 是     | 200         |
| POST   | 创建资源     | POST /api/v1/system/users           | 否     | 201         |
| PUT    | 完整更新     | PUT /api/v1/system/users/:id        | 是     | 200         |
| PATCH  | 部分更新     | PATCH /api/v1/system/users/:id      | 是     | 200         |
| DELETE | 删除资源     | DELETE /api/v1/system/users/:id     | 是     | 200         |

### 3.3 标准资源操作

```
GET    /api/v1/system/users           # 列表查询（支持过滤、排序、分页）
POST   /api/v1/system/users           # 创建用户
GET    /api/v1/system/users/:id       # 查询单个用户
PUT    /api/v1/system/users/:id       # 更新用户（完整替换）
PATCH  /api/v1/system/users/:id       # 更新用户（部分字段）
DELETE /api/v1/system/users/:id       # 删除用户
```

### 3.4 子资源设计

```
GET    /api/v1/system/tenants/:id/users        # 获取租户下的用户列表
POST   /api/v1/system/tenants/:id/users        # 为租户创建用户
DELETE /api/v1/system/tenants/:id/users/:uid   # 删除租户下的某个用户
```

**规则：**
- 子资源深度建议不超过 2 层
- 超过 2 层时考虑使用查询参数：`GET /api/v1/system/users?tenant_id=1`

### 3.5 特殊操作的 RESTful 化

**原则：** 优先考虑可读性和直观性，适度资源化，不追求教条式的RESTful。

**核心观点：**
- ✅ **清晰直观优先** - API设计首要目标是让开发者容易理解和使用
- ✅ **合理使用动词** - 对于"触发动作"类操作，使用动词往往更清晰
- ⚠️ **避免过度资源化** - 不要为了资源化而降低可读性

**推荐做法：**

| 操作类型           | 推荐方案                           | 说明                                      |
|--------------------|------------------------------------|-------------------------------------------|
| 测试已有引擎       | POST /engines/:id/test             | 动作清晰，保留动词合理                    |
| 创建前测试连接     | POST /engines/test-connection      | 临时操作，test-connection 比 test 更明确  |
| 触发扫描任务       | POST /engines/:id/scan             | 触发动作，保留动词直观                    |
| 修改密码           | PUT /users/:id/change-password     | change-password 比 password 语义更明确    |
| 导出日志           | GET /logs/export?format=csv        | 独立操作，使用独立路径更直观              |
| 批量删除           | POST /users/batch_delete           | 批量操作，使用动词清晰                    |
| 用户登录           | POST /auth/login                   | 认证操作，保留动词（业界标准）            |
| Token 刷新         | POST /auth/refresh                 | 认证操作，保留动词（业界标准）            |

**对比说明：**

```bash
# ❌ 过度资源化 - 降低可读性
POST /engines/:id/connection_tests         # 比 /test 更冗长，没有带来额外价值
PUT /users/:id/password                    # 容易与 GET 混淆，不如 change-password 清晰
GET /logs?format=csv&export=true           # 查询参数冗长，不如独立路径直观

# ✅ 推荐做法 - 清晰直观
POST /engines/:id/test                      # 一目了然：测试引擎连接
POST /engines/test-connection               # 明确：创建前的临时测试
PUT /users/:id/change-password              # 清晰：修改密码操作
GET /logs/export                            # 直观：导出日志
```

**例外场景（可保留动词）：**
1. **认证相关** - `/auth/login`、`/auth/logout`、`/auth/refresh`（业界标准）
2. **触发动作** - `/test`、`/scan`、`/execute`、`/deploy`（不创建持久资源的操作）
3. **批量操作** - `/batch_delete`、`/batch_update`（涉及多个资源的操作）
4. **语义明确性** - 当动词能显著提高可读性时（如 `change-password` vs `password`，`export` vs 查询参数）

**GitHub API 案例参考：**
- `POST /repos/:owner/:repo/merges` - 合并操作
- `POST /repos/:owner/:repo/dispatches` - 触发工作流
- `PUT /repos/:owner/:repo/topics` - 更新主题

**小结：**
RESTful是指导原则，不是教条。优秀的API设计应该在RESTful原则和实用性之间取得平衡，让开发者能够快速理解和正确使用。**当资源化会降低可读性时，保留动词是更好的选择。**

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
| 用户名密码错误           | 401    | `{"error": "用户名或密码错误"}` |
| Token 无效/过期          | 401    | `{"error": "未登录或 token 已过期"}` |
| JSON 格式错误            | 400    | `{"error": "请求格式错误"}` |
| 缺少必填参数             | 400    | `{"error": "缺少必填参数：用户名"}` |
| 无权限访问资源           | 403    | `{"error": "无权访问该资源"}` |
| 资源不存在               | 404    | `{"error": "引擎不存在"}` |
| 用户名已存在             | 409    | `{"error": "用户名已存在"}` |
| 数据库连接失败           | 500    | `{"error": "数据库连接失败"}` |

**状态码选择要点：**
- **401** - 用于所有认证失败的场景（登录失败、token无效、token过期）
- **400** - 用于请求格式问题（JSON解析失败、参数缺失、参数类型错误）
- **403** - 用于已登录但权限不足
- **422** - 可选，用于业务逻辑验证失败；也可统一使用400

### 4.3 错误处理原则

1. **HTTP 状态码表达错误类型** - 充分利用 HTTP 语义，无需在 body 中重复
2. **error 字段清晰描述错误原因** - 可直接展示给用户
3. **生产环境可隐藏 detail 字段** - 避免泄露敏感信息
4. **统一错误处理中间件** - 避免每个 handler 重复处理

---

## 五、API 版本控制

### 5.1 版本策略

**统一使用 `/api/v1/` 路径前缀：**

```
/api/v1/system/users        # 当前使用
/api/v1/system/engines      # 当前使用
/api/v2/system/users        # 未来破坏性变更时升级
```

**版本控制的优点：**
- 直观明确，便于 client 生成和 AI agent 调用
- 支持同时运行多个版本
- 便于网关路由和灰度发布

### 5.2 版本演进规则

- ADDP 当前处于积极开发阶段，默认采用 clean break：不保留旧路径、旧字段、旧 query 或双轨兼容分支。
- 接口契约变化必须先更新规范、Swagger 和调用方，再一次性切换到新的唯一主路径。
- 若未来已经对外发布稳定 API，且确实需要长期并行两个契约，才通过 `/api/v2/` 引入新版本；该决策必须先形成明确设计文档。

### 5.3 路由前缀要求

**Go 端（Gin）**：各模块必须使用 `/api/v1/{module}`：

```go
v1 := r.Group("/api/v1/system")
```

**Python 端（FastAPI）**：各模块必须使用 `/api/v1/{module}`：

```python
app.include_router(router, prefix="/api/v1/agent")
```

**Gateway**：路由转发规则必须匹配 `/api/v1/:module/*path`：

```go
protected.Any("/api/v1/:module/*path", ...)
```

**前端**：必须调用 `/api/v1/system`、`/api/v1/manager` 等模块路径，不得新增 `/api/{module}` 旧路径。

---

## 六、过滤、排序、搜索规范

### 6.1 过滤参数

通过 Query 参数过滤：

```
GET /api/v1/system/users?username=admin&is_active=true
GET /api/v1/system/logs?start_time=2024-01-01&end_time=2024-12-31&action=create
GET /api/v1/system/engines?engine_type=postgresql&tenant_id=1
```

**规则：**
- 字段名使用 snake_case
- 布尔值使用 `true/false`
- 时间使用 ISO 8601 格式或 Unix 时间戳

### 6.2 排序参数

```
GET /api/v1/system/users?sort=created_at&order=desc
GET /api/v1/system/users?sort=created_at&order=asc
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
GET /api/v1/system/users?search=张三
GET /api/v1/system/engines?search=postgres
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
// HTTP 401 Unauthorized
{
  "error": "未登录或 token 已过期"
}
```

**权限不足（403）：**

```json
// HTTP 403 Forbidden
{
  "error": "无权访问该资源"
}
```

### 8.3 Token 刷新

```
POST /api/v1/system/refresh
Authorization: Bearer <expired_token>
```

响应：

```json
// HTTP 200 OK
{
  "access_token": "new_jwt_token",
  "token_type": "Bearer"
}
```

---

## 九、文档规范

**统一使用 Swagger/OpenAPI 自动生成 API 文档，废弃手写的 api-manifest.json。**

### 9.1 Go 端（swaggo/swag）

**安装依赖**：

```bash
go get -u github.com/swaggo/swag/cmd/swag
go get -u github.com/swaggo/gin-swagger
go get -u github.com/swaggo/files
```

**main.go 注册 Swagger 路由**：

```go
import (
    _ "your-module/docs"  // swag 生成的 docs 包
    ginSwagger "github.com/swaggo/gin-swagger"
    swaggerFiles "github.com/swaggo/files"
)

r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
```

**handler 注释示例**：

```go
// @Summary 用户登录
// @Description 用户登录获取 JWT token
// @Tags 认证
// @Accept json
// @Produce json
// @Param request body LoginRequest true "登录信息"
// @Success 200 {object} LoginResponse "登录成功"
// @Failure 401 {object} ErrorResponse "用户名或密码错误"
// @Router /api/v1/system/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
    // ...
}
```

**生成文档**：

```bash
swag init -g cmd/server/main.go -o docs
```

### 9.2 Python 端（FastAPI 原生）

FastAPI 天然生成 OpenAPI，无需额外工具。在路由函数添加描述：

```python
@router.post("/chat", summary="发送消息", tags=["对话"])
async def chat(request: ChatRequest):
    """
    向 AI agent 发送消息，返回流式响应。
    session_id 可通过 GET /api/v1/agent/sessions 获取。
    """
    ...
```

访问入口：`http://localhost:8090/docs`（Swagger UI）或 `/openapi.json`

### 9.3 Swagger UI 访问入口

各模块 Swagger UI 地址：

```
http://localhost:8180/swagger/index.html   # System 模块
http://localhost:8081/swagger/index.html   # Manager 模块
http://localhost:8082/swagger/index.html   # Meta 模块
http://localhost:8083/swagger/index.html   # Develop 模块
http://localhost:8090/docs                 # Agent 模块（FastAPI）
http://localhost:8091/docs                 # Copilot 模块（FastAPI）
```

### 9.4 接口文档要求

每个接口应包含：
1. **Summary** - 一句话功能说明
2. **Description** - 详细说明，包括参数来源提示（便于 AI agent 理解）
3. **Tags** - 分组标签，对应功能模块
4. **请求参数** - 路径参数、Query 参数、Body 参数的类型和说明
5. **响应示例** - 成功和失败的响应结构
6. **认证要求** - 是否需要 JWT（`@Security BearerAuth`）

---

## 十、实施指南

### 10.1 新接口开发

所有新开发的接口**必须**遵循本规范：

1. 使用 `/api/v1/` 路径前缀（无论产品版本号为何）
2. 采用灵活响应策略（简单场景直接返回，复杂场景适度包装）
3. 遵循 RESTful 设计（可读性优先，合理使用动词）
4. 使用 snake_case 命名
5. 添加 Swagger 注释

**参考实现**：System 模块的 API 设计可作为其他模块的参考标准

### 10.2 后端实现分层与共享能力

各服务的后端实现应遵循 `Handler -> Service -> Repository -> Database` 的分层思路：

1. **Handler** 负责 HTTP 请求解析、参数绑定、认证上下文读取、响应写回和 Swagger 注解维护。
2. **Service** 负责业务规则、事务边界、跨 Repository 编排和领域错误返回。
3. **Repository** 负责数据访问封装，避免在 Handler 或 Service 中散落 SQL/GORM 细节。
4. **Database** 表示具体数据库、外部存储或引擎调用边界。

后端通用能力应优先沉淀到 `common/`，不要在各模块重复实现。适合放入 `common/` 的能力包括统一响应、错误映射、认证上下文、分页模型、通用客户端、存储路径与指纹等跨模块复用逻辑。

### 10.3 现有接口迁移

**迁移策略（已完成）**：

所有模块的 API 路径已于 2026-03 统一迁移至 `/api/v1/` 前缀，迁移覆盖：
- Go 各模块 `router.go`：`router.Group("/api/{module}")` → `router.Group("/api/v1/{module}")`
- Python 模块（Agent、Copilot）：`prefix="/api/{module}"` → `prefix="/api/v1/{module}"`
- Gateway 路由：`router.Group("/api")` → `router.Group("/api/v1")`
- 各模块前端 API 调用路径
- `common/client/system.go` 内部调用路径

**兼容性说明**：
- ADDP 当前处于积极开发阶段，**不做向后兼容**，旧路径 `/api/` 已废弃
- 产品版本号（v0.x）与 API 版本前缀（v1）无关，v1 表示接口契约稳定，不随产品迭代频繁变更

**优先级：**
- 高频接口优先统一格式（如用户、引擎、日志）
- 内部接口可延后

### 10.4 共享响应处理

在 `common/api` 模块中提供统一的响应方法（参考 System 模块实现）：

```go
// RespondSuccess 成功响应 - 直接返回数据
func RespondSuccess(c *gin.Context, data interface{}) {
    c.JSON(http.StatusOK, data)
}

// RespondCreated 创建成功响应
func RespondCreated(c *gin.Context, data interface{}) {
    c.JSON(http.StatusCreated, data)
}

// RespondError 错误响应 - 简洁格式
func RespondError(c *gin.Context, code int, message string) {
    c.JSON(code, gin.H{
        "error": message,
    })
}

// RespondOrError 智能响应 - 自动映射错误到 HTTP 状态码
func RespondOrError(c *gin.Context, data interface{}, err error) {
    if err != nil {
        code := MapErrorToHTTPStatus(err)
        RespondError(c, code, err.Error())
        return
    }
    RespondSuccess(c, data)
}

// RespondPaginated 分页响应
func RespondPaginated(c *gin.Context, data interface{}, total int64, page, pageSize int) {
    totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
    c.JSON(http.StatusOK, gin.H{
        "data":        data,
        "total":       total,
        "page":        page,
        "page_size":   pageSize,
        "total_pages": totalPages,
    })
}
```

**错误映射函数**：

```go
func MapErrorToHTTPStatus(err error) int {
    switch {
    case errors.Is(err, gorm.ErrRecordNotFound):
        return http.StatusNotFound
    case errors.Is(err, gorm.ErrDuplicatedKey):
        return http.StatusConflict
    case errors.Is(err, ErrBadRequest):
        return http.StatusBadRequest
    case errors.Is(err, ErrUnauthorized):
        return http.StatusUnauthorized
    case errors.Is(err, ErrForbidden):
        return http.StatusForbidden
    default:
        return http.StatusInternalServerError
    }
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

**A:** 推荐返回 **200 + 简洁消息**，便于前端提示用户。

```json
// HTTP 200 OK
{
  "message": "删除成功"
}
```

204 不返回 body，虽然符合 HTTP 语义，但前端处理不便，推荐使用 200 + message。

### Q3: 用户名密码错误返回什么状态码？

**A:** 返回 **401 Unauthorized**（推荐）。

**状态码语义（RFC 9110标准）：**
- **401 Unauthorized** - 认证失败（用户名密码错误、token无效/过期）
- **400 Bad Request** - 请求格式错误（JSON解析失败、缺少必填参数、参数类型错误）
- **403 Forbidden** - 已认证但权限不足

**示例：**
```http
# 用户名密码错误
POST /api/v1/system/login
Response: 401 {"error": "用户名或密码错误"}

# Token过期
GET /api/v1/system/users
Response: 401 {"error": "token已过期"}

# JSON格式错误
POST /api/v1/system/users
Response: 400 {"error": "JSON格式错误"}

# 已登录但无权限
GET /api/v1/system/admin/users
Response: 403 {"error": "无权访问该资源"}
```

**注意：** 虽然国内一些项目将登录失败归为400（认为是"参数错误"），但这不符合HTTP标准语义。建议遵循RFC标准使用401。

### Q4: 分页信息放在哪里？

**A:** 与 `data` 平级，放在外层：

```json
// HTTP 200 OK
{
  "data": [...],
  "total": 100,
  "page": 1,
  "page_size": 20,
  "total_pages": 5
}
```

**不推荐**嵌套在 `pagination` 对象中，因为会增加一层结构。

### Q5: 是否需要细分业务错误码（如 10001、20001）？

**A:** 当前不需要。使用 HTTP 状态码 + 清晰的 `error` 消息即可。

**推荐做法**：
```json
{"error": "用户名已存在"}  // HTTP 409
```

**不推荐**：
```json
{"code": 10001, "message": "用户名已存在"}  // 增加复杂度
```

后续如有特殊需求（如多语言、详细错误追踪），可扩展。

### Q6: 如何处理批量操作？

**A:** 使用 POST 请求，body 传递批量数据：
```
POST /api/v1/system/users/batch_delete
Body: {"user_ids": [1, 2, 3]}
```

### Q7: 内部 API（/api/v1/internal）是否也需要遵循规范？

**A:** 是的。内部 API 也应遵循统一响应格式和命名规范，便于维护和调试。

---

## 十二、amis 集成规范

[百度 amis](https://aisuda.bce.baidu.com/amis/zh-CN/docs/start/getting-started) 是 ADDP 前端组件化的核心框架。amis 对 API 响应格式有特定要求，需要在前端适配层处理，**后端不做任何改动**。

### 12.1 amis 期望的响应格式

```json
{
  "status": 0,
  "msg": "",
  "data": { ... }
}
```

- `status: 0` 表示成功，非 0 表示失败
- `msg` 为错误消息
- `data` 为实际数据

### 12.2 统一适配器

在 `common-frontend/basic/src/utils/amis-adaptor.js` 中提供统一适配器，**所有使用 amis 的模块必须使用此适配器**，不得各自实现：

```javascript
/**
 * ADDP API 响应 → amis 格式适配器
 * ADDP 使用灵活响应（直接返回数据 + HTTP 状态码），amis 需要 {status, msg, data} 格式
 */
export function toAmisResponse(data, httpStatus = 200) {
  if (httpStatus >= 400) {
    return {
      status: 1,
      msg: data?.error || '请求失败',
    }
  }
  return {
    status: 0,
    msg: '',
    data: data,
  }
}

/**
 * amis CRUD 列表适配器（处理分页响应）
 * ADDP 分页格式: { data: [], total, page, page_size, total_pages }
 * amis 期望格式: { items: [], total }
 */
export function toAmisListResponse(data, httpStatus = 200) {
  if (httpStatus >= 400) {
    return { status: 1, msg: data?.error || '请求失败' }
  }
  // 处理分页响应
  if (data?.data && data?.total !== undefined) {
    return {
      status: 0,
      msg: '',
      data: {
        items: data.data,
        total: data.total,
      },
    }
  }
  // 处理数组响应
  if (Array.isArray(data)) {
    return {
      status: 0,
      msg: '',
      data: { items: data, total: data.length },
    }
  }
  return { status: 0, msg: '', data }
}
```

### 12.3 分页参数映射

amis 默认发送 `page` 和 `perPage`，ADDP 使用 `page` 和 `page_size`，在 amis 组件配置中映射：

```json
{
  "type": "crud",
  "api": {
    "url": "/api/v1/system/users",
    "data": {
      "page": "${page}",
      "page_size": "${perPage}"
    },
    "adaptor": "return window.addpAmisAdaptor.toAmisListResponse(payload.data, payload.status)"
  }
}
```

### 12.4 axios 拦截器集成

在各模块前端的 axios 实例中统一处理，避免每个 amis 组件单独配置 adaptor：

```javascript
import { toAmisResponse } from '@addp/basic/utils/amis-adaptor'

// amis 专用 axios 实例
export const amisAxios = axios.create({ baseURL: '/api/v1' })

amisAxios.interceptors.response.use(
  (response) => toAmisResponse(response.data, response.status),
  (error) => toAmisResponse(error.response?.data, error.response?.status || 500)
)
```

---

## 十三、AI Agent 友好性规范

AI agent 调用 API 时需要理解接口语义、判断错误是否可重试、知道参数从哪里获取。以下规范让 API 对 agent 更友好。

### 13.1 Swagger 注释中的 x-ai-hint

在 Go handler 注释中添加 `x-ai-hint` 扩展字段，描述 API 用途和参数来源：

```go
// @Summary 执行开发任务
// @Description 执行指定的工作流或 SQL 开发任务
// @Tags 开发任务
// @x-ai-hint 执行用户创建的工作流或 SQL。task_id 可通过 GET /api/v1/develop/task-definitions 获取；engine_id 可通过 GET /api/v1/system/engines 获取
// @Router /api/v1/develop/task-definitions/{id}/execute [post]
func (h *ItemHandler) Execute(c *gin.Context) { ... }
```

在 Python FastAPI 中通过 `openapi_extra` 传入：

```python
@router.post(
    "/chat",
    summary="发送消息",
    openapi_extra={
        "x-ai-hint": "向 AI agent 发送消息。session_id 可通过 GET /api/v1/agent/sessions 获取，不传则自动创建新会话"
    }
)
async def chat(request: ChatRequest): ...
```

### 13.2 错误响应增加 error_type

对于 agent 需要判断是否重试的场景，错误响应可附加 `error_type` 字段（可选）：

```json
// 瞬时错误，可重试
{
  "error": "数据库连接超时",
  "error_type": "transient",
  "retry_after": 5
}

// 永久错误，不应重试
{
  "error": "引擎不存在",
  "error_type": "permanent"
}

// 用户输入错误，需要修正后重试
{
  "error": "SQL 语法错误：第 3 行",
  "error_type": "user_error"
}
```

| error_type | 含义 | agent 行为 |
|------------|------|-----------|
| `transient` | 瞬时错误（超时、连接失败） | 等待 retry_after 秒后重试 |
| `permanent` | 永久错误（资源不存在） | 不重试，报告给用户 |
| `user_error` | 用户输入错误 | 修正输入后重试 |

**注意**：`error_type` 是可选字段，不强制要求所有接口都实现。优先在 agent 频繁调用的接口上添加。

### 13.3 agent 调用其他模块的认证

agent 调用其他模块（develop、meta、manager 等）时，统一使用内部 API Key 认证：

```python
headers = {
    "X-Internal-API-Key": settings.INTERNAL_API_KEY
}
```

不得使用用户 JWT token 代理调用，避免权限混淆。

---

## 十四、Python Client 规范

`agent/backend/tools/` 目录下的各模块 client 必须继承统一基类，保持一致的接口风格。

### 14.1 基类定义

基类位于 `agent/backend/tools/base_client.py`：

```python
import httpx
from typing import Any, Optional

class AddpBaseClient:
    """ADDP 模块 HTTP Client 基类"""

    def __init__(
        self,
        base_url: str,
        internal_api_key: Optional[str] = None,
        user_token: Optional[str] = None,
        timeout: float = 30.0,
    ):
        headers = {"Content-Type": "application/json"}
        if internal_api_key:
            headers["X-Internal-API-Key"] = internal_api_key
        elif user_token:
            headers["Authorization"] = f"Bearer {user_token}"

        self._client = httpx.AsyncClient(
            base_url=base_url,
            headers=headers,
            timeout=timeout,
        )

    async def get(self, path: str, **kwargs) -> Any:
        resp = await self._client.get(path, **kwargs)
        resp.raise_for_status()
        return resp.json()

    async def post(self, path: str, **kwargs) -> Any:
        resp = await self._client.post(path, **kwargs)
        resp.raise_for_status()
        return resp.json()

    async def put(self, path: str, **kwargs) -> Any:
        resp = await self._client.put(path, **kwargs)
        resp.raise_for_status()
        return resp.json()

    async def delete(self, path: str, **kwargs) -> Any:
        resp = await self._client.delete(path, **kwargs)
        resp.raise_for_status()
        return resp.json()

    async def close(self):
        await self._client.aclose()

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await self.close()
```

### 14.2 子类示例

```python
from .base_client import AddpBaseClient

class DevelopClient(AddpBaseClient):
    """Develop 模块 Client"""

    def __init__(self, base_url: str, internal_api_key: str):
        super().__init__(base_url, internal_api_key=internal_api_key)

    async def list_tasks(self, task_type: str = None) -> list:
        params = {}
        if task_type:
            params["type"] = task_type
        return await self.get("/api/v1/develop/task-definitions", params=params)

    async def execute_task(self, task_id: int, params: dict = None) -> dict:
        return await self.post(f"/api/v1/develop/task-definitions/{task_id}/execute", json=params or {})
```

### 14.3 规范要求

- 所有 client 必须继承 `AddpBaseClient`
- 方法名使用 snake_case，与 API 路径语义对应
- 不在 client 中处理业务逻辑，只做 HTTP 调用和基本的参数组装
- 使用 `httpx.HTTPStatusError` 处理 HTTP 错误，不吞掉异常

---

## 十五、参考资料

- [Google API Design Guide](https://cloud.google.com/apis/design)
- [Microsoft REST API Guidelines](https://github.com/microsoft/api-guidelines)
- [RESTful API Best Practices](https://restfulapi.net/)
- [HTTP 状态码 RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html)

---

## 附录：快速对照表

### A. HTTP 方法对照

| 操作     | 方法   | 路径                   | 响应码 |
|----------|--------|------------------------|--------|
| 列表查询 | GET    | /api/v1/system/users          | 200    |
| 创建     | POST   | /api/v1/system/users          | 201    |
| 查询详情 | GET    | /api/v1/system/users/:id      | 200    |
| 更新     | PUT    | /api/v1/system/users/:id      | 200    |
| 删除     | DELETE | /api/v1/system/users/:id      | 200    |

### B. 响应格式对照

| 场景           | HTTP状态 | 响应格式                                     |
|----------------|----------|----------------------------------------------|
| 查询单个资源   | 200      | `{id, name, ...}` 直接返回对象                |
| 查询列表       | 200      | `[{...}, {...}]` 直接返回数组                 |
| 查询分页列表   | 200      | `{data: [...], total, page, page_size, total_pages}` |
| 创建成功       | 201      | `{id, name, ...}` 返回新创建的对象            |
| 更新成功       | 200      | `{id, name, ...}` 返回更新后的对象            |
| 删除成功       | 200      | `{message: "删除成功"}` 或空响应              |
| 参数错误       | 400      | `{error: "xxx错误"}`                          |
| 认证失败       | 401      | `{error: "认证失败"}`                         |
| 无权限         | 403      | `{error: "无权限"}`                           |
| 资源不存在     | 404      | `{error: "不存在"}`                           |
| 资源冲突       | 409      | `{error: "已存在"}`                           |
| 服务器错误     | 500      | `{error: "服务器错误"}`                       |

### C. 命名规范对照

| 类型         | 规范          | 示例                     |
|--------------|---------------|--------------------------|
| URL 路径     | snake_case    | /api/v1/system/audit_logs |
| JSON 字段    | snake_case    | user_id, created_at      |
| 时间格式     | ISO 8601      | 2024-01-01T12:00:00Z     |
| 布尔值       | true/false    | is_active: true          |
| 枚举值       | 字符串        | engine_type: "postgresql"|

---

**本规范最后更新：2026-01-06（v1.2 修订版）**
**适用版本：ADDP v0.0.20+**

**主要修订内容（v1.2）：**
1. **调整为灵活响应策略** - 简单 CRUD 直接返回数据，复杂场景适度包装，避免冗余
2. **错误格式简化** - 使用简洁的 `{error: "..."}` 格式，充分利用 HTTP 状态码
3. **统一版本前缀** - 所有模块 API 使用 `/api/v1/{module}`，旧 `/api/{module}` 路径不再保留
4. **承认动词路径的合理性** - 更新推荐做法，认可 `change-password`、`export`、`test-connection` 等实用路径
5. **删除不合理的迁移建议** - System 模块当前实现已是最佳实践，无需调整
6. **更新实施指南和常见问题** - 反映实际的灵活响应策略和实用主义原则

**设计理念：**
- 规范应反映实际的最佳实践，而非理想化的标准
- 实用性优于教条主义，可读性优于完美一致性
- 当前阶段（v0.x）优先保持概念和接口收敛，发现旧路径或旧契约时直接清理，不做兼容分支

**参考实现：**
System 模块已完整实现本规范，可作为其他模块的参考标准。
