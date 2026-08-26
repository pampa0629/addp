# /api/{module} 路径专项清理记录

> 状态：专项待办记录。本文只记录后续清理范围和建议路线，不作为正式规范；正式约束以 `docs/spec/addp-API设计规范.md`、`docs/spec/addp-Swagger集成指南.md` 和各模块 Swagger 为准。

## 背景

ADDP 当前模块管理 API 的主路径应统一为：

```text
/api/v1/{module}/...
```

旧式路径 `/api/{module}/...` 容易和当前 Gateway 透明代理、Swagger BasePath、前端 API client、模块自注册 endpoint 产生歧义。上一轮任务体系收口时，已清理了部分明确旧路径，例如 Develop 的 `/api/develop/health` 兼容别名，以及若干文档中的 `/api/system`、`/api/develop`、`/api/graph`、`/api/quality` 示例。

## 当前原则

1. 模块管理 API 使用 `/api/v1/{module}`，不新增 `/api/{module}` 旧路径。
2. Gateway 的模块透明代理按 `/api/v1/:module/*path` 识别模块。
3. 模块根健康检查使用 `/health/live` 与 `/health/ready`，不放在 `/api/{module}/health`。
4. 已确认是旧兼容别名的路由应直接删除，不保留双轨。
5. Service 发布的数据访问端点可能具有用户侧公开 URL 语义，不能和普通模块管理 API 机械等同，需要单独确认。

## 当前代码事实（2026-06-22 盘点）

Service 后端当前真实注册了四类路径：

| 类别 | 当前路径 | 代码位置 | 初步判断 |
| --- | --- | --- | --- |
| 管理 API | `/api/v1/service/...` | `service/backend/internal/api/router.go` | 主路径正确 |
| 查询服务公开执行 | `POST /api/query/:serviceName/query` | `service/backend/internal/api/router.go`、`gateway/internal/router/router.go` | 公开 API，但游离于模块前缀之外 |
| 图查询服务公开执行 | `POST /api/gquery/:serviceName` | `service/backend/internal/api/router.go`、`gateway/internal/router/router.go` | 公开 API，但游离于模块前缀之外 |
| 注册服务公开代理 | `ANY /api/service/registered/proxy/:id/*path` | `service/backend/internal/api/router.go` | 使用旧式 `/api/service`，应清理 |
| OGC/瓦片标准访问 | `/ogc/features/...`、`/tiles/...`、`/wmts/...`、`/ogc/tiles/...` | `service/backend/internal/api/router.go`、`gateway/internal/router/router.go` | 行业/协议访问路径，先不并入本次 `/api/{module}` 清理 |

Service 前端存在一个实际调用风险：`service/frontend/src/api/client.js` 使用 `createAPIClient` 默认 `baseURL=/api/v1`，但部分公开端点测试 API 调用的是 `client.get('/query/...')`、`client.post('/gquery/...')`、`client.get('/tiles/...')`、`client.get('/ogc/...')` 等相对路径，实际会拼成 `/api/v1/query/...`、`/api/v1/gquery/...`、`/api/v1/tiles/...`、`/api/v1/ogc/...`，与后端和 Gateway 注册路径不一致。页面上用于展示和复制的 endpoint 多数直接拼 `window.location.origin + /api/query/...`、`/ogc/...`，与 API client 调用路径也不完全一致。

另外，`service/backend/internal/service/graph_query_service_service.go` 当前返回的图查询执行 endpoint 是 `/api/v1/gquery/:serviceName`，但真实路由是 `/api/gquery/:serviceName`，这属于明确不一致。

## 建议路径决策

建议将 Service 发布服务访问 API 统一定义为 Service 模块下的公开 API 命名空间：

```text
/api/v1/service/public/query/:serviceName
/api/v1/service/public/gquery/:serviceName
/api/v1/service/public/registered/proxy/:id/*path
```

理由：

1. 仍满足全平台 HTTP API 使用 `/api/v1/{module}` 的规范。
2. `public` 明确表达“发布服务访问 API”，和管理 API `/api/v1/service/query`、`/api/v1/service/graph`、`/api/v1/service/registered` 分离。
3. Gateway 可以继续透明代理到 Service，只需在受保护 `/api/v1/:module/*path` 之前注册 `/api/v1/service/public/...` 的公开转发规则，认证和 public/private 判断仍由 Service handler 内部完成。
4. 旧的 `/api/query`、`/api/gquery`、`/api/service/registered/proxy` 可以一次性删除，不保留兼容别名。

暂不建议迁移 OGC、WMTS、XYZ Tiles 路径。它们是协议入口，不是 ADDP 模块管理 API；本次只在 Gateway 文档中明确列为“Service 标准/公开访问路径”，避免和 `/api/{module}` 旧路径混为一谈。

## 已知已处理

| 类别 | 处理结果 |
| --- | --- |
| Develop 旧健康别名 | 已删除 `GET /api/develop/health`，统一使用 `GET /health/live` 与 `GET /health/ready` |
| TaskProvider 标准 endpoint | 已收敛为 `/api/v1/{module}/tasks...` 内的标准相对路径 `/tasks...` |
| API 规范文档 | 已补充 `/api/v1/{module}` 主线和 TaskProvider 分页例外 |
| 部分模块说明文档 | 已将明确旧的 `/api/system`、`/api/develop`、`/api/graph`、`/api/quality` 示例改为 `/api/v1/...` |

