# Asset 模块说明

## 模块定位

Asset 模块负责数据资产管理，包括资产类型、资产目录、资产上下架、授权申请、授权有效期、评价反馈以及跨 Meta、Service、Standard、Develop 的资产自动发现。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8183`，环境变量 `ASSET_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5184`，启动脚本环境变量 `ASSET_FE_PORT`。
- 数据库：PostgreSQL `asset` schema。
- 依赖：System 认证与模块注册，Redis 认证缓存，Meilisearch 资产全文搜索可选。

## 重要目录

```text
asset/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/router.go
│   ├── internal/models/models.go
│   ├── internal/service/      # asset/catalog/type/application/authorization/rating
│   └── internal/search/       # Meilisearch indexer
└── frontend/src/
    ├── views/                 # AssetManager、CatalogManagement、ApplicationList
    ├── api/asset.js
    └── components/Layout.vue
```

## API 与数据

- 路由前缀：`/api/v1/asset`。
- 主要资源：`type-definitions`、`catalogs`、`assets`、`applications`、`authorizations`、`ratings`。
- 资产自动发现会调用 `META_URL`、`SERVICE_URL`、`STANDARD_URL`、`DEVELOP_URL`，不要在业务逻辑中硬编码单一来源。
- 资产发布、授权和评价均按租户隔离，认证信息通过 `common/middleware/auth` 获取。

## 开发与验证

```bash
bash scripts/dev/start.sh -asset
bash scripts/dev/restart.sh -asset
curl http://localhost:8183/health
```

API 或路由变更后运行：

```bash
bash scripts/swagger/gen-swagger.sh asset
bash scripts/swagger/check-route-coverage.sh asset
```

## 相关文档

- `docs/plan/数据资产模块群规划.md`
- `docs/plan/数据资产模块群开发计划.md`
- `docs/spec/addp-API设计规范.md`
- `docs/spec/addp-Swagger集成指南.md`
