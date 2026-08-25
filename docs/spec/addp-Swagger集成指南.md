# ADDP Swagger 集成指南

本文档说明如何为 ADDP 各 HTTP API 模块集成和维护 Swagger/OpenAPI 文档。该规范适用于所有带 HTTP API 的模块，不限 Go 模块：Go + Gin 模块统一使用 swaggo，FastAPI 模块使用框架内置 OpenAPI。

## 强制同步要求

API 修改必须同步 Swagger。任何新增、删除、修改公开 HTTP API 的变更，都必须同时更新：

- 真实路由注册代码。
- Handler 方法和请求/响应 DTO。
- Swagger/OpenAPI 注解。
- 生成产物：`docs/swagger.json`、`docs/swagger.yaml`、`docs/docs.go`。

不允许只修改路由或 Handler 而留下旧 Swagger path，也不允许 Swagger 生成成功但文档缺失真实公开接口。

API 修改后必须执行：

```bash
bash scripts/swagger/gen-swagger.sh <module>
bash scripts/swagger/check-route-coverage.sh <module>
```

涉及多个模块时使用：

```bash
bash scripts/swagger/gen-swagger.sh all
bash scripts/swagger/check-route-coverage.sh all
```

`scripts/dev/restart.sh -<module>` 和 `scripts/dev/restart.sh -all` 会参与 Swagger 生成和覆盖校验，但不能替代开发者补注解。生成失败默认中断重启；覆盖校验在历史欠账清理阶段可降级为告警，详见脚本输出。

## 依赖版本

所有模块统一使用以下版本（已在 `docs/spec/addp技术栈规约.md` 中记录）：

```
github.com/swaggo/swag v1.16.4
github.com/swaggo/gin-swagger v1.6.0
github.com/swaggo/files v1.0.1
```

## 工具安装

