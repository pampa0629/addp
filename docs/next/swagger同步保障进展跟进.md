# Swagger 同步保障进展跟进

更新时间：2026-05-04

本文跟进 ADDP 全平台 Swagger/OpenAPI 同步保障专项。目标不是只修 Meta 模块，而是建立所有带 HTTP API 模块都必须遵守的 Swagger 文档治理机制。

## 一、专项目标

当前存在的核心风险是：

```text
真实 API 已经新增、删除或改路径
Swagger 注解没有同步修改
生成脚本仍然执行成功
Console API 文档中心展示缺失接口或旧接口
```

本专项要解决的是：

1. API 修改时，开发规范明确要求同步 Swagger。
2. AI/人类开发者在改 API 前能从 `AGENTS.md` 明确读到 Swagger 集成指南。
3. 当前所有带 API 的模块 Swagger 尽量补齐到可用状态。
4. 增加验证脚本，自动发现 Swagger 缺失、旧 path 残留、BasePath 不一致等问题。
5. 将验证脚本接入开发脚本，使问题在重启/验证阶段及时告警。

## 二、执行任务清单

### 任务 1：完善 Swagger 集成指南

目标文档：

```text
docs/spec/addp-Swagger集成指南.md
```

当前状态：已完成

需要补充的重点：

- 明确该规范适用于所有带 HTTP API 的模块，不限 Go 模块；Go 使用 swaggo，FastAPI 使用内置 OpenAPI。
- 明确“新增、删除、修改公开 API 时必须同步 Swagger 注解和生成产物”。
- 明确 `@Router` 必须与真实 Gin 路由一致，路径参数格式必须按 Swagger 使用 `{id}`。
- 明确模块 `@BasePath` 必须与真实路由组一致。
- 明确公开接口、内部接口、健康检查、Swagger 自身路由的文档边界。
- 明确 API 修改后的必跑命令：

```bash
bash scripts/swagger/gen-swagger.sh <module>
bash scripts/swagger/check-route-coverage.sh <module>
```

- 明确 `restart.sh -<module>` / `restart.sh -all` 会参与 Swagger 生成和验证，但不能替代开发者补注解。
- 补充常见问题：生成成功但接口缺失、路径显示旧值、BasePath 错误、Swagger UI 可访问但内容错误。

验收标准：

- 文档中出现“API 修改必须同步 Swagger”的强约束。
- 文档中明确生成和覆盖校验命令。
- 文档中明确 route coverage 校验的作用和排除规则。
- 文档中明确 `docs/swagger.json`、`docs/swagger.yaml`、`docs/docs.go` 需要随代码同步更新。

### 任务 2：更新 AGENTS.md 引用

目标文件：

```text
AGENTS.md
```

当前状态：已完成

需要补充的重点：

- 必读文档导航中已经有 API 设计和 Swagger 集成指南引用，需要强化为强制要求。
- 在“后端与 API 约定”中增加：
  - 新增或修改 API 前必须阅读 `docs/spec/addp-Swagger集成指南.md`。
  - API 路由、Handler、DTO、Swagger 注解、生成文档必须同步更新。
  - API 修改后必须运行 Swagger 生成和覆盖校验脚本。
  - 不允许只改路由或 handler 而留下旧 Swagger path。

验收标准：

- `AGENTS.md` 中明确写入 Swagger 集成指南。
- `AGENTS.md` 中明确 API 修改时必须执行 Swagger 同步和验证。
- 后续 AI 会话能从入口规范直接得到该要求。

### 任务 3：检查并完善所有模块当前 Swagger

目标范围：

```text
system
manager
meta
transfer
orchestrator
develop
service
monitor
standard
model
quality
portal
graph
agent
copilot
```

当前状态：已完成 Go/Gin 模块覆盖修复，FastAPI 运行时检查暂缓

初步判断：

- Go 后端模块通过 `scripts/swagger/gen-swagger.sh` 生成 `docs/swagger.json`、`docs/swagger.yaml`、`docs/docs.go`。
- Agent / Copilot 等 FastAPI 模块通过 `/openapi.json` 暴露文档。
- Console API 文档中心已经集中展示各模块 Swagger/OpenAPI。
- Meta 当前已知存在 Swagger 覆盖不足和旧 path 注解残留，应作为首个修复样例。

检查维度：

- 模块是否暴露 Swagger/OpenAPI。
- `@BasePath` 是否与真实路由一致。
- 真实公开路由是否被 Swagger paths 覆盖。
- Swagger 中是否残留已删除路由。
- `@Router` HTTP method 是否与真实路由一致。
- 公开描述是否泄漏具体旧抽象，例如过时的 schema/table 统称、对象存储专属路径冒充 Meta 主接口。
- 请求体、响应体和错误响应是否符合 `docs/spec/addp-API设计规范.md`。

建议执行顺序：

