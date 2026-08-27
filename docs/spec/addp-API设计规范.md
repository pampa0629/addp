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

### 2.2.1 资源版本与并发更新

#### 适用范围与版本主体

所有可变的持久化主资源必须返回正整数 `version` 字段，数据库统一使用非空 `BIGINT`，创建时从 `1` 开始。这里的主资源是具有独立身份、可单独读取且能够作为一次编辑提交边界的资源；只读投影、执行日志、不可变历史记录和纯查询结果不需要并发版本。

聚合子资源不建立独立版本。子资源的新增、更新、删除以及关联集合变更都使用聚合根的 `version`，并在同一数据库事务中校验和递增聚合根版本。例如 EntityAttribute 使用 Entity 版本，LogicalField、TableRelation 和 FactMetricMapping 使用 LogicalTable 版本。具有独立生命周期、可脱离任一聚合单独编辑的关系事实属于主资源，应维护自己的版本。

创建独立主资源不携带版本；创建聚合子资源必须携带父资源版本。对已有资源执行的更新、删除、审批、重新打开、启停、上传替换和关联变更等写操作，都必须携带版本。聚合内部的批量或全量替换必须携带聚合根版本；跨多个独立聚合的集合整体替换必须定义稳定的集合修订版本，不能绕过并发控制。

显式列举成员的原子批量命令不属于集合整体替换，可以为每个成员分别携带 `id + version`，无需为此制造全局集合聚合根或 Tenant 级 `revision`。这类命令必须固定成员数量上限、拒绝重复 ID、按稳定顺序锁定全部成员，在同一事务校验并递增每个成员版本；任一成员不存在、越权、版本冲突或业务校验失败时整批回滚。不得接受“当前筛选结果”“全部匹配项”或执行时动态展开的隐式成员集合。

#### 字段与 API 契约

`version` 仅表示资源并发版本，不得同时承载业务对象自身的版次、发布版本或修订号。业务版次必须使用带领域含义的字段名，例如标准文档使用 `document_version`。

跨多个独立聚合且没有单一聚合根的集合整体替换统一使用正整数 `revision`，数据库同样使用非空 `BIGINT` 并从 `1` 开始。集合读取或导出必须返回当前 `revision`，替换请求必须在 body 中携带该值，成功后返回递增后的新值；其原子校验、冲突响应、前端保留和测试要求与资源 `version` 相同。不得用集合成员数量、时间戳或任一成员的 `version` 代替集合修订版本。显式成员批量命令则以请求中每个独立聚合自己的 `version` 为并发边界，不得混用集合 `revision`。

请求体中的 `version` 必须是大于 `0` 的必填整数，不能从 query、Header 或服务端当前值兜底，也不能同时接受多种版本传递方式。成功写入后，直接资源操作返回更新后的完整资源；聚合子资源或关联操作至少返回递增后的聚合根版本：

```json
{ "version": 4 }
```

前端必须立即用成功响应中的新版本替换本地版本。一次用户操作包含多个顺序写请求时，后续请求必须使用前一个响应返回的新版本；不得并行发送共享同一旧版本的聚合写请求。

#### 后端原子性

版本校验、业务写入和版本递增必须在同一条条件更新或同一数据库事务中完成。主资源更新的条件至少包含 `id + tenant_id + version`，成功时执行 `version = version + 1`；不得先读取版本再执行无条件更新，也不得仅在 Service 内比较后写入。

```sql
UPDATE resources
SET name = $1, version = version + 1
WHERE id = $2 AND tenant_id = $3 AND version = $4;
```

受影响行数为 `0` 时，服务端必须在租户边界内区分资源不存在与版本冲突：资源不存在返回 `404`，资源存在但版本不匹配返回 `409 Conflict`。版本冲突不得产生任何主资源、子资源、关联、文件元数据或清理队列副作用。

#### 冲突响应与前端体验

版本不匹配时不得覆盖当前数据，返回 HTTP `409 Conflict`，响应使用统一错误格式和稳定错误码：

```json
// PUT /api/v1/{module}/resources/1
{ "name": "更新后的名称", "version": 3 }
```

```json
// HTTP 409 Conflict
{
  "error": "资源已被其他用户修改，请刷新后重试",
  "error_code": "resource_version_conflict"
}
```

客户端收到版本冲突后必须：

- 保持当前页面、弹窗或抽屉打开，保留所有本地未保存内容和脏状态。
- 明确提示资源已变化，提供刷新或重新加载入口；刷新前不得用服务端数据覆盖本地表单。
- 不得自动重试旧版本请求，不得静默改用服务端最新版本再次提交，也不得把版本冲突显示为普通网络错误。
- 用户主动刷新后，以最新服务端状态建立新的编辑基线；自动合并只有在资源有明确、经过测试的领域合并规则时才允许。

