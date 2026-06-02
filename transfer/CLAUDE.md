# Transfer 模块说明

## 模块定位

Transfer 模块是 ADDP 的数据传输中枢，负责导入、导出、同步任务、任务配置、字段映射 / 转换编排、写后 Meta 扫描触发和基于 Asynq 的异步执行。

当前主路径基于 `common/engine`、`common/format`、`common/contentio` 和 `common/engine/contentadapter`：

- Transfer 负责任务 JSON、planner、policy、transform、worker、checkpoint、日志、指标和写后 Meta 扫描触发。
- 具体 engine-native 读写由 `common/engine` 提供。
- 具体格式和数据类型读写由 `common/format` 提供。
- content 的定位、读取、写入、range 和 scope list 由 `common/contentio` 表达；multi ref 的组织规则和读写语义由 `common/format` / `common/dataitem` / Transfer 编排层表达；engine content provider 到 contentio 的桥接由 `common/engine/contentadapter` 提供。
- 旧 Transfer 私有 reader / writer 插件体系、旧 `pkg/pipeline`、旧 `pkg/plugin_loader` 不作为新功能入口。

table 类型 Transfer 主链路已经稳定：native table、encoded single file/object、encoded multi refs 和 encoded whole scope 都统一走 planner + executor + common provider，不按具体引擎组合建立专用通道。后续新增 table 能力应优先补 `common/engine` 或 `common/format`，不要在 Transfer 内恢复私有 reader / writer。

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
│   ├── internal/planner/      # source/target endpoint -> table transfer plan
│   ├── internal/executor/     # 基于 common engine/format/contentio 的 table transfer executor
│   ├── internal/service/      # task、execution、system engine resolver、Meta scan 触发
│   ├── internal/worker/       # Asynq queue、handler、scheduler
│   └── pkg/vfs/               # 兼容性辅助能力，非新 transfer reader/writer 主入口
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
- 字段映射：`POST /tasks/:id/mappings`、`GET /tasks/:id/mappings`、`DELETE /mappings/:id`。该接口仍存在，但新执行主线只消费 `config.transforms[type=field_mapping]`。
- 本地引擎：旧路线遗留能力；新任务 endpoint 使用 System engine，不以 local engine 作为新功能入口。
- 对象存储：`POST /object-storage/browse`、`POST /object-storage/list-files`。
- 执行记录：`GET /executions`、`GET /executions/statistics`、`GET /executions/:id`、`POST /executions/:id/cancel|retry`、`GET /executions/:id/progress|logs`。
- 转换器：`GET /transforms`、`GET /transforms/stats`、`GET /transforms/:name`、`POST /transforms/:name/validate|test`。

## 执行规则

- 新任务配置必须使用 source / target endpoint JSON，旧 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段出现即拒绝。
- table transfer 统一走 `internal/planner` + `internal/executor`，按 data type / representation / layout 分叉，不按具体引擎组合分叉。
- encoded file/object 读写必须通过 `common/engine` content provider + `common/engine/contentadapter` + `common/format` provider，不在 Transfer 中新增私有 reader / writer。
- Shapefile 等 multi 文件格式通过 `contentio.Reader` / `contentio.Writer` + `[]format.RelatedRef` 与 `common/format` multi table provider 接入。
- overwrite / append 是 Transfer policy；删除指定资源由 `common/engine` ResourceDeleteProvider 提供。
- checkpoint 当前只用于进度展示、故障定位和 provider marker 观测；失败执行 retry 按 restartable 从头重新入队，append 任务 retry 会被拒绝。不得宣称 table Transfer 已支持 checkpoint resumable。
- 大数据传输要优先考虑批大小、连续读取 / 写入 session、进度日志和 restartable retry。
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
- `transfer/docs/design.md`
- `transfer/docs/transfer转换器架构分析.md`
- `transfer/docs/transfer高性能分析.md`
- `transfer/docs/tables/tasks表.md`
- `transfer/docs/tables/task_executions表.md`
