# ADDP Swagger 集成指南

本文定义 ADDP HTTP API 的 Swagger/OpenAPI 生成、注解和覆盖检查要求。Go + Gin 使用 swaggo，FastAPI 使用框架原生 OpenAPI；响应与路由契约遵循[API 设计规范](addp-API设计规范.md)。

常用入口：[生成与检查](#强制同步要求)、[依赖与工具](#依赖版本)、[IAM 声明](#iam-授权扩展)、[Go 接入](#为-go-模块添加-swagger-的步骤)、[排障](#常见问题)。

## 强制同步要求

新增、删除或修改公开 HTTP API 时，必须同步真实路由、Handler、请求/响应 DTO、调用方及 Swagger/OpenAPI 描述；删除旧路径的同时删除旧注解。生成成功不等于覆盖完整。

| 模块类型 | 随代码提交的生成产物 |
| --- | --- |
| Go | `<module>/backend/docs/docs.go`、`swagger.json`、`swagger.yaml` |
| FastAPI | `<module>/backend/openapi.json` |

从仓库根目录使用标准脚本，例如：

```bash
# 单模块
bash scripts/swagger/gen-swagger.sh system
bash scripts/swagger/check-route-coverage.sh system

# 多模块
bash scripts/swagger/gen-swagger.sh system manager
bash scripts/swagger/check-route-coverage.sh system manager

# 全模块
bash scripts/swagger/gen-swagger.sh all
bash scripts/swagger/check-route-coverage.sh all
```

模块清单由[生成脚本](../../scripts/swagger/gen-swagger.sh)与[覆盖检查脚本](../../scripts/swagger/check-route-coverage.sh)登记；两者无参数都选择全部模块。生成入口负责工作区锁、输入指纹与产物发布，不以模块目录中的裸 `swag init` 替代。

`restart.sh -<module>` 和 `restart.sh -all` 也会生成并检查文档，失败时不得继续编译和启动；它们不能替代开发者补全注解。覆盖检查结果：0 为通过，1 为已执行比较但不一致，2 为检查无法执行，例如缺少入口、路由或文档、Python 环境缺失、应用导入或 OpenAPI 导出失败。非零都应阻断，不提供告警降级或跳过检查的开关。

FastAPI 生成与比较均使用所属模块唯一的 `backend/venv/bin/python`，不回退到系统 Python。仅导入真实应用并读取 `app.openapi()`，不进入 lifespan、不启动服务或连接运行中的 ADDP。平台 CI 先通过现有 `prepare-python-gate` action 准备 Agent、Copilot 各自的环境。

根 `make test-swagger` 执行检查脚本回归和全模块覆盖比较，`make test-authorization` 调用同一入口，并验证授权契约。门禁范围按[测试与验收规范](addp测试与验收规范.md)选择，不能用 Swagger UI 可访问代替检查通过。

## 开发期增量生成

`gen-swagger.sh` 对 Go 模块使用内容指纹复用已成功生成的文档，缓存保存在不提交的 `.dev-state/swagger/`。指纹覆盖整个当前 Go workspace 的本地模块及本地 replace 源码、模块依赖声明和锁文件、`.swaggo`、Go 环境、swag 可执行文件及生成脚本。共享输入只计算一次；任一本地 Go 源码变化会保守地使所选 Go 模块重新生成，不依赖文件时间戳或 Git 状态。第三方模块仍以 Go 版本及校验和锁定，不支持直接修改 Go 模块下载缓存。

复用还必须逐一校验 `docs.go`、`swagger.json`、`swagger.yaml` 的内容摘要；缓存缺失、损坏或产物缺失、被修改均重新生成。并发生成命令通过工作区文件锁串行进入，锁内各模块仍并行执行。生成失败或生成期间输入变化不能留下有效的新缓存；输入变化会使本次命令失败，需重新执行。删除 `.dev-state/swagger/manifest.json` 可要求下一次重新生成。

FastAPI 继续实时导出 OpenAPI，避免遗漏运行环境对动态 schema 的影响。增量复用不跳过路由覆盖校验，也不改变发布检查的覆盖门禁。干净 CI 工作区没有本地缓存，首次始终重新生成。

## 依赖版本

`swag`、`gin-swagger`、`files` 的统一版本以[技术栈规约](addp技术栈规约.md)为准，模块依赖在各自 `go.mod` / `go.sum` 中维护；本指南不再维护另一张版本表。

## 工具安装

需要手工准备 CLI 时，按技术栈规约固定版本安装，例如当前规定的 swag 版本：

```bash
go install github.com/swaggo/swag/cmd/swag@v1.16.6
```

生成脚本先查 PATH，再查 `~/go/bin/swag`。当前脚本在 CLI 缺失时仍自动安装 `v1.16.4`，且不会校验已有 CLI 是否符合规约；这与规约的 `v1.16.6` 尚未对齐。使用前核对实际工具版本，不把自动安装成功视为版本合规，也不使用 `@latest` 隐式改变工具链。

## 文档边界

必须纳入 Swagger/OpenAPI：

- 面向 Console 和各模块前端的公开 HTTP API。
- 模块间通过 HTTP 调用的 API。
- 对外开放的数据服务 API。

通常不纳入 Swagger/OpenAPI：

- 进程存活与模块就绪检查：`/health/live`、`/health/ready`。两者的响应契约由 [ADDP API 设计规范](api/健康与平台控制面.md#进程存活与模块就绪端点) 和公共类型统一维护，不在各模块 Swagger 重复声明。
- Swagger/OpenAPI 自身路由：`/swagger/*any`、`/docs`、`/redoc`、`/openapi.json`。
- 内部路由：`/api/v1/internal/**` 或明确标记为 internal/debug/metrics 的路由。
- 临时调试接口。

排除公开覆盖校验的路由必须有明确规则，不能用排除规则掩盖真实公开 API 缺失。具体过滤由标准检查脚本维护；Inference 的 `/api/v1/inference/internal/` 路由明确纳入覆盖，不能仅凭路径含 `internal` 就将其排除。

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

在模块 `go.mod` 中登记 swaggo 相关依赖，版本按[依赖规则](#依赖版本)维护。仅初始化 Swagger 不应覆盖已有模型或引入新的响应包装格式。

### 2. 在 main.go 添加总注解

声明 API 标题、版本、说明及与真实路由组一致的 `@BasePath /api/v1/<module>`；启用 Bearer 认证的接口使用 `Authorization` header，例如：

```go
// @BasePath /api/v1/<module>
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Bearer 后接 ADDP User Access Token | Bearer followed by an ADDP User Access Token.
```

运行地址按部署输出和[端口规范](addp端口分配.md)确定，不在本指南重复列模块端口表。`@BasePath` 与 `@Router` 的拼接规则见[路由注解一致性](#路由注解一致性)。

### 3. 在 Handler 方法上添加端点注解

注解紧贴实际 Handler，至少覆盖以下内容：

| 注解 | 要求 |
| --- | --- |
| `@Summary`、`@Description`、`@Tags` | 按[注解规范](#注解规范)描述实际行为并分组 |
| `@Accept`、`@Produce` | 与实际请求及响应媒体类型一致 |
| `@Param` | 声明 path / query / body 的名称、类型、必填性与说明；分页参数按端点实际契约声明 |
| `@Success`、`@Failure` | 状态码、DTO 与真实 JSON 结构一致；列表声明真实分页字段，不能用统一包装示例覆盖实际响应 |
| `@Security` | 声明实际认证方案；需要 Bearer 时使用 `BearerAuth` |
| `@x-addp-*` | 每个公开 Operation 按[IAM 授权扩展](#iam-授权扩展)声明认证模式及适用 Permission |
| `@Router` | 使用 BasePath 后的相对路径及真实 method；`:id` 写为 `{id}` |

### 4. 创建通用响应结构（如未继承 common）

先复用已有的公共或模块响应类型、别名；确需模块 DTO 时放在既有 `internal/models/` 等位置。Swagger 只描述实际响应，不为了文档新增 `code/message/data` 包装，也不按本指南另造一套成功或错误格式。结构与错误策略见[响应规范](api/响应与错误.md)，共享实现见[API 实施规范](api/文档与实施.md#共享响应处理)。

### 5. 在 router.go 注册 Swagger 路由

导入本模块生成的 docs 包（路径取模块 `go.mod`），使用 `gin-swagger` 和 `swaggerFiles.Handler` 注册 `/swagger/*any`。文档路由与受保护业务路由分开，不为展示文档放宽业务认证；Swagger 自身路由不计入公开 API 覆盖。

### 6. 生成文档

使用[标准生成入口](#强制同步要求)，不在各模块重复维护生成命令。脚本已传入 `--parseDependency --parseInternal` 以解析共享依赖；Go 三份产物均须生成并提交，不能只提交 JSON 或手工修改生成文件。

### 7. 验证

执行同节的覆盖检查，核对遗漏接口、旧路径、method、BasePath 和路径参数。检查脚本统一 Gin 与 Swagger 参数表示，并按[文档边界](#文档边界)过滤；授权声明另按 IAM 契约校验。

服务已运行且需要查看页面时，按当前实际地址访问 `/swagger/index.html`。UI 可用只证明文档页面可访问，不能代替静态覆盖或接口行为验收；无需为纯文档检查重启服务。

## Python 模块（FastAPI）

FastAPI 模块（Agent、Copilot）由框架提供：

- Swagger UI：`/docs`
- ReDoc：`/redoc`
- OpenAPI JSON：`/openapi.json`

新增、删除、修改 FastAPI 路由时，必须同步端点描述、请求/响应模型、认证声明和 `openapi_extra`，并通过标准脚本重新导出、提交 `backend/openapi.json`；覆盖检查使用实时 `app.openapi()` 比较该投影。Console API 文档中心使用 OpenAPI JSON，不另建接口清单。

开发重启必须在停止所选 Agent/Copilot 后，复用生命周期唯一依赖指纹与安装锁同步环境，再导入应用生成和校验 OpenAPI；不能以已有 venv 或少量可导入包推定完整。全量重启准备两个 Backend，局部只准备所选 Backend。独立文档生成／覆盖检查只使用 Owner venv，不隐式安装依赖，不改写活动服务环境；依赖准备与失败边界见[生命周期规范](addp开发服务生命周期与构建身份规范.md#python-依赖同步)。

## 常见问题

| 问题 | 核对与处理 |
| --- | --- |
| 生成成功但接口缺失 | 检查真实 Handler 是否有可解析的 `@Router`、注解是否紧贴函数，再通过标准入口生成并检查覆盖 |
| 路径仍为旧值或 BasePath 错误 | 对照实际路由组、`@BasePath` 和相对 `@Router`；删除旧注解，重新生成全部产物 |
| `cannot find type definition` | 核对 DTO、导入与共享依赖；标准入口已包含依赖解析参数，不另写裸 `swag init` 命令 |
| 缺少 FastAPI 环境或无法导入应用 | 按模块声明准备其唯一 venv，保留原始错误；不换系统 Python、不跳过覆盖检查 |
| Swagger UI 可用但内容错误 | 页面可访问不代表契约正确，继续核对生成产物和标准覆盖检查结果 |
| 生成目录是否提交 | Go 三份 docs 产物与 FastAPI `backend/openapi.json` 均随契约变更提交，不提交缓存或虚拟环境 |
| Swagger UI 如何调用受保护接口 | 按[登录规范](addp登录认证的统一要求.md#61-登录)完成必要 MFA 与 Context 选择，取得当前有效的短期 Access Token，再在 Authorize 中填入 `Bearer <access_token>`；Refresh Cookie 和其他票据不作为 Bearer Token |

## 注解规范

### 双语注解

Swagger 注解中面向人阅读的描述应使用 `中文 | English` 格式，中文在前，英文在后。运行时错误消息不依赖 Swagger 注解翻译，必须按 [国际化开发规范](addp国际化开发规范.md) 使用前后端 i18n 机制。

```go
// @Summary 获取用户列表 | Get user list
// @Description 根据条件分页查询系统用户列表 | Query system user list with pagination
// @Param page query int false "页码，默认1 | Page number, default 1"
```

### 标签（Tags）分组建议

按模块领域分组，复用已有 Tag，例如认证、用户管理、引擎管理；不要以每个 Handler 或每条路由另造分组。

### 示例值

重要字段可用 `example` 标签给出与类型、枚举和真实校验一致的公开样例；示例不使用真实账号密码、Token 或连接 Secret。

### 优先级

所有公开 HTTP API 都应有注解。历史模块补齐时按以下优先级推进：

1. **P0**：认证相关（登录、刷新）
2. **P0**：核心 CRUD（List、Create、GetByID）
3. **P1**：其他常用操作（更新、删除、测试连接等）
4. 内部 API 是否纳入按[文档边界](#文档边界)及显式规则判定，不以优先级跳过应覆盖的接口。

## 参考

- [Swaggo 官方文档](https://github.com/swaggo/swag)
- [System 模块实现](../../system/backend/internal/api/)
- [Swagger 注解规范](https://swagger.io/docs/specification/about/)