#### Swagger 与测试要求

所有带版本的写接口必须在 Swagger 中使用具体请求 DTO 声明必填整数 `version`，并声明 `409` 响应；文件上传等 multipart 接口必须显式声明必填的 `version` formData。不得使用宽泛 `map[string]interface{}` 隐藏版本契约。

每类版本主体至少覆盖以下测试：

- 当前版本写入成功并返回递增后的版本。
- 旧版本写入返回 `409`，且数据库内容和版本不变。
- 聚合子资源冲突时，父版本和所有子资源、关联均无副作用。
- 跨租户 ID 不可用于探测资源是否存在或区分版本。
- 前端冲突后保留本地输入和未保存状态；连续聚合写请求使用服务端返回的新版本。
- 数据库迁移为全部存量行生成非空版本；若原有 `version` 承载业务版次，先按领域语义原位改名，再新增并发版本，不保留兼容字段。

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

供 SDK、Runtime 或多跳代理消费且调用方必须区分会话失效、权限拒绝、资源不存在、不支持和瞬时上游故障的接口，应在 `error` 之外返回稳定的 `error_code`。`error_code` 使用小写下划线命名，不国际化，不包含动态文本，也不重复 HTTP 状态码；SDK 只能按 HTTP 状态码与 `error_code` 分支，不能解析本地化 `error` 文案。

```json
// HTTP 504 Gateway Timeout
{
  "error": "读取引擎目录超时",
  "error_code": "engine_catalog_timeout",
  "error_type": "transient"
}
```

`error_type` 和 `retry_after` 继续遵守第 13.2 节；同一个 `error_code` 的重试类别必须稳定。简单 CRUD 接口不需要为包装一致性强行增加 `error_code`。

### 2.4 分页响应格式

本节的 `page/page_size` 用于平台管理列表。已发布查询服务面向业务数据的查询不能默认执行精确计数，也不能以 `OFFSET` 作为深分页主路径，统一使用稳定排序键上的 cursor/keyset 分页：

```json
{
  "data": [],
  "page": {
    "limit": 100,
    "has_more": true,
    "next_cursor": "opaque-cursor"
  },
  "service_version": "revision-id"
}
```

查询游标必须绑定服务发布版本、查询指纹、有效排序和最后一行排序值，并由服务端完整性保护。消费者不得构造或修改游标。精确计数属于显式、高成本能力，不进入普通查询默认路径。

OGC API Features 继续遵循协议参数和响应结构，但 Items 的 `next` link 应携带同一查询游标；`numberReturned` 必须准确，`numberMatched` 只有在已取得低成本精确值时才返回，不得为每个 Items 请求强制执行 `COUNT(*)`。

**请求参数：**

