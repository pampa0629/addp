# Orchestrator 模块说明

## 模块定位

Orchestrator 模块负责任务编排、DAG 执行、定时调度、跨模块任务调用和任务提供者发现。它不直接处理业务数据，而是编排 Meta、Transfer、Manager、Develop 等模块通过 TaskProvider 声明的任务能力。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8084`，环境变量 `ORCHESTRATOR_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5177`，启动脚本环境变量 `ORCHESTRATOR_FE_PORT`。
- 数据库：PostgreSQL `orchestrator` schema。
- 依赖：System、Redis、各任务提供者模块。

## 重要目录

```text
orchestrator/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/handler.go
│   ├── internal/api/router.go
│   ├── internal/models/orchestration.go
│   ├── internal/repository/repository.go
│   └── internal/service/      # executor、scheduler、engine registry、task client
├── docs/
│   ├── 数据库架构.md
│   ├── 参数化模板说明.md
│   └── tables/
└── frontend/src/
    ├── components/            # DAGEditor、TaskPanel
    ├── views/                 # OrchestrationList、OrchestrationForm、ExecutionList
    └── api/
```

## 核心 API

路由前缀：`/api/v1/orchestrator`。

- 编排管理：`POST /orchestrations`、`GET /orchestrations`、`GET /orchestrations/:id`、`PUT /orchestrations/:id`、`DELETE /orchestrations/:id`。
- 执行管理：`POST /orchestrations/:id/execute`、`GET /orchestrations/:id/executions`、`GET /executions`、`GET /orch-executions/:id`。
- 能力发现：`GET /task-providers`、`GET /tasks`。
- 健康检查：`GET /health`。

## 开发规则

- 编排步骤必须形成 DAG，执行前要校验循环依赖。
- 新编排优先使用 System 注册的 TaskProvider 能力，避免硬编码模块 URL。
- 参数模板只能引用前序依赖步骤的输出，相关说明见 `orchestrator/docs/参数化模板说明.md`。
- Orchestrator 只负责调度和状态聚合，不在本模块实现 Meta 扫描、Transfer 传输或 Manager 瓦片生成的业务细节。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh orchestrator` 和 `bash scripts/swagger/check-route-coverage.sh orchestrator`。

## 开发与验证

```bash
bash scripts/dev/start.sh -orchestrator
bash scripts/dev/restart.sh -orchestrator
curl http://localhost:8084/health
```

常用日志：

- `logs/orchestrator-backend.log`

## 相关文档

- `orchestrator/docs/数据库架构.md`
- `orchestrator/docs/参数化模板说明.md`
- `orchestrator/docs/tables/orchestrations表.md`
- `orchestrator/docs/tables/executions表.md`
- `docs/spec/addp引擎能力声明规范.md`
- `docs/spec/addp工作流计算引擎接口规范.md`
