# Meta 模块说明

## 模块定位

Meta 模块负责元数据扫描、元数据存储、元数据查询、对象元数据提取、扫描任务调度、扫描运行记录和资产发现接口。它是 Manager 数据探查、Asset 自动发现和搜索索引的重要来源。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8082`，环境变量 `META_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5175`，启动脚本环境变量 `META_FE_PORT`。
- 数据库：PostgreSQL `metadata` schema。
- 依赖：System、Redis、Meilisearch、MinIO，可选 pgvector/嵌入服务。

## 重要目录

```text
meta/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # 统一 Handler 与路由
│   ├── internal/models/       # node、item、scan_task、cleanup
│   ├── internal/service/      # scan、repository、database/object/filesystem/nosql 扫描
│   ├── internal/search/       # Meilisearch indexer
│   ├── internal/worker/       # Asynq 扫描 Worker
│   └── docs/                  # Swagger 产物
├── docs/
│   ├── 数据库架构.md
│   └── tables/
└── frontend/src/
    ├── views/                 # MetadataScan、TaskMonitor
    └── api/
```

## 核心 API

路由前缀：`/api/v1/meta`。

- 资产发现：`GET /assets/discoverable`。
- 引擎：`GET /engines`。
- 扫描：`POST /scan/auto`、`POST /scan/engine`、`POST /scan/run/manual`。
- 扫描运行：`GET /scan/runs`、`GET /scan/runs/:run_id`、`POST /scan/runs/:run_id/cancel`。
- 扫描任务：`GET /scan/tasks`、`POST /scan/tasks`、`PUT /scan/tasks/:task_id`、`DELETE /scan/tasks/:task_id`、`POST /scan/tasks/:task_id/trigger`。
- 元数据对象：`GET /metadata/object`、`POST /metadata/extract`。
- 引擎数据项：`GET /engines/:engine_id/items`。
- 树查询：`GET /engines/:engine_id/tree`、`GET /nodes/:node_id`、`GET /nodes/:node_id/children`、`GET /nodes/:node_id/items`、`GET /nodes/by-catalog-path`、`GET /items/by-catalog-path`。
- 字段与空间信息：`GET /items/:item_id/fields`、`GET /items/:item_id/spatial`、`GET /items/:item_id`。
- 统计与缓存：`GET /stats`、`DELETE /cache/engines/:engine_id`、`POST /cache/refresh`。

## 开发规则

- 扫描必须执行租户隔离校验，不能绕过 System 引擎归属与当前用户租户。
- 数据库、对象存储、文件系统和 NoSQL 扫描逻辑按 service 分层扩展，避免在 Handler 中写扫描细节。
- 空间元数据必须动态检测几何列、SRID、范围和几何类型，不要默认字段名。
- 扫描去重使用 Redis 锁；调试异常扫描时先检查 `meta:scan_dedup:*`。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh meta` 和 `bash scripts/swagger/check-route-coverage.sh meta`。

## 开发与验证

```bash
bash scripts/dev/start.sh -meta
bash scripts/dev/restart.sh -meta
curl http://localhost:8082/health
```

常用日志：

- `logs/meta-backend.log`
- `logs/meta-worker.log`

## 相关文档

- `meta/docs/数据库架构.md`
- `meta/docs/tables/meta_node表.md`
- `meta/docs/tables/meta_item表.md`
- `meta/docs/tables/scan_tasks表.md`
- `meta/docs/tables/scan_task_runs表.md`
- `docs/spec/addp引擎插件接口规范.md`
- `docs/spec/addp引擎能力声明规范.md`
- `manager/CLAUDE.md`
