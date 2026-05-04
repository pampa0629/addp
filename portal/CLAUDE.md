# Portal 模块说明

## 模块定位

Portal 模块是用户侧数据资产门户，通过 BFF 后端聚合 Asset 和 Service 能力，提供资产首页、目录浏览、搜索、详情、申请、我的申请、服务端点和资产评价。

## 技术栈与端口

- 后端：Go + Gin，默认端口 `8184`，环境变量 `PORTAL_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5185`，启动脚本环境变量 `PORTAL_FE_PORT`。
- 依赖：System 认证，Asset 内部 API，Service 内部 API，Redis 认证缓存。

## 重要目录

```text
portal/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/router.go
│   ├── internal/api/handlers.go
│   └── internal/config/config.go
└── frontend/src/
    ├── views/                 # Home、Search、Catalog、AssetDetail、MyApplications
    ├── components/AssetCard.vue
    ├── composables/useAssetType.js
    └── api/portal.js
```

## API 与边界

- 路由前缀：`/api/v1/portal`。
- 主要接口：`/home`、`/search`、`/catalogs`、`/catalogs/:id/assets`、`/assets`、`/assets/:id`、`/assets/:id/apply`、`/assets/:id/apply-status`、`/my/applications`、`/assets/:id/endpoints`、`/assets/:id/ratings`。
- Portal 不直接持久化资产业务数据；新增能力优先在 Asset 或 Service 落业务规则，Portal 只做用户侧聚合和展示。

## 开发与验证

```bash
bash scripts/dev/start.sh -portal
bash scripts/dev/restart.sh -portal
curl http://localhost:8184/health
```

API 或路由变更后运行：

```bash
bash scripts/swagger/gen-swagger.sh portal
bash scripts/swagger/check-route-coverage.sh portal
```

## 相关文档

- `asset/CLAUDE.md`
- `service/CLAUDE.md`
- `docs/plan/数据资产模块群规划.md`
- `docs/spec/addp-API设计规范.md`
