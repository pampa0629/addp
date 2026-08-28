# Portal 模块说明

## 模块定位

Portal 模块是用户侧数据资产门户，通过 BFF 后端聚合 Asset 和 Service 能力，提供资产首页、AssetCategory 资产目录浏览、搜索、详情、申请、我的申请、服务端点和资产评价。

Portal 只消费 Asset 已发布对象和 AssetCategory 投影，不直接搜索 Enterprise Catalog 全量资源，不调用 Meta 自动发现，也不绕过 Asset 的申请、授权和运营边界。企业资源盘点和治理视图属于 Catalog；Asset 是资产目录事实 owner，Portal 只负责消费者展示。

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
- 主要接口：`/home`、`/search`、`/categories`、`/categories/:id/assets`、`/assets`、`/assets/:id`、`/assets/:id/apply`、`/assets/:id/apply-status`、`/my/applications`、`/assets/:id/ratings`。
- Portal 不直接持久化资产业务数据；新增能力优先在 Asset 或 Service 落业务规则，Portal 只做用户侧聚合和展示。
- Portal 调用 Asset 消费 API 时只在当前同步请求栈内转发已验证的 User Bearer，不保存 Token，不提交 User/Tenant/Role 字段，也不使用 Portal Service Principal 替代资产使用者。
- Portal 不读取 `INTERNAL_API_KEY`，也不发送 `X-Internal-API-Key` 或 `X-Tenant-ID`。
- Portal 正式浏览器入口固定为 Console 当前 origin 的 `/portal/`。开发端口 `5185` 只承载前端服务，由 Console Vite 代理；Console 不直接打开该端口，确保 Portal 与 Console 共享 Browser AuthSession 协调域。
- Portal Backend 使用 `addp-portal` 的 Platform Service Token 向 System 唯一模块注册表注册 `/portal` 并持续心跳；Gateway 不保留 Portal 静态地址或未注册 fallback。平台 `platform.portal_runtime` 只包含 `system.runtime_registry.update`，Portal 不再持有 Tenant Runtime Role。

## 前端公开路由

- Portal 是独立用户门户，公开路由固定保留 `/portal` 前缀，不接入 Console iframe 模块导航桥。
- 搜索页使用 `keyword`、`type_id`、`page`，资产分类页使用 path `/portal/categories/:id` 与 query `page`，默认页码 1 省略。
- 资产详情唯一使用 `/portal/assets/:id`。返回操作只复用已验证的 `/portal/...` 浏览器历史；没有合法 Portal 来源时，根据资产 `category_id` 回资产分类，否则回搜索页。
- 不接受任意 `return_url`，不把申请表单、API Key、Token 或服务凭据写入 URL。

## 开发与验证

```bash
bash scripts/dev/start.sh -portal
bash scripts/dev/restart.sh -portal
curl http://localhost:8184/health/ready
cd portal/frontend && npm test && npm run build
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
