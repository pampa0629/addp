# Nginx 配置说明

## 配置文件

- `nginx.conf` - Nginx 主配置文件

## 使用方式

### 方式 1: Docker Compose（推荐）

在根目录的 `docker-compose.yml` 中已配置 nginx 服务（使用 profile）：

```bash
# 启动包含 Nginx 的完整环境
docker-compose --profile nginx up -d

# 或者单独启动 Nginx
docker-compose up -d nginx
```

### 方式 2: 独立 Docker 容器

```bash
# 构建并启动包含唯一生产路由配置的镜像
docker build -t addp-nginx ./nginx
docker run -d \
  --name addp-nginx \
  -p 80:80 \
  --network addp-network \
  addp-nginx
```

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

```bash
# 查看占用 80 端口的进程
lsof -i :80

# 修改配置文件中的端口
listen 8080;  # 改为其他端口
```

### 2. 日志查看

```bash
# Docker 容器日志
docker logs addp-nginx

```

## HTTPS 配置

### 使用 Let's Encrypt 免费证书

```bash
# 安装 certbot
brew install certbot  # macOS
sudo apt install certbot  # Ubuntu

# 获取证书
sudo certbot certonly --nginx -d addp.example.com

# 证书会保存在
/etc/letsencrypt/live/addp.example.com/fullchain.pem
/etc/letsencrypt/live/addp.example.com/privkey.pem
```

### 配置 HTTPS

取消 `nginx.conf` 中 HTTPS 部分的注释，并更新证书路径。

## 性能优化

### 启用 Gzip 压缩

```nginx
gzip on;
gzip_types text/css application/javascript application/json;
```

### 启用 HTTP/2

```nginx
listen 443 ssl http2;
```

### 调整工作进程

```nginx
# nginx.conf 顶部添加
worker_processes auto;
worker_connections 1024;
```

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