```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

安装后工具路径：`~/go/bin/swag`

## 文档边界

必须纳入 Swagger/OpenAPI：

- 面向 Console 和各模块前端的公开 HTTP API。
- 模块间通过 HTTP 调用的 API。
- 对外开放的数据服务 API。

通常不纳入 Swagger/OpenAPI：

- 进程存活与模块就绪检查：`/health/live`、`/health/ready`。两者的响应契约由 [ADDP API 设计规范](addp-API设计规范.md#44-进程存活与模块就绪端点) 和公共类型统一维护，不在各模块 Swagger 重复声明。
- Swagger/OpenAPI 自身路由：`/swagger/*any`、`/docs`、`/redoc`、`/openapi.json`。
- 内部路由：`/api/v1/internal/**` 或明确标记为 internal/debug/metrics 的路由。
- 临时调试接口。

排除公开覆盖校验的路由必须有明确规则，不能用排除规则掩盖真实公开 API 缺失。

## 路由注解一致性

- `@BasePath` 必须与真实 Gin 路由组一致，例如 `router.Group("/api/v1/meta")` 对应 `@BasePath /api/v1/meta`。
- `@Router` 必须与真实 Gin 路由一致。Go 路由中的 `:id` 在 Swagger 中必须写为 `{id}`。
- `@Router` 只写 `@BasePath` 之后的相对路径，例如真实路由 `/api/v1/meta/engines/:engine_id/tree` 应写：

```go
// @Router /engines/{engine_id}/tree [get]
```

- HTTP method 必须一致，不能真实路由是 `POST` 而 Swagger 写成 `[get]`。
- 删除或迁移 API 时，必须删除旧 `@Router` 注解并重新生成文档，避免旧 path 残留。

## IAM 授权扩展

每个公开 Operation 必须显式输出 `x-addp-auth-mode`，取值只能是：

```text
public | authenticated | self | permission | delegated_tool | resource_ticket | internal
```

`permission`、`delegated_tool` 和 `resource_ticket` 必须同时输出非空、无重复的 `x-addp-required-permissions` 数组；其他模式不得携带业务 Permission Key。多个 Permission 固定按 all-of 处理。

当路由具有稳定的功能 Permission，但还会根据严格解析后的请求内容选择额外 Permission 时，只能在 `permission` 模式下声明 `x-addp-conditional-permissions`。该数组列出服务端可能动态追加校验的完整 Permission 集合，用于授权覆盖报告确认真实消费者；它不属于静态 all-of Guard，不能与 `x-addp-required-permissions` 重复。服务端必须默认拒绝无法分类的请求，不能把条件权限当作 any-of，也不能由客户端直接指定要绕过的 Permission。

Go swaggo 目标注解：

```go
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
```

动态效果示例：

```go
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute"]
// @x-addp-conditional-permissions ["develop.data_read.execute","develop.data_write.execute","develop.data_ddl.execute","develop.data_external_effect.execute"]
```

FastAPI 目标声明：

```python
openapi_extra={
    "x-addp-auth-mode": "permission",
    "x-addp-required-permissions": ["manager.data_item.read"],
}
```

Permission Key 必须来自其事实 owner 的 `authorization/permissions.yaml` 及生成常量，不得在注解中发明新 Key、使用通配符或按 URL 前缀推导 Permission。Portal、代理路由或跨模块投影可以显式引用其他 owner 的 Permission，最终仍由该事实 owner 执行资源策略。

授权覆盖报告使用：

```bash
cd common
go run ./authorization/cmd/manifest --coverage-report --repository-root ..
```

该报告与 `scripts/swagger/check-route-coverage.sh` 职责不同：后者验证真实路由与 Swagger path/method 一致，前者验证 Swagger Operation 与 Permission/Tool 契约一致；两者都必须通过才能生成最终 IAM SQL seed。

## 为 Go 模块添加 Swagger 的步骤

### 1. 添加依赖

在模块的 `go.mod` 中添加（使用上面统一的版本号）：

```bash
cd <module>/backend
go get github.com/swaggo/gin-swagger@v1.6.0
go get github.com/swaggo/files@v1.0.1
```

### 2. 在 main.go 添加总注解

```go
// @title           ADDP <ModuleName> API
// @version         1.0
// @description     全域数据平台 - <模块名称> API

// @host      localhost:<port>
// @BasePath  /api/v1/<module>

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and an ADDP User Access Token.
func main() {
```

各模块端口和 BasePath 对照：

| 模块 | 端口 | BasePath |
|------|------|----------|
| system | 8180 | /api/v1/system |
| manager | 8081 | /api/v1/manager |
| meta | 8082 | /api/v1/meta |
| transfer | 8083 | /api/v1/transfer |
| orchestrator | 8084 | /api/v1/orchestrator |
| develop | 8185 | /api/v1/develop |
| service | 8086 | /api/v1/service |
| monitor | 8100 | /api/v1/monitor |
| standard | 8110 | /api/v1/standard |
| model | 8181 | /api/v1/model |
| quality | 8182 | /api/v1/quality |
| portal | 8184 | /api/v1/portal |
| graph | 8186 | /api/v1/graph |

### 3. 在 Handler 方法上添加端点注解

基本格式：

```go
// MethodName godoc
// @Summary      简短描述（一句话）
// @Description  详细描述
// @Tags         标签名（用于分组）
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        参数名 [query|path|body] 类型 是否必须 "描述"
// @Success      200 {object} 响应类型
// @Failure      400 {object} models.ErrorResponse
// @Router       /路径 [get|post|put|delete]
func (h *Handler) MethodName(c *gin.Context) {
```

#### 常用 @Param 示例

```go
// 路径参数
// @Param id path int true "ID"

// 查询参数
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(10)

// Body 参数
// @Param request body models.CreateRequest true "创建请求"
```

#### 常用 @Success/@Failure 示例

```go
// 单个对象
// @Success 200 {object} models.User

// 列表（分页）
// @Success 200 {object} object{data=[]models.User,total=int,page=int,page_size=int}

// 创建成功
// @Success 201 {object} models.User

// 无内容
// @Success 204

// 错误响应（统一使用 models.ErrorResponse）
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
```

### 4. 创建通用响应结构（如未继承 common）

在 `internal/models/swagger.go` 中：

```go
package models

// ErrorResponse 错误响应
type ErrorResponse struct {
    Error string `json:"error" example:"错误信息描述"`
}

// SuccessResponse 成功响应
type SuccessResponse struct {
    Message string `json:"message" example:"操作成功"`
}
```

### 5. 在 router.go 注册 Swagger 路由

```go
import (
    swaggerFiles "github.com/swaggo/files"
    ginSwagger "github.com/swaggo/gin-swagger"
    _ "github.com/addp/<module>/docs" // 导入生成的 docs
)

func SetupRouter(...) *gin.Engine {
    router := gin.Default()
    // ...

    // Swagger 文档（放在认证路由之前）
    router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

    // ... 其他路由
}
```

### 6. 生成文档

```bash
cd <module>/backend
~/go/bin/swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal
```

> 注意：必须使用 `--parseDependency --parseInternal` 选项，因为 ADDP 使用了 common 共享模块，Swagger 需要解析跨模块的类型定义。

生成后会创建：
- `docs/docs.go` - Go 代码（供 `_ "..."` 导入）
- `docs/swagger.json` - OpenAPI JSON 规范
- `docs/swagger.yaml` - OpenAPI YAML 规范

这些生成产物必须随代码同步更新并提交。

### 7. 验证

重启服务后访问：`http://localhost:<port>/swagger/index.html`

静态覆盖校验：

```bash
bash scripts/swagger/check-route-coverage.sh <module>
```

该脚本会对比真实公开路由与 `docs/swagger.json` 中的 paths，发现：

- Swagger 缺失真实公开路由。
- Swagger 残留已删除或已迁移的旧 path。
- `@Router` method 与真实路由不一致。
- `@BasePath` 与真实路由组不一致。

脚本会统一 Gin `:id` 与 Swagger `{id}` 的路径参数格式，并排除 health、swagger、docs、internal、debug、metrics 等不需要公开的路由。

## Python 模块（FastAPI）

FastAPI 模块（Agent、Copilot）无需额外配置，自动提供：

- Swagger UI：`/docs`
- ReDoc：`/redoc`
- OpenAPI JSON：`/openapi.json`

新增、删除、修改 FastAPI 路由时，同样必须确保 `/openapi.json` 能反映真实 API。Console API 文档中心接入 FastAPI 模块时，应使用该 OpenAPI JSON 作为来源。

## 常见问题

### 生成成功但接口缺失

通常是 Handler 缺少 `@Router` 注解，或注解没有紧贴可被 swaggo 解析的函数。补齐注解后重新执行 `gen-swagger.sh` 和 `check-route-coverage.sh`。

### 路径显示旧值

通常是路由迁移后旧 `@Router` 没删除，或生成产物未更新。删除旧注解并重新生成 `docs/swagger.json`、`docs/swagger.yaml`、`docs/docs.go`。

### BasePath 错误

检查 `cmd/server/main.go` 中的 `@BasePath`，必须与模块真实 `router.Group("/api/v1/<module>")` 一致。BasePath 错误会导致 Console API 文档中心展示的接口前缀不准确。

### Swagger UI 可访问但内容错误

Swagger UI 可访问只说明静态文档服务正常，不代表 API 文档准确。必须同时执行 route coverage 校验。

## 注解规范

### 双语注解

Swagger 注解中面向人阅读的描述应使用 `中文 | English` 格式，中文在前，英文在后。运行时错误消息不依赖 Swagger 注解翻译，必须按 [国际化开发规范](addp国际化开发规范.md) 使用前后端 i18n 机制。

```go
// @Summary 获取用户列表 | Get user list
// @Description 根据条件分页查询系统用户列表 | Query system user list with pagination
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Success 200 {object} response.Data "返回用户列表 | Return user list"
```

### 标签（Tags）分组建议

按功能模块分组，使用中文：

```go
// @Tags 认证
// @Tags 用户管理
// @Tags 引擎管理
// @Tags 日志管理
// @Tags 租户管理
```

### 示例值

为重要字段添加 `example` 标签：

```go
type LoginRequest struct {
    Username string `json:"username" example:"admin"`
    Password string `json:"password" example:"123456"`
}
```

### 优先级

所有公开 HTTP API 都应有注解。历史模块补齐时按以下优先级推进：

1. **P0**：认证相关（登录、刷新）
2. **P0**：核心 CRUD（List、Create、GetByID）
3. **P1**：其他常用操作（更新、删除、测试连接等）
4. **P2**：内部 API（通常不需要 Swagger）

## 常见问题

### Q: 生成时报 "cannot find type definition"

**原因**：模型引用了 common 模块的类型，swag 默认不解析外部依赖。

**解决**：使用 `--parseDependency` 选项：
```bash
swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal
```

### Q: docs 目录要不要提交到 git？

**建议**：提交 `docs/` 目录。原因：
- 保证 CI/CD 环境无需安装 swag 工具也能编译
- 文档内容与代码同步提交，便于追踪变更

### Q: Swagger UI 显示的 BasePath 不对？

检查 `main.go` 的 `@BasePath` 注解是否与路由组的前缀一致。

### Q: 如何在 Swagger UI 中测试需要认证的接口？

1. 先调用登录接口（`POST /login`）获取 token
2. 点击 Swagger UI 右上角的 **Authorize** 按钮
3. 在 `BearerAuth (apiKey)` 输入框中填入 `Bearer <your-token>`
4. 点击 Authorize，之后所有请求都会自动携带该 token

## 参考

- [Swaggo 官方文档](https://github.com/swaggo/swag)
- [System 模块实现](../../system/backend/internal/api/)
- [Swagger 注解规范](https://swagger.io/docs/specification/about/)