```
GET /api/v1/system/platform/users?page=1&page_size=20
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

1. **使用复数名词**：`/api/v1/system/platform/users`、`/api/v1/system/engines`
2. **小写字母 + 下划线**：`/api/v1/system/platform/identity_changes`
3. **避免动词**：❌ `/api/v1/system/getUsers`、✅ `/api/v1/system/platform/users`
4. **使用名词表示资源**：❌ `/api/v1/system/create_user`、✅ `POST /api/v1/system/platform/users`

### 3.2 HTTP 方法语义

| 方法   | 语义         | 示例                         | 幂等性 | 响应码      |
|--------|--------------|------------------------------|--------|-------------|
| GET    | 查询资源     | GET /api/v1/{module}/resources            | 是     | 200         |
| POST   | 创建资源     | POST /api/v1/{module}/resources           | 否     | 201         |
| PUT    | 完整更新     | PUT /api/v1/{module}/resources/:id        | 是     | 200         |
| PATCH  | 部分更新     | PATCH /api/v1/{module}/resources/:id      | 是     | 200         |
| DELETE | 删除资源     | DELETE /api/v1/{module}/resources/:id     | 是     | 200         |

### 3.3 标准资源操作

```
GET    /api/v1/{module}/resources           # 列表查询（端点明确声明时支持过滤、排序、分页）
POST   /api/v1/{module}/resources           # 创建资源
GET    /api/v1/{module}/resources/:id       # 查询单个资源
PUT    /api/v1/{module}/resources/:id       # 更新资源（完整替换）
PATCH  /api/v1/{module}/resources/:id       # 更新资源（部分字段）
DELETE /api/v1/{module}/resources/:id       # 删除资源
```

### 3.4 子资源设计

```
GET    /api/v1/{module}/{parents}/:parent_id/{children}           # 获取父资源下的子资源
POST   /api/v1/{module}/{parents}/:parent_id/{children}           # 创建父子关系或子资源
DELETE /api/v1/{module}/{parents}/:parent_id/{children}/:child_id # 删除父子关系或子资源
```

**规则：**
- 子资源深度建议不超过 2 层
- 超过 2 层时考虑使用查询参数：`GET /api/v1/{module}/{children}?parent_id=1`

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

### 4.4 进程存活与模块就绪端点

所有 ADDP HTTP Backend 只使用两个公开、无认证健康端点：

| 端点 | 成功 | 失败 | 语义 |
| --- | --- | --- | --- |
| `GET /health/live` | `200` | 进程无法响应 | 只证明 HTTP 进程 Alive，不检查任何远程依赖 |
| `GET /health/ready` | `200` | `503` | 证明当前实例可以接受平台工作；业务模块必须已在 System 成功注册 |

`/health/live` 响应使用统一构建身份字段：

```json
{
  "status": "live",
  "module": "meta",
  "build_id": "...",
  "git_commit": "...",
  "source_fingerprint": "...",
  "built_at": "...",
  "started_at": "..."
}
```

`/health/ready` 在 Ready 和 Not Ready 时使用同一响应结构：

```json
{
  "status": "not_ready",
  "module": "meta",
  "role": "backend",
  "instance_id": "process-uuid",
  "registration_state": "recovering",
  "checks": [
    {"name": "local_dependencies", "status": "ready"},
    {
      "name": "system_registration",
      "status": "not_ready",
      "error_code": "system_registration_unavailable"
    }
  ],
  "build_id": "...",
  "git_commit": "...",
  "source_fingerprint": "...",
  "built_at": "...",
  "started_at": "..."
}
```

约束如下：

- `status` 只能为 `live`、`ready`、`not_ready`；模块注册生命周期只能为 `starting`、`registered`、`recovering`、`failed`、`stopped`。
- 业务 Backend 的 `role`、`instance_id`、`registration_state` 必填。System 不依赖自注册，Gateway 不是业务模块运行实例；两者的就绪响应省略不适用的三个字段。System 使用 `local_dependencies`、`iam_bootstrap` 检查项，Gateway 使用 `system_registry_snapshot` 检查项表达各自唯一 Ready 条件。
- `checks[].name`、`status`、`error_code` 是机器可读稳定值，不国际化；健康端点不返回用户展示文案、凭据、DSN、下游响应正文或堆栈。
- 业务 Backend Ready 必须同时满足自身必需 Infra 就绪且 `registration_state=registered`；任一心跳失败被观测后立即返回 `503`，重注册成功后恢复 `200`。
- System 的 Ready 不依赖自注册；Gateway Ready 要求至少成功应用一次 System 完整模块路由快照。
- 健康端点不属于业务 API，不使用通用 `{error, error_code}` 错误体；HTTP `503` 与上述固定就绪结构已构成唯一契约。
- 旧 `GET /health` 必须与所有调用方一次性切换后删除，不保留别名、重定向或兼容响应。

---

## 五、API 版本控制

### 5.1 版本策略

**统一使用 `/api/v1/` 路径前缀：**

```
/api/v1/system/platform/users        # 当前使用
/api/v1/system/engines      # 当前使用
/api/v2/system/platform/users        # 未来破坏性变更时升级
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
GET /api/v1/system/platform/users?search=admin&status=active
GET /api/v1/system/platform/audit/events?start_time=2024-01-01T00:00:00Z&end_time=2025-01-01T00:00:00Z&event_name=iam.user.created
GET /api/v1/system/engines?engine_type=postgresql
```

**规则：**
- 字段名使用 snake_case
- 布尔值使用 `true/false`
- 时间使用 ISO 8601 格式或 Unix 时间戳
- 同一资源的 ID 集合精确筛选统一使用单个逗号分隔参数 `ids=1,2,3`；不得同时接受重复 `ids`、`ids[]` 或其他兼容写法。端点必须在 Swagger 中声明数量上限，服务端去重并拒绝空值、非正整数和超限请求。

### 6.2 排序参数

```
GET /api/v1/{module}/resources?sort=created_at&order=desc
GET /api/v1/{module}/resources?sort=name&order=asc
```

| 参数  | 值            | 说明           |
|-------|---------------|----------------|
| sort  | 字段名        | 排序字段       |
| order | asc/desc      | 升序/降序      |

**默认排序：**
- 通常按 `created_at desc`（最新的在前）
- 或按 `id desc`
- 只有端点 Swagger 明确声明 `sort` / `order` 时才允许客户端提交；服务端必须使用排序字段白名单，不得直接拼接 SQL。

### 6.3 搜索参数

```
GET /api/v1/system/platform/users?search=张三
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
  "status": "active"             // ✅ 推荐
}