1. Meta：先修已知问题，作为 route coverage 校验样例。
2. System：作为引擎和认证基础模块，确保 Swagger 规范化。
3. Manager：接口多且依赖 Meta/System，优先保证数据管理和预览相关文档准确。
4. Transfer / Develop / Service / Orchestrator：业务流程接口较多，按模块补齐。
5. Monitor / Standard / Model / Quality / Portal / Graph：按实际公开 API 补齐。
6. Agent / Copilot：验证 FastAPI OpenAPI 是否可通过 Console 访问并符合基本命名。

验收标准：

- `bash scripts/swagger/gen-swagger.sh all` 成功。
- 新增的覆盖校验脚本对目标模块输出清晰结果。
- 对暂不纳入公开 Swagger 的内部路由有明确排除规则。
- Console API 文档中心不再展示明显旧 path 或严重缺失的核心 API。

### 任务 4：增加 Swagger 验证脚本并接入 dev 脚本

目标新增或改造脚本：

```text
scripts/swagger/check-route-coverage.sh
scripts/swagger/verify-swagger.sh
scripts/dev/restart.sh
```

当前状态：已完成

新增脚本建议：

```bash
bash scripts/swagger/check-route-coverage.sh <module>
bash scripts/swagger/check-route-coverage.sh all
```

校验逻辑：

```text
1. 提取模块真实公开路由。
2. 提取模块 docs/swagger.json 或 FastAPI openapi.json 中的 paths。
3. 统一路径参数格式：Gin 的 :id 转为 Swagger 的 {id}。
4. 拼接或剥离 BasePath 后比较真实 path。
5. 排除 health、swagger、自身 docs、internal、debug、metrics 等不需要公开的路由。
6. 输出 missing、stale、method mismatch、basePath mismatch。
7. 严重问题返回非零状态码。
```

告警示例：

```text
[meta] missing in swagger:
  POST /api/v1/meta/scan/engine
  GET  /api/v1/meta/engines/{engine_id}/tree

[meta] stale in swagger:
  GET /api/v1/meta/object-storage/{engine_id}/nodes

[meta] route mismatch:
  router:  GET /api/v1/meta/engines/{engine_id}/storage/nodes
  swagger: GET /api/v1/meta/object-storage/{engine_id}/nodes
```

接入 `restart.sh` 的建议：

- `restart.sh -<module>`：生成该模块 Swagger 后运行该模块覆盖校验。
- `restart.sh -all`：生成全部 Swagger 后运行全量覆盖校验。
- 默认生成失败应中断启动。
- 如需临时容忍失败，使用显式环境变量，例如：

```bash
ALLOW_SWAGGER_FAILURE=1 ./scripts/dev/restart.sh -meta
```

- 覆盖校验初期可先以 warning 模式接入，待模块补齐后切换为 fail-fast。

验收标准：

- 验证脚本能发现 Meta 当前这类“真实路由存在但 Swagger 缺失 / Swagger 旧 path 残留”的问题。
- 验证脚本输出能定位到模块、method、path 和问题类型。
- `restart.sh -meta` / `restart.sh -all` 能触发生成和校验。
- 有明确方式临时跳过或降级告警，避免历史欠账一次性阻断所有开发。

## 三、当前已知问题

### Meta 模块

当前状态：已修复

已知问题：

- 真实路由远多于 Swagger 展示的接口。
- 多个扫描、节点、数据项、字段、空间元数据接口缺少 Swagger 注解。
- `ListObjectStorageNodes` 注解中的 `@Router /object-storage/{engine_id}/nodes [get]` 与真实路由 `/engines/:engine_id/storage/nodes` 不一致。
- Swagger 展示出来的接口容易让 Meta 看起来像对象存储专属模块，而不是元数据中枢。

后续处理：

- 已修真实路径和核心公开接口注解。
- 已将 `/stats` 从匿名 Handler 调整为具名 Handler，便于 Swagger 注解跟踪。
- 已重新生成 Meta Swagger。
- 已通过新增覆盖校验脚本验证，当前 Meta 公开路由方法覆盖一致。

### 全平台

当前状态：已初步检查，待分批修复

可能存在的问题类型：

- 只有 Swagger UI 可访问性验证，没有内容覆盖验证。
- 部分模块新增接口后未补注解。
- 部分模块 Swagger path 可能和 router path 不一致。
- 部分模块 generated docs 可能不是最新。
- FastAPI 模块和 Go 模块缺少统一入口级检查。

2026-05-04 初步覆盖校验结果：

- 已通过：`meta`、`quality`、`portal`。
- FastAPI 静态校验暂跳过：`agent`、`copilot`，后续应补运行时 `/openapi.json` 可访问性和命名检查。
- 存在 Swagger 覆盖欠账：`system`、`manager`、`transfer`、`orchestrator`、`develop`、`service`、`monitor`、`standard`、`model`、`graph`。
- 典型问题包括真实路由 missing、旧函数名式 path stale、少量路由迁移后的 method/path 不一致。

2026-05-04 修复后全量覆盖校验结果：

