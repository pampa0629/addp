# Nginx 配置说明

## 配置文件

- `nginx.conf` - Nginx 主配置文件

## 使用方式

### 标准容器部署

Nginx 是根 [docker-compose.yml](../docker-compose.yml) 中 `addp-platform` 的默认服务，随标准容器部署入口启动。从项目根目录按场景选择入口，并按对应指南准备镜像与配置：

| 场景 | 启动入口 | 准备与生命周期说明 |
| --- | --- | --- |
| 本地容器部署 | `bash scripts/local/start.sh` | [Local 指南](../scripts/local/README.md#快速启动) |
| 生产部署 | `bash scripts/prod/start.sh` | [Prod 首次部署](../scripts/prod/README.md#首次部署) |

镜像的平台、Registry 和版本须与部署配置一致，构建与发布要求统一见[Build 指南](../scripts/build/README.md#build-imagessh)。

Console 公开路由由根入口处理；各模块 iframe 与静态资源通过 `/module-ui/<frontend>/` 转发。`/portal/` 和 `/data-apps/` 保持独立入口。生产镜像与部署包均使用 `nginx/nginx.conf`。

Console与Gateway是入口必需依赖；模块页面在请求时通过Docker DNS解析，未部署的模块页面返回明确的代理失败，不阻止入口及已部署页面启动。转发必须保留去除`/module-ui/<frontend>/`前缀后的路径和原查询参数，不能把子路径统一改成`/`。`make test-nginx-routes`用独占网络和真实Nginx核验路径、参数、静态资源及缺失模块502；该检查同时纳入现有`make test-monitor-traces`，不执行正式身份或Hosted验收。

`nginx.conf` 是完整的 Nginx 主配置，依赖 ADDP 容器网络中的服务名，不能作为本地 Nginx 的 `servers/` 片段直接复制。

## 配置说明

### Console 与模块路径

```nginx
location / {
    proxy_pass http://console;
}
location /module-ui/system/ {
    rewrite ^/module-ui/system/(.*)$ /$1 break;
    proxy_pass http://$system_frontend_upstream;
}
```

其余模块在同一配置中采用各自的 `/module-ui/<frontend>/` 路由；Console 的 `/<module>/` 公开地址仍由根入口处理。

### API 代理

```nginx
location /api/ {
    proxy_pass http://gateway;
}
```

仓库当前配置只将 `/api/` 转发 Gateway；Gateway 已挂载的 `/ogc/features`、`/wmts`、`/tiles`、`/ogc/tiles` 尚未在根 Nginx 转发，会落入指向 Console 的 `/`。Features 的地址来源和入口核对统一见[Service 协议测试专题](../service/docs/testing/查询与要素协议.md#元数据链接与公开入口核对)，瓦片沿[瓦片入口专题](../service/docs/architecture/瓦片发布与缓存.md#元数据链接与公开入口)。给协议 URL 添加 `/api` 不是已挂载的入口。

直连 Service 或 Gateway 与经根 Nginx、上级 HTTPS 代理访问须分别验收。配置语法、健康检查或 HTTP 200 不证明协议响应正确，返回内容可能是 Console 页面；集合发现或改写链接后成功也不证明原始链接可达。证书、实际代理路径、原始元数据链接及要素/瓦片读取按上述 Features 专题和[瓦片公开入口核对](../service/docs/testing/瓦片协议.md#元数据链接与公开入口核对)分别验证；配置公开 origin 或启用 HTTPS 不会补齐协议转发，未接通、失败和未运行项分别记录。

## 验证配置

```bash
# 在已启动的 ADDP Nginx 容器内检查实际加载的配置
docker exec addp-nginx nginx -t
docker exec addp-nginx nginx -T
```

## 常见问题

### 1. 端口已被占用

先按[生产端口冲突排查](../scripts/prod/README.md#问题-2-端口冲突)核对报错中的宿主机绑定地址、实际映射和占用者归属。需要更换本部署入口时，修改根 `.env` 的 `NGINX_PORT`，由 `NGINX_BIND_HOST` 控制监听范围；容器内保持 `listen 80;`，健康检查继续访问容器内部的 HTTP 80 `/health`。

公开访问地址或上级代理目标变化时，同步核对 `ADDP_PUBLIC_ORIGIN` 与代理转发配置，再通过[标准部署入口](#标准容器部署)生效。生产入口端口冲突时启动失败，不自动改选端口；映射和公开地址规则见[端口规范](../docs/spec/addp端口分配.md)。

### 2. 日志查看

先区分日志来源，Nginx 的配置路径以[主配置](nginx.conf)为准：

| 日志 | 当前来源 | 排查范围 |
| --- | --- | --- |
| Nginx 访问日志 | `/var/log/nginx/access.log`，使用 `main` 格式 | 客户端请求、Nginx 响应状态和发送字节数等，不包含模块内部处理过程 |
| Nginx 错误日志 | `/var/log/nginx/error.log`，记录 `warn` 及更严重级别 | Nginx 自身与代理链路的警告、错误，不代替 Gateway 或目标模块的异常正文 |
| 模块服务正文日志 | Backend、Worker、Scheduler、Gateway 的 stdout/stderr，由标准运行日志接收器按实例采集 | 在 System → 模块管理 → 服务实例 → 查看日志；来源、授权及查询边界见[运行日志规范](../docs/spec/addp模块服务运行日志规范.md#五正文查询与历史分页) |

从已启动的 Nginx 容器核对日志输出：

```bash
# 核对配置路径在实际镜像中是否链接到 stdout/stderr
docker exec addp-nginx ls -l /var/log/nginx/access.log /var/log/nginx/error.log
# 核对容器日志驱动，再查看最近的容器标准输出与标准错误
docker inspect --format '{{.HostConfig.LogConfig.Type}}' addp-nginx
docker logs --tail 100 addp-nginx
```

`docker logs` 是否包含访问和错误记录，取决于实际镜像的路径链接及日志驱动；普通文件须另行核对其采集与保留方式。根 Compose 未给 Nginx 配置日志目录挂载或模块运行日志接收器，不能将上述路径当作宿主机 `logs/` 文件，也不能据此认定已接入 System 的模块正文查询。

验证时，用实际请求及同一时间窗口的访问记录核对入口行为，代理故障再结合错误记录和 Gateway、目标模块的正文排查。能查到容器输出或某条请求记录，只能证明相应来源在该窗口可查，不能证明模块日志已送达、历史日志完整或业务执行成功。

## HTTPS 配置

仓库的 `nginx.conf` 当前只监听 HTTP 80，根 Compose 只发布该入口，未配置证书挂载或 HTTPS server。生产 HTTPS 由部署方在上级反向代理终止 TLS，再转发到 ADDP 的 Nginx 统一入口。

### 使用 Let's Encrypt 免费证书

证书申请、域名验证及续期在实际 HTTPS 代理所在环境按 [Certbot 官方指南](https://eff-certbot.readthedocs.io/en/stable/using.html#nginx)配置。证书链和私钥路径须对该代理可读，私钥限制访问；若代理使用 Nginx，其 HTTPS `server` 块中的配置片段例如：

```nginx
listen 443 ssl;
ssl_certificate /etc/letsencrypt/live/addp.example.com/fullchain.pem;
ssl_certificate_key /etc/letsencrypt/live/addp.example.com/privkey.pem;
```

路径和域名按实际证书部署填写，指令及证书链要求见 [Nginx HTTPS 指南](https://nginx.org/en/docs/http/configuring_https_servers.html)。

### 配置 HTTPS

上级代理须能访问 ADDP 实际发布的 Nginx 地址，并将 Console、模块前端和 `/api/` 一起转发到该统一入口；API 仍经 Gateway，不绕过统一路由。

在根 `.env` 将 `ADDP_PUBLIC_ORIGIN` 设置为用户实际访问的 HTTPS origin，例如 `https://addp.example.com`，配置要求见[环境变量参考](../docs/spec/configuration/环境变量参考.md#根目录-env)。该变量用于公开地址，不会生成证书或启用 TLS 监听。`NGINX_BIND_HOST` 和 `NGINX_PORT` 控制 ADDP 的宿主机监听范围与端口；同宿主机代理可使用回环监听，其他部署按实际代理网络限制监听范围，详见[端口规范](../docs/spec/addp端口分配.md)。

证书和 TLS 配置变更后，由部署方在上级代理执行配置检查，再验证公开 HTTPS 入口、页面资源及 API；ADDP 的 HTTP `/health` 不能代替这些验证。

## 性能优化

### 启用 Gzip 压缩

现有[主配置](nginx.conf)已在 `http` 块设置 `gzip on`。`gzip_types` 在默认 `text/html` 之外增加文本、CSS、JavaScript、JSON、XML/RSS、字体和 SVG 类型；完整 MIME 清单、压缩等级及其他参数以该块为准，指令含义见[Nginx Gzip 模块说明](https://nginx.org/en/docs/http/ngx_http_gzip_module.html)。

调整时修改既有 `http` 块，经[标准构建与部署入口](#标准容器部署)生效，再按[验证配置](#验证配置)核对实际加载配置。还须使用匹配 MIME 且满足压缩条件的真实响应，验证客户端 `Accept-Encoding` 协商及 `Content-Type`、`Content-Encoding` 等响应头；语法检查或 HTTP 200 不代替压缩效果、性能和业务验收。

### 启用 HTTP/2

若上级 HTTPS 代理使用 Nginx，HTTP/2 配置也在该代理维护；模块支持及配置语法按实际部署版本参照 [Nginx HTTP/2 模块说明](https://nginx.org/en/docs/http/ngx_http_v2_module.html)。

### 调整工作进程

`worker_processes` 位于主配置层，`worker_connections` 位于 `events` 块。[现有主配置](nginx.conf)已经包含这两项；调整时修改既有指令与 `events` 块，下面仅展示相关结构：

```nginx
worker_processes auto;

events {
    worker_connections 1024;
}
```

该片段不是完整主配置。调整后的实际加载配置仍按[验证配置](#验证配置)检查；语法检查通过不代表性能或业务链路已经验收。

## 监控

### 启用状态页面

当前[主配置](nginx.conf)未启用 `stub_status` 或 `/nginx_status`。以下是可选配置片段，应放在既有 `http` 内的 `server` 块中；实际启用前确认所部署 Nginx 包含该模块，指令与统计含义见[官方模块说明](https://nginx.org/en/docs/http/ngx_http_stub_status_module.html)。

```nginx
location = /nginx_status {
    stub_status;
    access_log off;
    allow 127.0.0.1;
    deny all;
}
```

允许列表按 Nginx 实际识别的客户端地址判定，规则见[访问控制说明](https://nginx.org/en/docs/http/ngx_http_access_module.html)。宿主机端口转发或上级代理访问时，应核对来源地址及拒绝行为；容器内的回环来源与宿主机访问地址分别验证。

配置通过[标准构建与部署入口](#标准容器部署)生效，并按[验证配置](#验证配置)检查实际加载的内容。完成部署后，可从 Nginx 容器内部检查统计正文：

```bash
docker exec addp-nginx wget -qO- http://127.0.0.1/nginx_status
```

应核对 `Active connections` 等统计内容，并验证非允许来源被拒绝；仅返回 HTTP 200 不能证明状态页已启用。该页面反映 Nginx 连接统计，不替代模块健康检查、性能测试或业务验收。

## 相关文档

- [部署和开发步骤](../docs/guide/addp部署和开发步骤.md) - 环境准备与标准部署入口
- [根目录 README](../README.md) - 项目整体文档
- [Gateway 架构](../gateway/docs/gateway架构说明.md) - 路由平面与代理边界