// ❌ 不推荐
{
  "status": 1
}
```

---

## 八、认证授权规范

### 8.1 认证方式

**（1）用户认证（User Access Token）**

```http
Authorization: Bearer <access_token>
```

**（2）内部服务认证（Service Access Token）**

```http
Authorization: Bearer <service_access_token>
```

内部服务使用各自独立的 Confidential OAuth Client，通过 System 唯一的
`POST /api/v1/system/oauth/token` 端点执行 Client Credentials Grant。请求必须指定
`audience=addp.api`、`scope=addp.api`，并且只允许以下两种互斥 Context 选择：

- Tenant Runtime 请求提交目标 `tenant_id`，System 只允许选择该 OAuth Client 所绑定
  Service Principal 的有效 Tenant Membership；
- 平台控制面请求提交 `context_type=platform`，只允许平台所有 Service Principal 且必须
  存在专用 Platform Service Role Assignment，不允许持有或借用平台三员 Role。

System 把所选 Context 固化到短期、不可刷新的 Service Access Token 中。owner API 不接受
`X-Internal-API-Key`、`X-Tenant-ID` 或调用方提交的 Principal/Membership 作为 Tenant
授权事实。

Service Access Token 只证明当前服务主体及其固定 Context。服务代表用户执行 SQL、Workflow、Jupyter 或其他计算时，必须另外消费由当前 User AuthContext 派生、绑定唯一 execution 的 Execution Authorization。已发布查询服务可以由 `addp-service` 基于自身权威服务定义签发只读、限定 Source Engine 和 definition version/hash 的 Execution Authorization；该路径不允许其他 Service Principal、其他效果或任意 owner assertion。Service Principal 本身不得通过 Runtime Role 获得通用 Tenant 数据权限，也不得根据 Engine 创建人或注册时账号决定执行权限。

同步 BFF 是用户请求链的传输边界，不是独立业务授权主体。Portal 等 BFF 调用 owner 的消费 API
时，必须原样转发当前请求已经验证的 User Bearer，使 owner 继续以同一 Principal、Tenant Context、
Role Permission 和 Resource Policy 作出决定；BFF 不得把 User ID、Tenant ID、Role 或授权结果放入
Header、Query 或 Body 让 owner 信任。该 User Access Token 只能存在于当前同步调用栈，禁止写入
数据库、任务、缓存、日志或异步消息。

BFF 以自身身份执行不代表用户的聚合读取时，才使用自己的 Service Access Token。例如 Portal 在
Asset 已按当前 User 确认有效资产授权后，可以使用 `addp-portal` Tenant Service Access Token 读取
Service 的端点投影。此类路由必须同时校验精确 Permission 和固定 OAuth Client；机器身份获得的
端点元数据不能替代用户对真实数据、下载或服务执行的 Resource Grant。

执行入口的 Swagger 必须把稳定入口能力写入 `x-addp-required-permissions`，把服务端依据已解析执行效果动态追加校验的完整候选集合写入 `x-addp-conditional-permissions`。条件权限只用于描述动态校验契约和覆盖关系，不表示 any-of，也不替代 owner 的资源授权判断。

**（3）应用认证（API Key）**

```http
X-API-Key: <app_api_key>
```

**（4）浏览器原生资源认证（Browser Resource Access Ticket）**

图片、媒体、下载和三维加载器等无法设置 Authorization Header 的请求，可以由 Owner 明确允许的 GET/HEAD 资源路由读取 System 下发的 HttpOnly Resource Access Ticket Cookie。普通 CRUD、搜索、任务和写入 API 不接受该 Cookie；任何 API 都不得接受 `?token=` User Access Token。

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
Cookie: addp_refresh_token=<http_only_refresh_token>
```

响应：

