# Asset 模块说明

## 模块定位

Asset 模块负责数据资产管理，包括资产类型、`AssetCategory` 多级资产目录、一个或多个企业 CatalogEntry 的资产组合、资产上下架、授权申请、授权有效期、评价反馈和运营。AssetCategory 只组织 Asset，不复制企业资源目录、业务域或引擎资源树；CatalogEntry 是唯一资源组合入口。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8183`，环境变量 `ASSET_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus，开发端口 `5184`，启动脚本环境变量 `ASSET_FE_PORT`。
- 数据库：PostgreSQL `asset` schema。
- 依赖：System 认证与模块注册，Redis 认证缓存，Meilisearch 资产全文搜索可选。

## 重要目录

```text
asset/
├── authorization/
│   └── permissions.yaml       # Asset Permission Manifest，发布期聚合事实源
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/router.go
│   ├── internal/api/handlers.go
│   ├── docs/                  # 由 swag 生成的发布检查文档
│   ├── internal/models/models.go
│   ├── internal/service/      # asset/category/type/application/authorization/rating
│   └── internal/search/       # Meilisearch indexer
└── frontend/src/
    ├── views/                 # AssetManager、CategoryManagement、ApplicationList
    ├── api/asset.js
    └── components/Layout.vue
```

## API 与数据

- Asset 是 `asset.management.read`、`asset.category.*`、`asset.entry.*`、`asset.application.*`、`asset.authorization.*` 和 `asset.rating.*` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。
- 路由前缀：`/api/v1/asset`。
- 主要资源：`type-definitions`、`categories`、`assets`、`applications`、`authorizations`、`ratings`。
- 公开业务路由只绑定 `internal/api/handlers.go` 中的真实 Handler；类型定义只读，不发布永久返回 403 的写路由。
- 每个公开 Operation 必须在 Swagger 中声明 `x-addp-auth-mode`，Permission 模式使用 Asset Manifest 中的精确 Key。
- 管理路由必须同时要求 `asset.management.read` 和对应资源 Permission；消费路由位于唯一的 `/consumer` 子资源下，只允许已发布资产，并把申请、授权和评价主体固定为当前 User AuthContext。
- Portal 仅在同步请求栈内向 Asset 消费路由转发当前 User Bearer；Asset 不接受调用方提交的 `applicant_id`、`user_id`、Tenant Header 或内部密钥。
- 目标来源模型是 `Asset -> AssetComponent[] -> catalog_entry_id`。Asset 不复制 Catalog 的来源绑定、语义关联或责任事实；发布时校验 CatalogEntry 有效性，发布承诺和需要冻结的说明由 Asset 自己版本化。
- Asset 只通过 Catalog 选择和组合目录对象，不调用 Meta、Service、Standard 或 Develop 自动创建草稿，也不保留 `{source_module, source_reference}` 或 fallback。
- 资产发布、授权和评价均按租户隔离，认证信息通过 `common/middleware/auth` 获取。

## 开发与验证

```bash
bash scripts/dev/start.sh -asset
bash scripts/dev/restart.sh -asset
curl http://localhost:8183/health/ready
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

## 前端公开路由

- 模块内 Router 使用 `/assets`、`/applications`、`/categories` 等无模块前缀路径；Console 公开 URL 统一加 `/asset` 前缀。
- 资产公开路由为 `/assets/new`、`/assets/:id` 和 `/assets/:id/edit`；创建与编辑都基于 CatalogEntry 选择和组合。
- 资产列表分类使用 `category_id`；申请与授权默认 `applications` Tab 省略，问题反馈使用 `?tab=feedbacks`。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`；编辑保存和取消均用 `replace` 回到详情。
