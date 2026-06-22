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
3. 模块根健康检查继续使用 `/health`，不放在 `/api/{module}/health`。
4. 已确认是旧兼容别名的路由应直接删除，不保留双轨。
5. Service 发布的数据访问端点可能具有用户侧公开 URL 语义，不能和普通模块管理 API 机械等同，需要单独确认。

## 已知已处理

| 类别 | 处理结果 |
| --- | --- |
| Develop 旧健康别名 | 已删除 `GET /api/develop/health`，保留 `GET /health` |
| TaskProvider 标准 endpoint | 已收敛为 `/api/v1/{module}/tasks...` 内的标准相对路径 `/tasks...` |
| API 规范文档 | 已补充 `/api/v1/{module}` 主线和 TaskProvider 分页例外 |
| 部分模块说明文档 | 已将明确旧的 `/api/system`、`/api/develop`、`/api/graph`、`/api/quality` 示例改为 `/api/v1/...` |

## 待专项盘点范围

### 1. Service 模块公开端点

Service 当前存在多类路径：

```text
/api/v1/service/...                 # 管理 API 主线
/api/query/:serviceName             # 查询服务公开执行端点
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

1. 先列出当前真实路由：从各模块 `router.go`、Gateway router、Swagger paths 生成清单。
2. 将路由按“模块管理 API / 公开数据服务 API / 健康检查 / OGC 或瓦片等标准访问 API / 历史兼容路径”分类。
3. 对“历史兼容路径”直接删除，并补测试或覆盖校验。
4. 对 Service 公开访问 API 先做概念决策，再做 clean break 迁移。
5. 同步更新 `docs/spec/addp-API设计规范.md`、Gateway 文档、Service 文档、Swagger 和前端 API client。
6. 最后跑：

```bash
bash scripts/swagger/check-route-coverage.sh all
```

并按涉及模块补充后端测试或前端构建验证。

## 暂不处理

以下内容不在本记录中直接定稿：

1. Service 发布服务 URL 是否必须进入 `/api/v1/service`。
2. `/api/query`、`/api/gquery` 是否需要保留为对外短路径。
3. OGC、WMTS、XYZ tiles 等行业标准访问路径是否应归入 API 版本前缀。

这些需要在 Service/Gateway 专项中结合用户访问语义、已发布服务稳定性和平台 clean break 原则再确认。