```json
// HTTP 200 OK
{
  "access_token": "addp_at_...",
  "token_type": "Bearer",
  "expires_in": 900
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
// @Description 用户登录获取短期 opaque Access Token，并设置 HttpOnly Refresh Token Cookie
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
6. **认证要求** - 是否需要 User Access Token（`@Security BearerAuth`）

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
GET /api/v1/system/platform/users
Response: 401 {"error": "token已过期"}

# JSON格式错误
POST /api/v1/system/platform/users
Response: 400 {"error": "JSON格式错误"}

# 已登录但无权限
GET /api/v1/system/platform/users
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
POST /api/v1/{module}/resources/batch_delete
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
    "url": "/api/v1/system/platform/users",
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

### 13.2 Agent 与 SDK 错误响应增加 error_type

对于 Agent 或 SDK 需要判断是否重试的场景，错误响应可附加 `error_type` 字段（可选）：

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

**注意**：`error_type` 是可选字段，不强制要求所有接口都实现。优先在 Agent、SDK 或 Runtime 频繁调用且确实需要稳定重试判断的接口上添加。

### 13.3 agent 调用其他模块的认证

Agent 代表用户调用 owner 模块时，必须先由 System 签发绑定 owner audience、Tool Scope、AgentRun 和 ToolCall 的短期 Delegated Access Token。原始 User Access Token 不得进入 owner Client，也不得使用内部 API Key 模拟用户身份：

```python
headers = {"Authorization": f"Bearer {delegated_access_token}"}
```

内部模块以自身服务身份调用 owner API 时，使用独立 Confidential OAuth Client 通过 Client Credentials Grant 获取 Service Access Token。两种调用都只发送 Bearer，不发送 `X-Internal-API-Key` 或 `X-Tenant-ID`。

内部模块继续完成由用户发起的异步计算时，不得把原始 User Access Token 写入任务或转发给 Runtime。owner 先以当前 User AuthContext 创建绑定 execution、Tenant、Engine、效果和授权版本的 Execution Authorization；后续只有 audience 匹配的 Runtime Service Principal 可以使用自身 Service Access Token 消费。统一执行记录只保存授权 ID 和脱敏摘要，不保存任何 Token 或明文连接。

---

## 十四、Python Client 规范

`agent/backend/tools/` 目录下的各模块 client 必须继承统一基类，保持一致的接口风格。

### 14.1 基类定义

基类位于 `agent/backend/tools/base_client.py`：

```python
import httpx
from typing import Any

class AddpBaseClient:
    """ADDP 模块 HTTP Client 基类"""

    def __init__(
        self,
        base_url: str,
        access_token: str,
        timeout: float = 30.0,
    ):
        headers = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {access_token}",
        }

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

    def __init__(self, base_url: str, access_token: str):
        super().__init__(base_url, access_token=access_token)

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
- `access_token` 必须是当前调用类型对应的 Delegated Access Token 或 Service Access Token，不得传入原始 User Access Token
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
| 列表查询 | GET    | /api/v1/{module}/resources          | 200    |
| 创建     | POST   | /api/v1/{module}/resources          | 201    |
| 查询详情 | GET    | /api/v1/{module}/resources/:id      | 200    |
| 更新     | PUT    | /api/v1/{module}/resources/:id      | 200    |
| 删除     | DELETE | /api/v1/{module}/resources/:id      | 200    |

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
| URL 路径     | snake_case    | /api/v1/system/platform/identity_changes |
| JSON 字段    | snake_case    | user_id, created_at      |
| 时间格式     | ISO 8601      | 2024-01-01T12:00:00Z     |
| 布尔值       | true/false    | is_active: true          |
| 枚举值       | 字符串        | engine_type: "postgresql"|

---

**本规范最后更新：2026-08-25（v1.3 修订版）**
**适用版本：ADDP v0.0.20+**

**主要修订内容（v1.3）：**
1. **调整为灵活响应策略** - 简单 CRUD 直接返回数据，复杂场景适度包装，避免冗余
2. **错误格式简化** - 使用简洁的 `{error: "..."}` 格式，充分利用 HTTP 状态码
3. **统一版本前缀** - 所有模块 API 使用 `/api/v1/{module}`，旧 `/api/{module}` 路径不再保留
4. **承认动词路径的合理性** - 更新推荐做法，认可 `change-password`、`export`、`test-connection` 等实用路径
5. **删除不合理的迁移建议** - System 模块当前实现已是最佳实践，无需调整
6. **更新实施指南和常见问题** - 反映实际的灵活响应策略和实用主义原则
7. **统一健康契约** - 以 `/health/live` 和 `/health/ready` 分离进程存活与模块就绪，明确 System 注册是业务模块 Ready 的强依赖

**设计理念：**
- 规范应反映实际的最佳实践，而非理想化的标准
- 实用性优于教条主义，可读性优于完美一致性
- 当前阶段（v0.x）优先保持概念和接口收敛，发现旧路径或旧契约时直接清理，不做兼容分支

**参考实现：**
System 模块已完整实现本规范，可作为其他模块的参考标准。
