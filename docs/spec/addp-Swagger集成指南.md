# ADDP Swagger 集成指南

本文档说明如何为 ADDP 的 Go 后端模块集成 Swagger UI，以 System 模块为参考实现。

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
// @description Type "Bearer" followed by a space and JWT token.
func main() {
```

各模块端口和 BasePath 对照：

| 模块 | 端口 | BasePath |
|------|------|----------|
| system | 8180 | /api/v1/system |
| manager | 8081 | /api/v1/manager |
| meta | 8082 | /api/v1/meta |
| develop | 8083 | /api/v1/develop |
| transfer | 8084 | /api/v1/transfer |
| service | 8085 | /api/v1/service |
| orchestrator | 8086 | /api/v1/orchestrator |
| monitor | 8087 | /api/v1/monitor |
| standard | 8088 | /api/v1/standard |
| model | 8089 | /api/v1/model |

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

### 7. 验证

重启服务后访问：`http://localhost:<port>/swagger/index.html`

## Python 模块（FastAPI）

FastAPI 模块（Agent、Copilot）无需额外配置，自动提供：

- Swagger UI：`/docs`
- ReDoc：`/redoc`
- OpenAPI JSON：`/openapi.json`

## 注解规范

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

不要为所有端点加注解，优先为以下端点添加：

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
