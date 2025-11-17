# 仓库规范（中文）

## 目录结构与模块组织
- 范围：仅 `mvt/` 目录。
- 后端（Go）：`backend/`，入口 `cmd/server/main.go`，内部代码在 `internal/`，配置位于 `backend/config/`（`*.yaml` 与 `*.yaml.example`）。提供调试工具 `cmd/debug-cache/main.go` 用于查看 PG 持久化缓存。
- 前端（Vite + JS/TS）：`frontend/`，包含 `src/`、`public/`、`package.json`、`vite.config.js`。
- 编排与产物：仓库根的 `Makefile`、`docker-compose.yml`；日志在 `logs/`；构建产物在 `dist/` 与 `frontend/dist/`。

## 构建、测试与开发命令
- 初始化：`make init` — 复制 `backend/config/*.example` 为活动配置，并安装前后端依赖。
- 启动基础设施：`make up` 运行 Redis；`make down` 停止；`make logs` 跟踪日志。
- 开发服务：`make dev` 同时启动后端（8090）与前端（5180）。
  - 仅后端：`make dev-backend` 或 `cd backend && go run cmd/server/main.go`。
  - 仅前端：`make dev-frontend` 或 `cd frontend && npm run dev`。
  - 热加载：修改 `backend/config/datasources.yaml` 会在运行时热重载，并自动清空缓存。
- 常用辅助：`make redis-cli` 进入 Redis；`make redis-flush` 清空 Redis 键。
- 构建：`make build`（后端 → `dist/mvt-server`；前端 → `frontend/dist/`）。也可用 `make build-backend`、`make build-frontend`。
- 测试：`make test` 或 `cd backend && go test -v ./...`。
- 清理：`make clean` 清理临时/日志文件。

## 代码风格与命名规范
- Go：包名小写，文件名使用 `snake_case`；对外标识符使用 PascalCase。
- 格式化：提交前运行 `go fmt ./...`。非导出代码放在 `backend/internal/`。
- 前端：组件 `ComponentName.vue`，composable 文件 `camelCase.ts`；样式尽量作用域化。

## 测试规范
- 后端：采用表驱动测试，放在目标代码旁的 `*_test.go` 中。
- 覆盖重点：处理器、服务、配置解析。
- 前端：暂未接入自动化；本地验证 UI 后在 PR 中记录手工场景。

## 提交与合并规范
- 使用 Conventional Commits，例如 `feat(backend): add tile endpoint`、`fix(frontend): guard empty layer list`。
- PR 必须包含：变更范围、影响面（后端/前端）、关联 issue、UI 截图（如有）、以及验证步骤（`make init`、`make up`、`make dev`、`make test`）。
- 合并前请 squash WIP 提交，保持历史清晰。

## 安全与配置提示
- 禁止提交敏感信息。配置文件位于 `backend/config/`；`make init` 会将 `*.example` 复制为活动文件。
- 基础设施使用 Docker 中的 Redis。维护命令：`make up`、`make redis-cli`、`make redis-flush`。

## 配置与运行时行为
- 配置文件：
  - 应用：`backend/config/app.yaml`（端口、数据库、Redis、CORS、缓存策略、预热）。
  - 数据源：`backend/config/datasources.yaml`（每层 extent、像素阈值规则等）。
- 环境变量覆盖：`APP_CONFIG`、`DATASOURCES_CONFIG`、`PORT`、`FRONTEND_URL`、`DATABASE_*`、`REDIS_*`、`CACHE_TTL`、`CACHE_PERSIST_MIN_DURATION`、`CACHE_PERSIST_MIN_RAW_KB`、`PREWARM_*`。
- 端点：
  - 瓦片：`/tiles/:datasource_id/:z/:x/:y.mvt`（返回已 gzip 的 MVT；包含 `Content-Encoding: gzip` 与 `X-Cache` HIT/MISS）。
  - API：`/api/datasources`、`/api/datasources/:id`、`/api/cache/clear[/:datasource_id]`、`/api/cache/stats`。
  - 健康检查：`/health`。
- Gzip 策略：JSON API 通过中间件 gzip；瓦片在处理器中预先 gzip（避免重复压缩）。
- CORS：允许的来源由 `app.yaml` 中的 `frontend_url` 提供。

## 缓存与预热
- 分层缓存：内存 LRU → Redis → Postgres。键格式 `mvt:<ds>:<z>:<x>:<y>`；Redis TTL 由 `redis.cache_ttl` 配置。
- 落库策略：瓦片生成耗时 ≥ `cache_policy.persist_min_duration`，或原始 MVT 大小 ≥ `cache_policy.persist_min_raw_kb`（KB），则将 gzip 结果持久化到 Postgres。
- 预热：启用时，对每个数据源在 z=0..`prewarm.max_zoom` 范围内根据数据范围生成瓦片，使用独立连接池，并全部持久化到 Postgres；并发由 `prewarm.concurrency` 控制。
- 热重载：`backend/config/datasources.yaml` 变更会被运行时检测并热加载，同时清空缓存以避免脏数据。

## 小工具
- 查看 PG 缓存行：`cd backend && go run cmd/debug-cache/main.go`。
