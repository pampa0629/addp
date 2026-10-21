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

`nginx.conf` 是完整的 Nginx 主配置，依赖 ADDP 容器网络中的服务名，不能作为本地 Nginx 的 `servers/` 片段直接复制。

## 配置说明

### Console 与模块路径

```nginx
location / {
    proxy_pass http://console;
}
location /module-ui/system/ {
    proxy_pass http://system-frontend/;
}
```

其余模块在同一配置中采用各自的 `/module-ui/<frontend>/` 路由；Console 的 `/<module>/` 公开地址仍由根入口处理。

### API 代理

```nginx
location /api/ {
    proxy_pass http://gateway;
}
```

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

```bash
# Docker 容器日志
docker logs addp-nginx

```

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

```nginx
gzip on;
gzip_types text/css application/javascript application/json;
```

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

```nginx
location /nginx_status {
    stub_status on;
    access_log off;
    allow 127.0.0.1;
    deny all;
}
```

访问 `http://localhost/nginx_status` 查看状态。

## 相关文档

- [Nginx 配置指南](../docs/NGINX_GUIDE.md) - 详细说明
- [根目录 README](../README.md) - 项目整体文档
- [Gateway 架构](../gateway/ARCHITECTURE.md) - Gateway 说明
