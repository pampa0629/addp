# Monitor 模块说明

## 模块定位

Monitor 模块是 ADDP 的统一执行监控中心，负责查询和展示各模块写入 `common.task_executions` 的任务执行记录、统计趋势和模块健康状态。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8100`，环境变量 `MONITOR_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus + ECharts，开发端口 `5179`，启动脚本环境变量 `MONITOR_FE_PORT`。
- 存储：PostgreSQL `common.task_executions`，Redis 用于认证缓存。

## 重要目录

```text
monitor/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # executions、statistics、modules health API
│   ├── internal/service/      # 查询、统计、健康检查服务
│   └── docs/                  # Swagger 产物
├── frontend/src/
│   ├── views/                 # Dashboard、ExecutionList
│   ├── components/            # StatisticsCard、ExecutionTable、ModuleStatusBadge
│   └── api/monitor.js
└── docs/Monitor模块实施报告.md
```

## API 与数据

- 路由前缀：`/api/v1/monitor`。
- 主要接口：`GET /executions`、`GET /executions/:id`、`GET /executions/stats`、`GET /executions/trend`、`GET /modules`、`GET /modules/:module/health`、`GET /modules/health/all`、`GET /providers/health`、`GET /providers/:module/health`。
- provider health 从 System 读取启用的 TaskProvider 注册记录，复用模块 `/health` 与标准 `GET /tasks?task_type=` 做无副作用探活；Monitor 不复制 capabilities、不修复 provider 注册、不读取 owner 私有表。
- Transfer continuous 的 lag 与 retention health 来自 `common.task_executions.metadata.continuous.diagnostics`；Monitor 列表和详情只展示 owner 已写入的 `healthy|degraded|critical|unknown` 及分区诊断，不直连业务 Kafka，不读取 `transfer.sync_states` 或 `transfer.runtime_leases`。
- 执行记录字段以 `common/execution/task_execution.go` 为准；新增模块写执行记录时应复用 `common/execution/repository.go` 和 `common/execution.EnsureStore`。

## 开发与验证

```bash
bash scripts/dev/start.sh -monitor
bash scripts/dev/restart.sh -monitor
curl http://localhost:8100/health
```

API 或路由变更后运行：

```bash
bash scripts/swagger/gen-swagger.sh monitor
bash scripts/swagger/check-route-coverage.sh monitor
```

## 相关文档

- `monitor/docs/Monitor模块实施报告.md`
- `docs/concepts/addp监控与执行体系图.md`
- `docs/spec/addp任务体系规范.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp-Swagger集成指南.md`
