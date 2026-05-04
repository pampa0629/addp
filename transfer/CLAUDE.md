# Transfer 模块说明

## 模块定位

Transfer 模块是 ADDP 的数据传输中枢，负责导入、导出、同步任务、本地引擎、对象存储辅助浏览、字段映射、转换器和基于 Asynq 的异步执行。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8083`，环境变量 `TRANSFER_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5176`，启动脚本环境变量 `TRANSFER_FE_PORT`。
- 数据库：PostgreSQL `transfer` schema。
- 依赖：System、Meta、Redis/Asynq、MinIO/S3。

## 重要目录

```text
transfer/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # tasks、executions、local-engines、object-storage、transforms
│   ├── internal/service/      # task、execution、local engine、object storage
│   ├── internal/worker/       # Asynq queue、handler、scheduler
│   ├── internal/transform/    # 内置转换器
│   ├── pkg/pipeline/          # Reader -> Transform -> Writer 执行引擎
│   ├── pkg/plugin_loader/
│   ├── pkg/postprocessor/
│   └── plugins/               # readers、writers、注册入口
├── docs/
│   ├── 数据库架构.md
│   ├── transfer-基本概念及配置说明.md
│   └── tables/
└── frontend/src/
    ├── views/                 # TaskList、TaskWizard、ExecutionList、LocalEngines
    ├── components/
    └── api/
```

## 核心 API

路由前缀：`/api/v1/transfer`。

- 公共连通：`GET /ping`。
- 数据源辅助：`GET /engines`、`GET /engines/:engine_id/tree`、`GET /nodes/:node_id/children`、`GET /tables/metadata`。
- 任务：`POST /tasks`、`GET /tasks`、`GET /tasks/statistics`、`GET /tasks/:id`、`PUT /tasks/:id`、`DELETE /tasks/:id`、`POST /tasks/:id/start|stop|pause|resume`、`GET /tasks/:id/executions`。
- 字段映射：`POST /tasks/:id/mappings`、`GET /tasks/:id/mappings`、`DELETE /mappings/:id`。
- 本地引擎：`GET /system-engines`、`GET/POST/PUT/DELETE /local-engines`、`POST /local-engines/test-connection`、`POST /local-engines/:id/test`、`POST /local-engines/:id/sync`。
- 对象存储：`POST /object-storage/browse`、`POST /object-storage/list-files`。
- 执行记录：`GET /executions`、`GET /executions/statistics`、`GET /executions/:id`、`POST /executions/:id/cancel|retry`、`GET /executions/:id/progress|logs`。
- 转换器：`GET /transforms`、`GET /transforms/stats`、`GET /transforms/:name`、`POST /transforms/:name/validate|test`。

## 插件与执行规则

- 数据流统一走 `pkg/pipeline`，遵循 Reader -> Transform -> Writer。
- Reader/Writer 插件在 `backend/plugins/` 下实现和注册，转换器在 `internal/transform/` 下注册。
- 大数据传输要优先考虑批大小、流式读取、Checkpoint 和幂等重试。
- Worker 任务载荷只保存 ID 和必要上下文，不要塞入大对象。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh transfer` 和 `bash scripts/swagger/check-route-coverage.sh transfer`。

## 开发与验证

```bash
bash scripts/dev/start.sh -transfer
bash scripts/dev/restart.sh -transfer
curl http://localhost:8083/health
```

常用日志：

- `logs/transfer-backend.log`
- `logs/transfer-worker.log`

## 相关文档

- `transfer/docs/数据库架构.md`
- `transfer/docs/transfer-基本概念及配置说明.md`
- `transfer/docs/transfer转换器架构分析.md`
- `transfer/docs/transfer高性能分析.md`
- `transfer/docs/tables/tasks表.md`
- `transfer/docs/tables/task_executions表.md`
- `docs/next/engine-plugin-transfer后续事项.md`