## 待专项盘点范围

### 1. Service 模块公开端点

Service 当前存在多类路径：

```text
/api/v1/service/...                 # 管理 API 主线
/api/query/:serviceName/query       # 查询服务公开执行端点
/api/gquery/:serviceName            # 图查询服务公开执行端点
/api/service/registered/proxy/:id/*path
```

这些路径中，`/api/v1/service/...` 是模块管理 API；其余路径更像“用户发布的数据服务访问 URL”。专项清理时需要先确认它们是否应：

1. 保持为非模块管理 API 的公开服务路径；
2. 迁移到更清晰的发布服务命名空间，例如 `/services/...` 或 `/api/v1/service/public/...`；
3. 或统一进入 `/api/v1/service/...`，并同步所有已发布 URL、文档、前端和 Gateway。

建议：优先把 Service 的“管理 API”和“已发布服务访问 API”在概念上拆开，再决定路径，而不是直接替换字符串。

### 2. Service 历史文档

当前大量 Service 文档仍包含 `/api/service/...`、`/api/query/...` 示例。专项时应按结论分三类处理：

| 类型 | 建议 |
| --- | --- |
| 管理 API 文档 | 改为 `/api/v1/service/...` |
| 公开服务访问 API | 按专项确认的新命名保留或迁移 |
| 过时规划文档 | 加状态说明，标明以现行 Swagger 和正式规范为准 |

### 3. Gateway 特殊转发

Gateway 当前除 `/api/v1/:module/*path` 外，还对 Service 公开查询端点做特殊转发。专项应确认：

1. 这些特殊转发是否仍是长期能力；
2. 是否需要纳入 Gateway 文档的“非模块管理 API”章节；
3. 若迁移路径，是否需要一次性调整 Service backend、Gateway、Service frontend、文档和测试。

## 建议推进路线

1. 在 `docs/spec/addp-API设计规范.md` 中补充“模块管理 API”和“发布服务访问 API”的边界：管理 API 必须是 `/api/v1/{module}/...`；Service 公开数据服务 API 使用 `/api/v1/service/public/...`；OGC/WMTS/XYZ 等行业协议路径作为协议入口单列。
2. 更新 Service 后端路由：新增唯一公开 API 组 `/api/v1/service/public`，迁移查询、图查询、注册服务代理；删除 `/api/query`、`/api/gquery`、`/api/service/registered/proxy`。
3. 更新 Gateway 路由：在受保护 `/api/v1` 组之前注册 `/api/v1/service/public/query/:serviceName`、`/api/v1/service/public/gquery/:serviceName`、`/api/v1/service/public/registered/proxy/:id/*path` 的公开转发；删除旧的 `/api/query`、`/api/gquery` 特殊转发；保留 OGC/WMTS/XYZ 公开转发。
4. 更新 Service 后端返回的 endpoints：`rest_api`、`execute`、`proxy` 等全部改为 `/api/v1/service/public/...`。
5. 更新 Service 前端：管理 API 继续使用默认 `/api/v1` client；公开端点测试和复制链接统一使用 `/api/v1/service/public/...`。OGC/WMTS/XYZ 测试若继续走根路径，需使用不带 `/api/v1` baseURL 的 public client 或直接 axios。
6. 同步更新 `service/CLAUDE.md`、Gateway README/架构说明、Service 核心文档和相关测试指南；历史规划文档只加状态说明，不继续维护旧示例。
7. 重新生成并校验 Service Swagger：

```bash
bash scripts/swagger/gen-swagger.sh service
bash scripts/swagger/check-route-coverage.sh service
```

8. 最后跑全量覆盖校验：

```bash
bash scripts/swagger/check-route-coverage.sh all
```

并按涉及模块补充后端测试、Gateway 路由测试或前端构建验证。

## 推荐首批落地清单

如果本专项继续推进，建议第一批只做 Service/Gateway 主路径收敛，不碰 OGC/WMTS/XYZ：

1. 文档决策：更新 API 规范、Gateway 文档、Service 模块说明。
2. 后端路由：Service 新增 `/api/v1/service/public/...`，删除旧公开 API 路由。
3. Gateway 路由：新增 `/api/v1/service/public/...` 公开转发，删除 `/api/query`、`/api/gquery`。
4. Endpoint 生成：修正 QueryService、GraphQueryService、RegisteredService 返回的公开 URL。
5. 前端调用：修正 Query/Graph/Registered 的公开端点测试路径；瓦片和 OGC 测试改为 public client，避免被 `/api/v1` baseURL 拼错。
6. 验证：Service Swagger 覆盖校验、Gateway 路由单测或最小 curl 验证、Service 前端构建。

## 暂不处理

以下内容不在本记录中直接定稿：

1. Service 发布服务 URL 是否必须进入 `/api/v1/service`。
2. `/api/query`、`/api/gquery` 是否需要保留为对外短路径。
3. OGC、WMTS、XYZ tiles 等行业标准访问路径是否应归入 API 版本前缀。

这些需要在 Service/Gateway 专项中结合用户访问语义、已发布服务稳定性和平台 clean break 原则再确认。