- Go/Gin 模块已全部通过：`system`、`manager`、`meta`、`transfer`、`orchestrator`、`develop`、`service`、`monitor`、`standard`、`model`、`quality`、`portal`、`graph`。
- `service` 已补齐注册服务、查询执行、图查询执行、OGC Features、数据源代理、服务端点、资产发现等 Swagger 注解，并处理公开瓦片通配符路由与 OpenAPI path 表达的归一化。
- `standard` 已将旧函数名式 `@Router` 同步为真实 REST 路径，并补齐资产发现接口注解。
- `manager` 已将旧函数名式 `@Router` 同步为真实 REST 路径；对复用 handler 的任务接口补多路由注解；移除未挂载扫描任务 handler 的过时 `@Router`，避免旧接口继续进入 Swagger。
- FastAPI 模块 `agent`、`copilot` 当前仍按脚本说明跳过静态覆盖校验，后续应补运行时 `/openapi.json` 可访问性和路径命名检查。

## 四、执行顺序

推荐按以下顺序推进：

1. 修改本文档，形成专项台账。（已完成）
2. 完善 `docs/spec/addp-Swagger集成指南.md`。
3. 更新 `AGENTS.md`，把 Swagger 同步要求写入入口规范。
4. 设计并实现 `scripts/swagger/check-route-coverage.sh`。
5. 先在 Meta 模块跑验证，修复 Meta Swagger。
6. 扩展到 System、Manager 等核心模块。
7. 全量扫描所有带 API 模块，记录并修复问题。
8. 将验证脚本接入 `scripts/dev/restart.sh`。
9. 待历史问题收敛后，把 warning 模式切换为默认失败。

## 五、状态跟踪

| 序号 | 工作项 | 状态 | 备注 |
| --- | --- | --- | --- |
| 1 | 将 Swagger 同步保障文档改为专项进展跟进文档 | 已完成 | 本文档 |
| 2 | 完善 `docs/spec/addp-Swagger集成指南.md` | 已完成 | 已补强制同步、文档边界、必跑命令和常见问题 |
| 3 | 更新 `AGENTS.md` | 已完成 | 已强化 API 修改时的 Swagger 要求 |
| 4 | 新增 route coverage 验证脚本 | 已完成 | 已支持 Go/Gin 静态覆盖校验，FastAPI 暂运行时检查 |
| 5 | 接入 `scripts/dev/restart.sh` | 已完成 | 生成失败默认中断，覆盖校验初期 warning 模式 |
| 6 | 修复 Meta Swagger | 已完成 | 覆盖校验通过，旧 object-storage path 已移除 |
| 7 | 检查并完善所有模块 Swagger | 已完成 | Go/Gin 模块全量覆盖校验通过；FastAPI 运行时检查列入暂缓项 |

## 六、验收口径

阶段性验收：

```bash
bash scripts/swagger/gen-swagger.sh meta
bash scripts/swagger/check-route-coverage.sh meta
```

全量验收：

```bash
bash scripts/swagger/gen-swagger.sh all
bash scripts/swagger/check-route-coverage.sh all
```

开发脚本验收：

```bash
./scripts/dev/restart.sh -meta
./scripts/dev/restart.sh -all
```

最终目标：

- API 修改时规范、入口说明、生成脚本、覆盖校验四者闭环。
- Console API 文档中心展示的接口与真实公开 API 基本一致。
- 新增公开 API 如果未补 Swagger，能在本地脚本或门禁中被发现。
- 已删除 API 如果仍残留在 Swagger 中，能被自动告警。

## 七、暂缓项

以下不作为本阶段必须完成：

- 引入 OpenAPI-first 开发模式。
- 从统一路由定义表自动生成 Gin 路由和 Swagger。
- 要求所有内部接口都进入 Swagger。
- 一次性把全部历史 Swagger 注解重构为完美状态。

当前阶段优先建立可执行的治理闭环，再逐步消化历史欠账。

## 八、最新验证结果

2026-05-04 已执行：

```bash
bash -n scripts/swagger/check-route-coverage.sh scripts/swagger/verify-swagger.sh scripts/dev/restart.sh
bash scripts/swagger/gen-swagger.sh service
bash scripts/swagger/check-route-coverage.sh service
bash scripts/swagger/gen-swagger.sh standard
bash scripts/swagger/check-route-coverage.sh standard
bash scripts/swagger/gen-swagger.sh manager
bash scripts/swagger/check-route-coverage.sh manager
bash scripts/swagger/check-route-coverage.sh all
```

全量覆盖校验结论：

```text
system      42 个公开路由方法一致
manager     47 个公开路由方法一致
meta        31 个公开路由方法一致
transfer    43 个公开路由方法一致
orchestrator 11 个公开路由方法一致
develop     59 个公开路由方法一致
service     55 个公开路由方法一致
monitor      7 个公开路由方法一致
standard    87 个公开路由方法一致
model       38 个公开路由方法一致
quality     16 个公开路由方法一致
portal      12 个公开路由方法一致
graph       63 个公开路由方法一致
```
