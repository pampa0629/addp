# ADDP Gateway

Gateway 提供统一入口、System 注册表驱动的模块代理，以及 API Consumer 数据面认证、限流和访问日志。业务资源的最终授权由目标 owner 模块完成。

## 路由

| 路由 | 用途 | 认证 |
| --- | --- | --- |
| `/api/v1/:module/*` | 控制面与 owner 管理 API | Bearer；拒绝 `X-API-Key` |
| `/api/query/:serviceName/query` | Query Service 数据面 | 公开、Bearer 或 API Consumer Credential |
| `/api/gquery/:serviceName` | 旧有图查询入口 | 由 Service 判断 |
| `/ogc/*`、`/wmts/*`、`/tiles/*` | 服务协议入口 | 由 Service 判断 |

表中协议路径为 Gateway 路由组概览；瓦片端点与入口边界统一见[Service 专题](../service/docs/architecture/瓦片发布与缓存.md#元数据链接与公开入口)。仓库根 Nginx 尚未转发 `/wmts`、`/tiles`、`/ogc/tiles` 协议路径；直连 Gateway 成功不代表统一公开入口可用，验收见[瓦片协议入口核对](docs/gateway运维手册.md#瓦片协议入口核对)。

API Consumer Credential 使用 `X-API-Key: addp_api_*`。它不是 User、Service Principal、Tenant Service Account 或 OAuth Client，不能访问 `/api/v1/*`。

## 开发

从仓库根目录执行 `make test-module MODULE=gateway`；验证分层及边界见[运维手册](docs/gateway运维手册.md#回归验证)。

仓库内启动和重启必须遵循根 `AGENTS.md`：

```bash
bash scripts/dev/start.sh
./scripts/dev/restart.sh -gateway
```

详细设计见 [Gateway 架构说明](docs/gateway架构说明.md)，排障见 [Gateway 运维手册](docs/gateway运维手册.md)。
